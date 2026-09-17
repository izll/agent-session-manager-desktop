// Command asmgrd runs on a server and answers questions from the desktop app.
//
// It is started over SSH as `asmgrd --stdio` and speaks on its own standard
// input and output, so nothing listens on a port and nothing is left running
// between connections. When the app disconnects — the laptop is closed, the
// network drops — this process ends, and the tmux sessions it started keep
// going. That is the whole point: the work belongs to the multiplexer, not to
// us.
//
// Because of that, the helper holds no state. Everything it reports is read
// from the server each time it is asked, so a reconnection needs no handover
// and a crash loses nothing.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"asmgr-desktop/remote/protocol"
)

// Build is stamped in at release time so a helper can be told apart from the
// one a previous version installed.
var Build = "dev"

// defaultCommandTimeout bounds a command that did not ask for a limit.
//
// Generous, because legitimate work here is slow — a first agent start, a diff
// over a large repository. It exists for the command that never returns at
// all, which would otherwise hold a goroutine and a pipe for as long as the
// connection lives.
const defaultCommandTimeout = 2 * time.Minute

func main() {
	stdio := flag.Bool("stdio", false, "serve the protocol on stdin/stdout")
	version := flag.Bool("version", false, "print the protocol version and exit")
	flag.Parse()

	if *version {
		// One line, machine-readable: the installer reads this to decide
		// whether the binary on the server is the one it expects.
		fmt.Printf("asmgrd protocol=%d build=%s arch=%s/%s\n",
			protocol.Version, Build, runtime.GOOS, runtime.GOARCH)
		return
	}

	if !*stdio {
		fmt.Fprintln(os.Stderr, "asmgrd is started by Agent Session Manager over SSH; "+
			"run it with --stdio to serve, or --version to identify it")
		os.Exit(2)
	}

	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "asmgrd: %v\n", err)
		os.Exit(1)
	}
}

// serve reads requests until the input ends.
//
// The input ending is the normal way this stops: it means the SSH channel
// closed, which means the app is gone.
func serve(in *os.File, out *os.File) error {
	reader := bufio.NewReaderSize(in, 64<<10)
	decoder := json.NewDecoder(reader)

	// Responses are written from several goroutines — requests are handled
	// concurrently so a slow command cannot block a quick one — and a JSON
	// message must not be interleaved with another.
	var writeMu sync.Mutex
	encoder := json.NewEncoder(out)

	respond := func(response *protocol.Response) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = encoder.Encode(response)
	}

	var pending sync.WaitGroup
	for {
		var request protocol.Request
		if err := decoder.Decode(&request); err != nil {
			// Wait for work already in flight rather than exiting under it:
			// a command that has started should finish, and its output is
			// lost anyway once we are gone.
			pending.Wait()
			return nil
		}

		pending.Add(1)
		go func(request protocol.Request) {
			defer pending.Done()
			respond(handle(&request))
		}(request)
	}
}

func handle(request *protocol.Request) *protocol.Response {
	response := &protocol.Response{ID: request.ID}

	switch request.Method {
	case protocol.MethodPing:
		response.Result = json.RawMessage(`{}`)

	case protocol.MethodVersion:
		result, err := json.Marshal(protocol.VersionResult{
			Protocol: protocol.Version,
			Build:    Build,
			Arch:     runtime.GOOS + "/" + runtime.GOARCH,
		})
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.Result = result

	case protocol.MethodRun:
		result, err := runCommand(request.Params)
		if err != nil {
			response.Error = err.Error()
			return response
		}
		response.Result = result

	default:
		response.Error = fmt.Sprintf("unknown method %q — the app and this helper "+
			"are different versions", request.Method)
	}
	return response
}

func runCommand(raw json.RawMessage) (json.RawMessage, error) {
	var params protocol.RunParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("bad parameters: %w", err)
	}
	if params.Command == "" {
		return nil, fmt.Errorf("no command given")
	}

	timeout := defaultCommandTimeout
	if params.TimeoutMs > 0 {
		timeout = time.Duration(params.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Through a login shell: this is the whole reason a remote agent goes
	// missing. `ssh host cmd` runs a non-interactive shell, which reads no
	// profile, so an agent installed in ~/.local/bin or through nvm is on the
	// PATH when the user logs in and absent when we run it.
	command := exec.CommandContext(ctx, "/bin/sh", "-lc", params.Command)
	if params.Dir != "" {
		command.Dir = params.Dir
	}

	// The timeout has to reach whatever the shell started, not just the shell.
	killWholeGroup(command)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	result := protocol.RunResult{Output: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if asExitError(err, &exitErr) {
			// A non-zero exit is an answer, not a fault: `command -v claude`
			// failing is how we learn the agent is not installed.
			result.ExitCode = exitErr.ExitCode()
		} else if !result.TimedOut {
			return nil, err
		}
	}

	if len(result.Output) > protocol.MaxMessageBytes {
		result.Output = result.Output[:protocol.MaxMessageBytes]
	}
	// stderr is the lesser of the two, and a runaway command can fill it just
	// as fast; capped well below the message limit so it cannot crowd out the
	// output the caller actually asked for.
	if len(result.Stderr) > 64<<10 {
		result.Stderr = result.Stderr[:64<<10]
	}
	return json.Marshal(result)
}

// asExitError is errors.As, spelled out to keep the import list of this file
// to what a reader needs.
func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}
