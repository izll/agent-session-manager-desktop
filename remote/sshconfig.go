package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Reading the machines already described in ~/.ssh/config.
//
// Anyone who uses SSH daily has their servers written down there once already,
// with the host, the port, the user and the key. Retyping all of that into
// another form is the sort of small friction that makes a feature feel like
// work, so the entries are offered as a starting point.
//
// Offered, not imported: the file is read, never written. What ends up in the
// server list is what the user picked and confirmed.

// SSHConfigHost is one entry from the file.
type SSHConfigHost struct {
	// Alias is the name after "Host" — what the user types after ssh.
	Alias string `json:"alias"`
	// HostName is the address to connect to, which defaults to the alias when
	// the entry does not give one.
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	KeyPath  string `json:"keyPath"`
	// ProxyJump names another entry to connect through.
	ProxyJump string `json:"proxyJump"`
}

// ReadSSHConfig returns the hosts described in the user's SSH configuration.
//
// A missing file is not an error: plenty of people have none, and the caller
// shows an empty list rather than a failure.
func ReadSSHConfig() ([]SSHConfigHost, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return readSSHConfigFile(filepath.Join(home, ".ssh", "config"), 0)
}

// maxIncludeDepth stops a file that includes itself, directly or in a ring.
const maxIncludeDepth = 4

func readSSHConfigFile(path string, depth int) ([]SSHConfigHost, error) {
	if depth > maxIncludeDepth {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var hosts []SSHConfigHost
	var current *SSHConfigHost

	// A directive belongs to the Host block above it, so the parse is a walk
	// with one open entry at a time.
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		keyword, value, ok := splitDirective(scanner.Text())
		if !ok {
			continue
		}

		switch keyword {
		case "host":
			if current != nil {
				hosts = append(hosts, *current)
				current = nil
			}
			alias := firstUsableAlias(value)
			if alias == "" {
				// A pattern like "Host *" sets defaults for everything rather
				// than naming a machine; there is nothing to connect to.
				continue
			}
			current = &SSHConfigHost{Alias: alias}

		case "include":
			// Included files are read in place, because a machine described in
			// one is as real as any other.
			for _, included := range expandInclude(value) {
				more, err := readSSHConfigFile(included, depth+1)
				if err != nil {
					continue
				}
				hosts = append(hosts, more...)
			}

		case "hostname":
			if current != nil {
				current.HostName = value
			}
		case "user":
			if current != nil {
				current.User = value
			}
		case "port":
			if current != nil {
				current.Port = parsePort(value)
			}
		case "identityfile":
			if current != nil && current.KeyPath == "" {
				// The first one only: an entry may list several, and the rest
				// are fallbacks we have no way to choose between.
				current.KeyPath = value
			}
		case "proxyjump":
			if current != nil {
				current.ProxyJump = value
			}
		}
	}
	if current != nil {
		hosts = append(hosts, *current)
	}
	if err := scanner.Err(); err != nil {
		return hosts, err
	}

	// An entry with no HostName connects to its own alias, which is how a
	// short name like "web" works at all.
	for index := range hosts {
		if hosts[index].HostName == "" {
			hosts[index].HostName = hosts[index].Alias
		}
	}
	return hosts, nil
}

// splitDirective breaks one line into its keyword and value.
//
// The format allows "Key value", "Key=value" and any amount of whitespace, and
// the keyword is case-insensitive — all three appear in configurations people
// actually have.
func splitDirective(line string) (keyword, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	if index := strings.IndexAny(trimmed, " \t="); index > 0 {
		keyword = strings.ToLower(trimmed[:index])
		value = strings.TrimSpace(strings.TrimLeft(trimmed[index:], " \t="))
		return keyword, value, value != ""
	}
	return "", "", false
}

// firstUsableAlias picks a name from a Host line.
//
// One line can name several aliases and patterns; the patterns are skipped
// because there is no single machine behind them.
func firstUsableAlias(value string) string {
	for _, candidate := range strings.Fields(value) {
		if strings.ContainsAny(candidate, "*?!") {
			continue
		}
		return candidate
	}
	return ""
}

func parsePort(value string) int {
	port := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0
		}
		port = port*10 + int(character-'0')
		if port > 65535 {
			return 0
		}
	}
	return port
}

// expandInclude resolves an Include line to the files it names.
func expandInclude(value string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var files []string
	for _, pattern := range strings.Fields(value) {
		// A relative include is relative to ~/.ssh, which is where the main
		// configuration lives.
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(home, ".ssh", strings.TrimPrefix(pattern, "~/"))
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}
	return files
}
