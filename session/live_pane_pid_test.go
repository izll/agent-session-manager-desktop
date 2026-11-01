package session

import "testing"

func TestParseLivePanePID(t *testing.T) {
	for _, c := range []struct {
		output string
		pid    int
		ok     bool
	}{
		{"0:1234\n", 1234, true},
		// The multiplexer did not say; that is not the same as dead.
		{":1234", 1234, true},
		// The pid of a process that has exited, and may be someone else's by now.
		{"1:1234", 0, false},
		{"0:", 0, false},
		{"0:0", 0, false},
		{"0:abc", 0, false},
		{"1234", 0, false},
		{"", 0, false},
	} {
		pid, ok := parseLivePanePID(c.output)
		if pid != c.pid || ok != c.ok {
			t.Errorf("parseLivePanePID(%q) = %d, %v; want %d, %v", c.output, pid, ok, c.pid, c.ok)
		}
	}
}
