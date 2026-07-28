//go:build windows

package session

// tmux has no native Windows build. psmux is a native, tmux-compatible
// multiplexer answering the same subcommands, so a Windows build looks for it
// instead. SetTmuxBinary still overrides this for anyone running tmux through
// WSL or Cygwin.
const defaultTmuxBinary = "psmux"
