package session

import (
	"strings"
	"testing"
)

// A session ID becomes the multiplexer's session name, and targets are built
// as "session:window". Anything the multiplexer reads as punctuation has to be
// gone before it gets there.
func TestSanitizeSessionName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			// The case that prompted this: on Windows a session is easily named
			// after its directory, and both the colon and the backslashes would
			// have been passed straight through to tmux.
			name: "windows path",
			in:   `C:\Users\User\Documents\asmgr-teszt`,
			want: "c_users_user_documents_asmgr-teszt",
		},
		{
			name: "unix path",
			in:   "/home/izll/NetBeansProjects/asmgr-desktop",
			want: "home_izll_netbeansprojects_asmgr-desktop",
		},
		{
			name: "spaces, as before",
			in:   "my project",
			want: "my_project",
		},
		{
			// A trailing ".1" would read as a pane index.
			name: "dots",
			in:   "v1.2.3",
			want: "v1_2_3",
		},
		{
			name: "runs collapse rather than piling up",
			in:   "a // b",
			want: "a_b",
		},
		{
			name: "leading and trailing punctuation is dropped",
			in:   "  ...name...  ",
			want: "name",
		},
		{
			name: "accented letters do not survive as themselves",
			in:   "árvíztűrő",
			want: "rv_zt_r",
		},
		{
			name: "hyphens are kept, being harmless",
			in:   "asmgr-desktop",
			want: "asmgr-desktop",
		},
		{
			name: "a name of nothing but punctuation empties out",
			in:   `\\:://`,
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeSessionName(c.in); got != c.want {
				t.Errorf("sanitizeSessionName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Whatever the name, the generated ID must be safe to use as a target.
func TestGenerateIDIsTargetSafe(t *testing.T) {
	for _, name := range []string{
		`C:\Users\User\Documents\asmgr-teszt`,
		"v1.2.3",
		"árvíztűrő tükörfúrógép",
		`\\:://`,
	} {
		id := generateID(name, AgentClaude)
		for _, bad := range []string{":", ".", `\`, "/", " "} {
			if strings.Contains(id, bad) {
				t.Errorf("generateID(%q) = %q, which still contains %q", name, id, bad)
			}
		}
		// Even a name that sanitises to nothing has to produce a usable ID.
		if id == "" {
			t.Errorf("generateID(%q) produced an empty ID", name)
		}
	}
}
