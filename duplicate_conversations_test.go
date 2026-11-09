package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyRecordsOfLiveProcessesCount(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("10.json", `{"pid":10,"sessionId":"aaa","procStart":"5"}`)
	write("11.json", `{"pid":11,"sessionId":"aaa","procStart":"6"}`)
	write("12.json", `{"pid":12,"sessionId":"bbb","procStart":"7"}`)
	// Its process is gone, or its number went to another one since.
	write("13.json", `{"pid":13,"sessionId":"aaa","procStart":"8"}`)
	write("bad.json", `{"pid":0,"sessionId":"aaa"}`)
	write("broken.json", `{`)
	write("14.key", `not a record`)
	alive := func(pid int, start string) bool { return pid != 13 }
	got := liveClaudeProcesses(dir, alive)
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestADuplicateIsNamedByItsTabs(t *testing.T) {
	processes := []claudeProcess{{10, "aaa"}, {11, "aaa"}, {12, "bbb"}, {13, "ccc"}, {14, "ccc"}}
	panes := parsePaneListing("100\twork_gui_2_99\t2\tweb\n" +
		"100\twork\t2\tweb\n" +
		"200\twork\t6\tcrawling\n" +
		"300\tother\t0\tclaude\n")
	if panes[100].session != "work" {
		t.Fatalf("the mirror's entry won: %+v", panes[100])
	}
	// Whichever comes first.
	if other := parsePaneListing("100\twork\t2\tweb\n100\twork_gui_2_99\t2\tweb\n"); other[100].session != "work" {
		t.Fatalf("the mirror's entry won: %+v", other[100])
	}
	parent := parseParents(" 10 100\n 11 50\n 50 200\n 12 300\n 13 1\n 14 1\n")
	places := placesOf([]int{10, 11, 13, 14}, panes, parent)
	describe := func(pane paneAt) tabPlace {
		place := tabPlace{name: pane.session + " › " + pane.name}
		if pane.session == "work" {
			place.sessionID = "work-id"
		}
		return place
	}
	got := findDuplicateConversations(processes, places, describe, "outside")
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].ConversationID != "aaa" || strings.Join(got[0].Places, ",") != "work › web,work › crawling" {
		t.Fatalf("aaa: %+v", got[0])
	}
	// The tabs to mark, by the base session's window index.
	if len(got[0].Tabs) != 2 || got[0].Tabs[0] != (DuplicateTab{"work-id", 2, 0}) || got[0].Tabs[1] != (DuplicateTab{"work-id", 6, 1}) {
		t.Fatalf("tabs: %+v", got[0].Tabs)
	}
	if len(got[1].Tabs) != 0 {
		t.Fatalf("tabs outside the app: %+v", got[1].Tabs)
	}
	// Claude started outside the app counts too, as it writes to the same history.
	if strings.Join(got[1].Places, ",") != "outside,outside" {
		t.Fatalf("ccc: %+v", got[1])
	}
}

func TestADuplicateIsReportedOnceUntilItChanges(t *testing.T) {
	first := []DuplicateConversation{{ConversationID: "a", Places: []string{"x", "y"}}}
	same := []DuplicateConversation{{ConversationID: "a", Places: []string{"x", "y"}}}
	moved := []DuplicateConversation{{ConversationID: "a", Places: []string{"x", "z"}}}
	if duplicatesKey(first) != duplicatesKey(same) || duplicatesKey(first) == duplicatesKey(moved) || duplicatesKey(nil) == duplicatesKey(first) {
		t.Fatal("keys")
	}
}

func TestAProcessIsKnownByWhenItStarted(t *testing.T) {
	self := os.Getpid()
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skip("no /proc here")
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if !processStillOriginal(self, fields[19]) {
		t.Fatal("this process was not recognised")
	}
	if processStillOriginal(self, "1") {
		t.Fatal("another start time passed")
	}
}
