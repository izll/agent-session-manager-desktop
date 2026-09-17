package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"

	"asmgr-desktop/remote/protocol"
)

// Talking to the helper once it is installed.
//
// One long-lived SSH channel carries every request, each paired with its
// response by id so several can be in flight at once. The sidebar asks about
// every session at the same time; waiting for each answer in turn would make
// the poll as slow as the sum of its parts.

// HelperPath is where the helper is installed, relative to the home directory.
// Hidden and per-user: it needs no privileges, and nothing else should be
// tempted to run it.
const HelperPath = ".asmgr/asmgrd"

// Helper is a running helper process on a server.
type Helper struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *protocol.Response
	closed  bool
	// closeErr explains why the helper is gone, so a request arriving after
	// the connection dropped says that rather than blocking or panicking.
	closeErr error

	writeMu sync.Mutex
	encoder *json.Encoder

	// onClosed is called once when the helper stops answering, so the owner of
	// the connection can replace it instead of handing out a dead one.
	onClosed func(error)
}

// OnClosed registers what to do when this helper ends.
func (h *Helper) OnClosed(handler func(error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onClosed = handler
}

// StartHelper launches the helper and begins reading its answers.
func StartHelper(client *Client) (*Helper, error) {
	session, err := client.SSH().NewSession()
	if err != nil {
		return nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	// Started by absolute path through $HOME rather than relying on the PATH,
	// which a non-interactive shell does not set up the way a login does.
	if err := session.Start("$HOME/" + HelperPath + " --stdio"); err != nil {
		session.Close()
		return nil, fmt.Errorf("could not start the helper: %w", err)
	}

	helper := &Helper{
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int64]chan *protocol.Response),
		encoder: json.NewEncoder(stdin),
	}
	go helper.readLoop()
	return helper, nil
}

// readLoop hands each response to whoever is waiting for it.
func (h *Helper) readLoop() {
	reader := bufio.NewReaderSize(h.stdout, 64<<10)
	decoder := json.NewDecoder(reader)

	for {
		var response protocol.Response
		if err := decoder.Decode(&response); err != nil {
			// The helper is gone. Everyone waiting has to be told, or they
			// wait forever for an answer that cannot arrive.
			h.failAll(fmt.Errorf("the connection to the helper ended: %w", err))
			return
		}

		h.mu.Lock()
		waiting, found := h.pending[response.ID]
		delete(h.pending, response.ID)
		h.mu.Unlock()

		if found {
			waiting <- &response
			continue
		}
		// A response to a request that gave up. Dropped rather than logged:
		// it is the expected result of a cancelled call, not a fault.
	}
}

func (h *Helper) failAll(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	h.closeErr = err
	for id, waiting := range h.pending {
		waiting <- &protocol.Response{ID: id, Error: err.Error()}
		delete(h.pending, id)
	}
	handler := h.onClosed
	h.onClosed = nil

	if handler != nil {
		// Outside the lock, and in its own goroutine: the handler drops this
		// connection from the pool, which takes locks of its own.
		go handler(err)
	}
}

// Call sends one request and waits for its answer.
func (h *Helper) Call(ctx context.Context, method string, params any, result any) error {
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = encoded
	}

	id := h.nextID.Add(1)
	answer := make(chan *protocol.Response, 1)

	h.mu.Lock()
	if h.closed {
		err := h.closeErr
		h.mu.Unlock()
		return err
	}
	h.pending[id] = answer
	h.mu.Unlock()

	// The write is serialised: two JSON messages interleaved on one pipe
	// would be unreadable at the other end.
	h.writeMu.Lock()
	err := h.encoder.Encode(protocol.Request{ID: id, Method: method, Params: raw})
	h.writeMu.Unlock()
	if err != nil {
		h.forget(id)
		return err
	}

	select {
	case <-ctx.Done():
		// Stop waiting, but leave nothing behind: the response may still
		// arrive, and readLoop drops one nobody is waiting for.
		h.forget(id)
		return ctx.Err()

	case response := <-answer:
		if response.Error != "" {
			return fmt.Errorf("%s", response.Error)
		}
		if result != nil && len(response.Result) > 0 {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	}
}

func (h *Helper) forget(id int64) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

// Run executes one command on the server.
func (h *Helper) Run(ctx context.Context, command string) (*protocol.RunResult, error) {
	var result protocol.RunResult
	err := h.Call(ctx, protocol.MethodRun, protocol.RunParams{Command: command}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// RunIn executes a command in a directory on the server.
func (h *Helper) RunIn(ctx context.Context, dir, command string) (*protocol.RunResult, error) {
	var result protocol.RunResult
	err := h.Call(ctx, protocol.MethodRun, protocol.RunParams{
		Command: command,
		Dir:     dir,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Version asks what the helper is.
func (h *Helper) Version(ctx context.Context) (*protocol.VersionResult, error) {
	var result protocol.VersionResult
	if err := h.Call(ctx, protocol.MethodVersion, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Close ends the helper process.
//
// A deliberate close is not a lost connection, so the closed handler is
// dropped first: the pool is already doing whatever it called this for, and
// telling it the connection failed would have it rebuild one nobody asked for.
func (h *Helper) Close() error {
	h.mu.Lock()
	h.onClosed = nil
	h.mu.Unlock()

	h.failAll(fmt.Errorf("the helper was closed"))
	h.stdin.Close()
	return h.session.Close()
}

// InstalledHelper is what a server already has, if anything.
type InstalledHelper struct {
	Present  bool
	Protocol int
	Build    string
	Arch     string
}

// UpToDate reports whether this helper can be talked to.
//
// An exact protocol match, not "at least": an older app talking to a newer
// helper is as wrong as the other way round, and a mismatch that is tolerated
// once becomes a mismatch nobody notices.
func (i *InstalledHelper) UpToDate() bool {
	return i.Present && i.Protocol == protocol.Version
}

// InspectHelper reports what is installed on a server.
func InspectHelper(ctx context.Context, client *Client) (*InstalledHelper, error) {
	// The absence of the file is the common case on a first connection, and
	// not an error: the command is written so that it answers either way.
	out, err := client.Run(ctx, "test -x $HOME/"+HelperPath+
		" && $HOME/"+HelperPath+" --version || echo absent")
	if err != nil {
		// A failure here is a connection problem, not a missing helper.
		return nil, err
	}
	return parseHelperVersion(string(out)), nil
}

// parseHelperVersion reads the one line the helper prints for --version.
func parseHelperVersion(output string) *InstalledHelper {
	line := strings.TrimSpace(output)
	if line == "" || strings.Contains(line, "absent") || !strings.HasPrefix(line, "asmgrd ") {
		return &InstalledHelper{}
	}

	installed := &InstalledHelper{Present: true}
	for _, field := range strings.Fields(line) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		switch key {
		case "protocol":
			if number, err := strconv.Atoi(value); err == nil {
				installed.Protocol = number
			}
		case "build":
			installed.Build = value
		case "arch":
			installed.Arch = value
		}
	}
	return installed
}
