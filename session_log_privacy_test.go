package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionLaunchLogsDoNotContainCommandsOrConversationIDs(t *testing.T) {
	files := []string{
		"app.go",
		filepath.Join("session", "instance.go"),
		filepath.Join("session", "codex_resume_detect.go"),
	}
	forbidden := []string{
		"final argv",
		"newExtraArgs=%q",
		"extraArgs=%q",
		"resumeID=%q",
		"ResumeSessionID=%q",
		"captured sessionID=%s",
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range forbidden {
			if strings.Contains(string(body), fragment) {
				t.Errorf("%s still logs sensitive launch content via %q", path, fragment)
			}
		}
	}
}
