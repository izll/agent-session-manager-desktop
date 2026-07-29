package main

import "testing"

// The flag has to work from both a shortcut (env) and a terminal (argument):
// a desktop app is often started by neither a shell nor with arguments.
func TestResolveDebugFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string
		want bool
	}{
		{name: "off by default", args: nil, env: "", want: false},
		{name: "--debug", args: []string{"--debug"}, want: true},
		{name: "-debug", args: []string{"-debug"}, want: true},
		{name: "--debug=true", args: []string{"--debug=true"}, want: true},
		{name: "flag among others", args: []string{"--other", "--debug", "x"}, want: true},
		{name: "case insensitive", args: []string{"--DEBUG"}, want: true},
		{name: "env 1", env: "1", want: true},
		{name: "env true", env: "true", want: true},
		{name: "env yes", env: "yes", want: true},
		{name: "env on", env: "on", want: true},
		{name: "env padded", env: "  1  ", want: true},
		{name: "env 0 stays off", env: "0", want: false},
		{name: "env empty stays off", env: "", want: false},
		{name: "env nonsense stays off", env: "maybe", want: false},
		// A substring must not enable it: --debugger is not --debug, and a path
		// that happens to contain the word is not a request for diagnostics.
		{name: "unrelated flag stays off", args: []string{"--debugger"}, want: false},
		{name: "path containing debug stays off", args: []string{"/home/u/debug/app"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveDebugFlag(tt.args, tt.env); got != tt.want {
				t.Fatalf("resolveDebugFlag(%q, %q) = %v, want %v", tt.args, tt.env, got, tt.want)
			}
		})
	}
}
