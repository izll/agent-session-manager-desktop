package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestMCPTransportLogsDoNotDumpMessageContent(t *testing.T) {
	raw, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{
		`string(data)`,
		`received: %s`,
		`non-JSON): %s`,
		`MCP stderr: %s`,
		`responding: %s`,
		`responding error: %s`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("MCP transport still logs message content via %q", forbidden)
		}
	}
}
