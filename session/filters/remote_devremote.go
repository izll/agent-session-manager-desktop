//go:build devremote

package filters

// Dev-only override. Activated with: wails build -tags "webkit2_41,devremote"
//
// Points the remote filter loader at a local HTTPS test server with a
// self-signed cert, and adds localhost / 127.0.0.1 to the host allowlist.
// Production builds never see this — the file is gated behind the
// `devremote` build tag.
//
// Usage:
//   1. Run scripts/test-remote-filters.sh to start the local server.
//   2. wails build -tags "webkit2_41,devmode,devremote" -devtools
//   3. Launch the dev binary; check ~/.cache/agent-session-manager/filters-remote.json
//      after ~10 seconds.

func init() {
	RemoteFiltersURL = "https://127.0.0.1:18443/v1.json"
	RemoteFiltersHostAllowlist = []string{
		"127.0.0.1",
		"localhost",
	}
	remoteTLSInsecure = true // accept the self-signed cert from the test server
}
