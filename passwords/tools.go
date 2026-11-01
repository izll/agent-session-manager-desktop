package passwords

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"asmgr-desktop/session"
)

const (
	keePassXCTool = "keepassxc-cli"
	bitwardenTool = "bw"
	// bitwardenPasswordEnv carries the master password to bw. Not an argument:
	// those can be read by every process on the machine.
	bitwardenPasswordEnv = "ASMGR_BW_PASSWORD"
	bitwardenSessionEnv  = "BW_SESSION"
	// bitwardenDataDirEnv picks the directory bw keeps a server and an account in.
	bitwardenDataDirEnv = "BITWARDENCLI_APPDATA_DIR"
	// bitwardenFlatpak is the desktop app's package, which carries a bw of its own
	// that is on no PATH. It keeps its own login, apart from the app's.
	bitwardenFlatpak = "com.bitwarden.desktop"
)

// findBitwarden says how to start bw here, or nil when it cannot be started.
func findBitwarden(ctx context.Context) []string {
	if toolInstalled(bitwardenTool) {
		return []string{bitwardenTool}
	}
	if !toolInstalled("flatpak") {
		return nil
	}
	check := exec.CommandContext(ctx, "flatpak", "info", bitwardenFlatpak)
	session.HideConsoleWindow(check)
	if check.Run() != nil {
		return nil
	}
	return []string{"flatpak", "run", "--command=" + bitwardenTool, bitwardenFlatpak}
}

// Runner runs a tool with text on its standard input. A variable shape so tests can
// stand in for a tool that is not installed.
type Runner func(ctx context.Context, stdin string, env []string, name string, args ...string) (stdout, stderr []byte, err error)

func runCommand(ctx context.Context, stdin string, env []string, name string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	session.HideConsoleWindow(command)
	command.Env = append(os.Environ(), env...)
	command.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func toolInstalled(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// toolError logs that a tool failed and how it exited, not what it printed: output
// of a password tool is not something to keep in a log file.
func toolError(tool, action string, err error) error {
	log.Printf("[passwords] %s %s failed: %v", tool, action, err)
	return ErrToolFailed
}

// --- KeePassXC ---

// keePassXCEnv asks for its fixed words in English, since the list is parsed.
var keePassXCEnv = []string{"LC_ALL=C.UTF-8", "LANGUAGE=en"}

type keePassXC struct {
	run      Runner
	database string
	keyFile  string
	master   string
	open     bool
}

func (k *keePassXC) configure(database, keyFile string) {
	database, keyFile = expandHome(database), expandHome(keyFile)
	if database != k.database || keyFile != k.keyFile {
		k.lock()
	}
	k.database, k.keyFile = database, keyFile
}

// expandHome reads a typed ~/ path: the tool gets the path as it is, with no shell
// in between to expand it.
func expandHome(file string) string {
	if !strings.HasPrefix(file, "~/") {
		return file
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return file
	}
	return home + file[1:]
}

func (k *keePassXC) lock() {
	k.master, k.open = "", false
}

func (k *keePassXC) unlocked() bool { return k.open }

func (k *keePassXC) listWith(ctx context.Context, master string) ([]Entry, error) {
	args := []string{"ls", "-q", "-R", "-f"}
	if k.keyFile != "" {
		args = append(args, "-k", k.keyFile)
	}
	args = append(args, "--", k.database)
	stdout, _, err := k.run(ctx, master+"\n", keePassXCEnv, keePassXCTool, args...)
	if err != nil {
		// keepassxc-cli gives no way to tell these apart: -q leaves it silent, and
		// without -q its message is in the system's language. So a tool that ran on a
		// file that exists and said no is reported as what it nearly always is.
		var notRun *exec.Error
		if errors.As(err, &notRun) || ctx.Err() != nil {
			return nil, toolError(keePassXCTool, "ls", err)
		}
		if _, statErr := os.Stat(k.database); statErr != nil {
			return nil, toolError(keePassXCTool, "ls", statErr)
		}
		return nil, ErrWrongMaster
	}
	return parseKeePassList(string(stdout)), nil
}

// parseKeePassList reads `ls -R -f`: one full path a line, a group ending in a slash.
func parseKeePassList(output string) []Entry {
	entries := []Entry{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasSuffix(line, "/") || strings.HasPrefix(line, "Recycle Bin/") {
			continue
		}
		// How it marks an empty group, in English thanks to keePassXCEnv.
		if path.Base(line) == "[empty]" {
			continue
		}
		group := path.Dir(line)
		if group == "." {
			group = ""
		}
		entries = append(entries, Entry{Source: SourceKeePassXC, ID: line, Name: path.Base(line), Group: group})
	}
	return entries
}

func (k *keePassXC) unlock(ctx context.Context, master string) ([]Entry, error) {
	entries, err := k.listWith(ctx, master)
	if err != nil {
		return nil, err
	}
	k.master, k.open = master, true
	return entries, nil
}

func (k *keePassXC) list(ctx context.Context) ([]Entry, error) {
	if !k.open {
		return nil, ErrLocked
	}
	entries, err := k.listWith(ctx, k.master)
	if errors.Is(err, ErrWrongMaster) {
		k.lock()
	}
	return entries, err
}

func (k *keePassXC) secret(ctx context.Context, id string) (string, error) {
	if !k.open {
		return "", ErrLocked
	}
	args := []string{"show", "-q", "-s", "-a", "Password"}
	if k.keyFile != "" {
		args = append(args, "-k", k.keyFile)
	}
	args = append(args, "--", k.database, id)
	stdout, _, err := k.run(ctx, k.master+"\n", keePassXCEnv, keePassXCTool, args...)
	if err != nil {
		// The database may have changed under an open source: its password, so the
		// remembered one no longer opens it, or this entry, now gone.
		if _, listErr := k.listWith(ctx, k.master); errors.Is(listErr, ErrWrongMaster) {
			k.lock()
			return "", ErrWrongMaster
		}
		log.Printf("[passwords] %s show failed: %v", keePassXCTool, err)
		return "", ErrNotFound
	}
	return strings.TrimRight(string(stdout), "\r\n"), nil
}

// --- Bitwarden ---

type bitwarden struct {
	run     Runner
	session string
	// The account: which one, and where the settings are that its folder goes by.
	id           string
	source       string
	settingsPath string
	// find stands in for findBitwarden in tests, whatever this machine has installed.
	find func(context.Context) []string
}

func (b *bitwarden) lock() { b.session = "" }

func (b *bitwarden) command(ctx context.Context) []string {
	if b.find != nil {
		return b.find(ctx)
	}
	return findBitwarden(ctx)
}

func (b *bitwarden) call(ctx context.Context, env []string, args ...string) ([]byte, []byte, error) {
	command := b.command(ctx)
	if command == nil {
		return nil, nil, errors.New("not installed")
	}
	env = append(env[:len(env):len(env)], bitwardenDataDirEnv+"="+bitwardenDataDir(b.settingsPath, b.id, command))
	return b.run(ctx, "", env, command[0], append(command[1:len(command):len(command)], args...)...)
}

// bitwardenLoginLine is what to type at a shell to log in to one account. Logging in
// can take a second factor of many kinds, so bw asks for them itself, in a terminal. It
// prints a session key when done, which nothing here needs and which would stay in the
// scrollback, so that goes nowhere; its questions and errors are on the other stream.
// Once it has worked the shell leaves, which is how the app knows to close the tab; on
// a failure it stays, with the reason on the screen.
//
// Windows has no one way to say any of this across its shells, so cmd says it, --quiet
// has to do for the key, and the tab is left for the user to close.
func bitwardenLoginLine(command []string, dataDir, goos string) string {
	line := strings.Join(append(command[:len(command):len(command)], "login"), " ")
	if goos == "windows" {
		return `cmd /c "set "` + bitwardenDataDirEnv + `=` + dataDir + `"&& ` + line + ` --quiet"`
	}
	return "env " + bitwardenDataDirEnv + "=" + shellQuote(dataDir) + " " + line + " > /dev/null && exit"
}

func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// bitwardenDataDir is where one account's bw keeps its state. bw knows one server and
// one account per directory, which is why each has its own. A Flatpak's bw can only
// write inside the package's own folder.
func bitwardenDataDir(settingsPath, id string, command []string) string {
	if len(command) > 1 {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".var", "app", bitwardenFlatpak, "config", "asmgr-accounts", id)
		}
	}
	return filepath.Join(filepath.Dir(settingsPath), "bitwarden", id)
}

// lastLine skips what a first run prints before its answer, about files it created.
func lastLine(output []byte) []byte {
	trimmed := bytes.TrimSpace(output)
	return trimmed[bytes.LastIndexByte(trimmed, '\n')+1:]
}

func (b *bitwarden) unlocked() bool { return b.session != "" }

func (b *bitwarden) sessionEnv() []string {
	return []string{bitwardenSessionEnv + "=" + b.session}
}

func (b *bitwarden) unlock(ctx context.Context, master string) ([]Entry, error) {
	stdout, _, err := b.call(ctx, nil, "status", "--nointeraction")
	if err != nil {
		return nil, toolError(bitwardenTool, "status", err)
	}
	var status struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(lastLine(stdout), &status) != nil {
		return nil, toolError(bitwardenTool, "status", errors.New("unreadable answer"))
	}
	// Logging in can take a second factor, which is the user's to do in a terminal.
	if status.Status == "unauthenticated" {
		return nil, ErrNotLoggedIn
	}

	stdout, stderr, err := b.call(ctx, []string{bitwardenPasswordEnv + "=" + master},
		"unlock", "--nointeraction", "--raw", "--passwordenv", bitwardenPasswordEnv)
	if err != nil {
		if strings.Contains(strings.ToLower(string(stderr)), "invalid master password") {
			return nil, ErrWrongMaster
		}
		return nil, toolError(bitwardenTool, "unlock", err)
	}
	key := strings.TrimSpace(string(stdout))
	if key == "" {
		return nil, ErrWrongMaster
	}
	previous := b.session
	b.session = key
	entries, err := b.list(ctx)
	if err != nil {
		b.session = previous
		return nil, err
	}
	return entries, nil
}

func (b *bitwarden) list(ctx context.Context) ([]Entry, error) {
	if !b.unlocked() {
		return nil, ErrLocked
	}
	stdout, _, err := b.call(ctx, b.sessionEnv(), "list", "items", "--nointeraction")
	if err != nil {
		return nil, toolError(bitwardenTool, "list", err)
	}
	return parseBitwardenList(b.source, stdout)
}

// parseBitwardenList takes names only. The answer holds every password of the vault,
// and this struct has nowhere to put them, so they are gone with the bytes.
func parseBitwardenList(source string, output []byte) ([]Entry, error) {
	var items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Type  int    `json:"type"`
		Login *struct {
			Username string `json:"username"`
		} `json:"login"`
	}
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, toolError(bitwardenTool, "list", errors.New("unreadable answer"))
	}
	entries := []Entry{}
	for _, item := range items {
		// Type 1 is a login; notes and cards have no password to type.
		if item.Type != 1 || item.Login == nil || item.ID == "" {
			continue
		}
		entries = append(entries, Entry{Source: source, ID: item.ID, Name: item.Name, Username: item.Login.Username})
	}
	return entries, nil
}

func (b *bitwarden) secret(ctx context.Context, id string) (string, error) {
	if !b.unlocked() {
		return "", ErrLocked
	}
	stdout, _, err := b.call(ctx, b.sessionEnv(), "get", "password", id, "--nointeraction")
	if err != nil {
		return "", toolError(bitwardenTool, "get", err)
	}
	return strings.TrimRight(string(stdout), "\r\n"), nil
}
