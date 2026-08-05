//go:build windows

package session

// ShellChoice is one option offered for what a terminal tab runs.
type ShellChoice struct {
	// Command is stored in settings; empty means the system default.
	Command string `json:"command"`
	// Label names it in the interface. Empty means the frontend supplies a
	// translated "system default" label.
	Label string `json:"label"`
}

// ShellChoices lists the shells worth offering on this platform.
//
// Windows is where this matters: "the shell" is genuinely ambiguous, and the
// environment does not say which one the user wants. COMSPEC names cmd.exe,
// but PowerShell is what many Windows users actually work in — and neither is
// discoverable as a preference. So both are offered by name.
//
// powershell.exe rather than pwsh.exe: the former ships with Windows, while
// pwsh is a separate install. Someone who has it can type it in.
func ShellChoices() []ShellChoice {
	return []ShellChoice{
		{Command: "", Label: ""},
		{Command: "cmd.exe", Label: "Command Prompt"},
		{Command: "powershell.exe", Label: "PowerShell"},
	}
}
