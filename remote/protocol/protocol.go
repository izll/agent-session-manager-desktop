// Package protocol is the language the app and the helper speak.
//
// Shared by both sides so a change to a message cannot be made on one end
// only: the helper imports this, and so does the app that installs it.
//
// The transport is the SSH connection itself. The helper is started as
// `asmgrd --stdio` and talks over its own standard input and output, so there
// is no port to open, no second authentication, and no service to leave
// running: it has exactly the access of the account used to log in.
package protocol

import "encoding/json"

// Version is what the two ends must agree on.
//
// A plain integer, separate from the application's version: the app releases
// often and most releases change nothing here, and tying the two would mean
// re-uploading a binary over SSH for every patch. It is bumped when a message
// changes shape, and an exact match is required in both directions — an older
// helper is as wrong as an older app.
const Version = 1

// Method names. Constants because both ends switch on them, and a typo would
// otherwise surface as "unknown method" at run time.
const (
	// MethodPing answers immediately. Used to tell a live helper from a
	// connection that is open but wedged.
	MethodPing = "ping"
	// MethodVersion reports the helper's protocol version and build.
	MethodVersion = "version"
	// MethodRun executes one command and returns its output.
	MethodRun = "run"
)

// Request is one message from the app.
type Request struct {
	// ID pairs a response with its request. Several may be in flight at once —
	// that is the point of having an id rather than a strict turn-taking
	// protocol, since the sidebar asks about every session at the same time.
	ID int64 `json:"id"`
	// Method is one of the constants above.
	Method string `json:"method"`
	// Params carries the method's own arguments, decoded by the handler.
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one message from the helper.
type Response struct {
	ID int64 `json:"id"`
	// Result is the method's own answer; absent when Error is set.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is a human-readable failure. Not a code: every one of these ends
	// up in front of a person trying to work out what is wrong with their
	// server, and a number would have to be translated back into this anyway.
	Error string `json:"error,omitempty"`
}

// VersionResult answers MethodVersion.
type VersionResult struct {
	Protocol int    `json:"protocol"`
	Build    string `json:"build"`
	// Arch is what the helper was built for, so a mismatch with the machine is
	// visible rather than surfacing as "exec format error".
	Arch string `json:"arch"`
}

// RunParams asks for one command.
type RunParams struct {
	Command string `json:"command"`
	// Dir runs the command somewhere other than the home directory.
	Dir string `json:"dir,omitempty"`
	// TimeoutMs bounds it. Zero means the helper's own default: a command with
	// no limit at all can hold a connection open forever, and the app has no
	// way to cancel one it cannot see.
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// RunResult carries what the command produced.
type RunResult struct {
	// Output is what the command wrote to standard output.
	//
	// Kept apart from Stderr because the helper runs commands through a login
	// shell — which is what makes an agent under ~/.local/bin visible at all —
	// and a login shell is entitled to print things of its own. A .profile
	// saying "mesg: ttyname failed" is ordinary, and mixed into the output it
	// would corrupt every answer the app parses.
	Output string `json:"output"`
	// Stderr is kept for error messages, which are worth showing when a
	// command fails and worth ignoring when it succeeds.
	Stderr string `json:"stderr,omitempty"`
	// ExitCode is the command's own status. A non-zero code is not a protocol
	// error: `command -v claude` failing is an answer, not a fault.
	ExitCode int `json:"exitCode"`
	// TimedOut says the command was killed rather than finishing.
	TimedOut bool `json:"timedOut,omitempty"`
}

// MaxMessageBytes caps one message in either direction.
//
// A capture of a long scrollback or a large diff is legitimate traffic, so the
// limit is generous — but without one, a runaway command on the server would
// be read into the app's memory until it ran out.
const MaxMessageBytes = 32 << 20
