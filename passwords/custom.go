package passwords

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"asmgr-desktop/session"

	"github.com/google/uuid"
)

// A source the user defines with two commands, for a password manager this package
// has no code for. One prints a name per line, the other the password of a name.
//
// The commands run without a shell, each word one argument, and a name from the list
// can only ever be one whole argument. A name is the tool's data, possibly synced from
// elsewhere, and must not get to be part of a command. Unlocking is the tool's own
// business: nothing is asked for here, and nothing is written to its standard input.

const (
	customSourcePrefix = "custom:"
	// customNamePlaceholder is replaced by the name, as a whole argument only.
	customNamePlaceholder = "{name}"
	// customNameEnv carries the name too, for a command that goes through a shell.
	customNameEnv = "ASMGR_SECRET_NAME"

	customListTimeout = 30 * time.Second
	// customGetTimeout leaves time to answer the tool's own prompt or touch a sensor.
	customGetTimeout = 2 * time.Minute
	customListLimit  = 4 << 20
	customGetLimit   = 64 << 10
)

// customPipeGrace bounds waiting for a pipe left open after the command has exited,
// which is what a helper daemon the command started does. A variable for tests.
var customPipeGrace = 2 * time.Second

// ErrBadCommand refuses a command that could not safely be run.
var ErrBadCommand = errors.New("error.passwordBadCommand")

// CustomSource is one such source as saved.
type CustomSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ListCommand string `json:"list_command"`
	GetCommand  string `json:"get_command"`
}

func (c CustomSource) source() string { return customSourcePrefix + c.ID }

// splitCommand reads a typed command into arguments: words, with single or double
// quotes keeping spaces together. Nothing else is special, so a Windows path survives.
func splitCommand(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, ErrBadCommand
	}
	if inWord {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return nil, ErrBadCommand
	}
	return words, nil
}

// checkCustomCommands refuses what would misbehave only later. The name inside a
// longer argument is the one that matters: in sh -c 'show {name}' it would be code.
// A password command that takes no name at all would type the same one for every
// entry, so it has to mention the name one way or the other.
func checkCustomCommands(list, get string) error {
	listArgs, err := splitCommand(list)
	if err != nil {
		return err
	}
	if strings.Contains(strings.Join(listArgs, " "), customNamePlaceholder) {
		return ErrBadCommand
	}
	getArgs, err := splitCommand(get)
	if err != nil {
		return err
	}
	if getArgs[0] == customNamePlaceholder {
		return ErrBadCommand
	}
	named := false
	for _, arg := range getArgs {
		if arg == customNamePlaceholder || strings.Contains(arg, customNameEnv) {
			named = true
		}
		if arg != customNamePlaceholder && strings.Contains(arg, customNamePlaceholder) {
			return ErrBadCommand
		}
	}
	if !named {
		return ErrBadCommand
	}
	return nil
}

// goesThroughCmd says Windows would hand this program to cmd.exe, whose own quoting
// rules no argument list can be safe against.
func goesThroughCmd(program string) bool {
	extension := strings.ToLower(filepath.Ext(program))
	return extension == ".bat" || extension == ".cmd"
}

func customGetArgs(command, name, goos string) ([]string, error) {
	args, err := splitCommand(command)
	if err != nil {
		return nil, err
	}
	if goos == "windows" {
		if program, err := exec.LookPath(args[0]); err != nil || goesThroughCmd(program) {
			return nil, ErrBadCommand
		}
	}
	for i, arg := range args {
		if arg == customNamePlaceholder {
			args[i] = name
		}
	}
	return args, nil
}

// limitedOutput stops the command once it has written more than limit: a tool asked
// for one password has no business filling memory.
type limitedOutput struct {
	limit    int
	data     []byte
	overflow bool
	stop     context.CancelFunc
}

func (l *limitedOutput) Write(p []byte) (int, error) {
	if l.overflow || len(l.data)+len(p) > l.limit {
		l.overflow = true
		l.data = nil
		l.stop()
		return len(p), nil
	}
	l.data = append(l.data, p...)
	return len(p), nil
}

// runCustomCommand runs a command and returns what it printed, or nothing if it
// failed. Its error output and the output of a failure stay unread: they can hold
// anything, and an error here could end up in a log.
func runCustomCommand(ctx context.Context, limit int, env, args []string) ([]byte, error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	output := &limitedOutput{limit: limit, stop: stop}
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	session.HideConsoleWindow(command)
	stopWholeProcessGroup(command)
	command.WaitDelay = customPipeGrace
	command.Env = append(os.Environ(), env...)
	command.Stdout = output
	err := command.Run()
	if output.overflow {
		return nil, toolError(args[0], "custom", errors.New("too much output"))
	}
	// The command itself succeeded; a helper it left running holds the pipe.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, toolError(args[0], "custom", err)
	}
	return output.data, nil
}

// parseCustomList takes one name a line. A name that starts with a dash would be read
// as an option, and a control character has no place in one, so neither is offered.
func parseCustomList(source string, output []byte) []Entry {
	entries := []Entry{}
	seen := map[string]bool{}
	skipped := 0
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !usableCustomName(line) {
			skipped++
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		entry := Entry{Source: source, ID: line, Name: line}
		if strings.Contains(line, "/") {
			entry.Name = path.Base(line)
			entry.Group = path.Dir(line)
		}
		entries = append(entries, entry)
	}
	if skipped > 0 {
		log.Printf("[passwords] %s: skipped %d unusable names", source, skipped)
	}
	return entries
}

func usableCustomName(name string) bool {
	if strings.HasPrefix(name, "-") || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (m *Manager) customSource(source string) (CustomSource, bool) {
	for _, custom := range m.cfg.Custom {
		if custom.source() == source {
			return custom, true
		}
	}
	return CustomSource{}, false
}

func customStatus(custom CustomSource) SourceStatus {
	status := SourceStatus{
		Source: custom.source(), Enabled: true,
		Name: custom.Name, ListCommand: custom.ListCommand, GetCommand: custom.GetCommand,
	}
	if args, err := splitCommand(custom.ListCommand); err == nil {
		status.Available = toolInstalled(args[0])
		status.Tool = args[0]
	}
	return status
}

// listCustom asks every custom source not yet asked, at once and without the manager's
// lock: one tool that hangs must not hold up the others or anything else in here.
func (m *Manager) listCustom(ctx context.Context, sources []CustomSource, refresh bool) ([]Entry, map[string]string) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	entries := []Entry{}
	failures := map[string]string{}
	for _, custom := range sources {
		source := custom.source()
		m.mu.Lock()
		cached, known := m.cache[source]
		m.mu.Unlock()
		if known && !refresh {
			entries = append(entries, cached...)
			continue
		}
		wg.Add(1)
		go func(custom CustomSource, source string) {
			defer wg.Done()
			fresh, err := listOneCustom(ctx, custom)
			if err == nil {
				m.mu.Lock()
				m.cache[source] = fresh
				m.armLock()
				m.mu.Unlock()
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures[source] = err.Error()
				return
			}
			entries = append(entries, fresh...)
		}(custom, source)
	}
	wg.Wait()
	return entries, failures
}

func listOneCustom(ctx context.Context, custom CustomSource) ([]Entry, error) {
	args, err := splitCommand(custom.ListCommand)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, customListTimeout)
	defer cancel()
	output, err := runCustomCommand(ctx, customListLimit, nil, args)
	if err != nil {
		return nil, err
	}
	return parseCustomList(custom.source(), output), nil
}

// customSecret fetches a password, outside the lock for the time it can take.
func (m *Manager) customSecret(ctx context.Context, source, id string) (string, error) {
	m.mu.Lock()
	_ = m.reload()
	custom, found := m.customSource(source)
	listed := found && m.listed(source, id)
	if listed {
		m.armLock()
	}
	m.mu.Unlock()
	if !listed || !usableCustomName(id) {
		return "", ErrNotFound
	}
	args, err := customGetArgs(custom.GetCommand, id, runtime.GOOS)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, customGetTimeout)
	defer cancel()
	output, err := runCustomCommand(ctx, customGetLimit, []string{customNameEnv + "=" + id}, args)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

// SaveCustom adds a source, or changes one when id is set.
func (m *Manager) SaveCustom(id, name, list, get string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if err := checkCustomCommands(list, get); err != nil {
		return "", err
	}
	if id == "" {
		id = uuid.NewString()
	}
	saved := CustomSource{ID: id, Name: name, ListCommand: strings.TrimSpace(list), GetCommand: strings.TrimSpace(get)}
	err := m.change(func() {
		delete(m.cache, saved.source())
		for i, custom := range m.cfg.Custom {
			if custom.ID == id {
				m.cfg.Custom[i] = saved
				return
			}
		}
		m.cfg.Custom = append(m.cfg.Custom, saved)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// DeleteCustom removes a source. What the tool itself stores stays where it is.
func (m *Manager) DeleteCustom(id string) error {
	return m.change(func() {
		for i, custom := range m.cfg.Custom {
			if custom.ID == id {
				delete(m.cache, custom.source())
				m.cfg.Custom = append(m.cfg.Custom[:i], m.cfg.Custom[i+1:]...)
				return
			}
		}
	})
}
