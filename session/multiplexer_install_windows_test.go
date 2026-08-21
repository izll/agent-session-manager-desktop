//go:build windows

package session

import (
	"bytes"
	"strings"
	"testing"
)

func TestMultiplexerInstallOutputIsBounded(t *testing.T) {
	var output boundedInstallOutput
	payload := bytes.Repeat([]byte("x"), multiplexerInstallOutputLimit+4096)
	n, err := output.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(payload))
	}
	got := output.String()
	marker := "\n[output truncated]"
	if len(got) != multiplexerInstallOutputLimit+len(marker) {
		t.Fatalf("bounded output size = %d", len(got))
	}
	if !strings.HasSuffix(got, marker) {
		t.Fatal("bounded output does not report truncation")
	}
}
