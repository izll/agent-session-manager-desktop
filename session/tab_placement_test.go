package session

import "testing"

func TestTabPlacement(t *testing.T) {
	inst := &Instance{Path: "/work/app", FollowedWindows: []FollowedWindow{
		{Index: 1, Name: "own dir", WorkDir: "/work/app/docs"},
		{Index: 2, Name: "session dir"},
		{Index: 100, Name: "on a server", ServerID: "srv1", WorkDir: "/srv/app"},
		{Index: 101, Name: "on a server, session dir", ServerID: "srv1"},
	}}
	cases := []struct {
		window          int
		server, workDir string
	}{
		{0, "", "/work/app"}, // the session's main window
		{1, "", "/work/app/docs"},
		{2, "", "/work/app"},
		{100, "srv1", "/srv/app"},
		{101, "srv1", "/work/app"},
		{7, "", "/work/app"}, // a window the session does not know
	}
	for _, c := range cases {
		server, workDir := inst.TabPlacement(c.window)
		if server != c.server || workDir != c.workDir {
			t.Errorf("window %d: placed on %q in %q, want %q in %q",
				c.window, server, workDir, c.server, c.workDir)
		}
	}
}
