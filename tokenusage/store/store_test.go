package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"asmgr-desktop/tokenusage"
)

const conversationA = "aaaaaaaa-0000-0000-0000-000000000001"
const conversationB = "bbbbbbbb-0000-0000-0000-000000000002"

func openTestStore(t *testing.T) *Store {
	t.Helper()
	opened, err := Open(filepath.Join(t.TempDir(), "usage.db"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

func claudeLine(messageID, requestID string, input, output, read, write int64) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":%q,"timestamp":"2026-10-20T10:00:00Z","cwd":"/work/app",`+
		`"message":{"id":%q,"model":"claude-opus-5-5",`+
		`"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		requestID, messageID, input, output, read, write) + "\n"
}

func codexLine(minute int, totalIn, totalOut, lastIn, lastOut int64) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-20T10:%02d:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{`+
		`"total_token_usage":{"input_tokens":%d,"output_tokens":%d},"last_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`,
		minute, totalIn, totalOut, lastIn, lastOut) + "\n"
}

// codexStart is a rollout's opening line. Token rows count only after the file's
// first pause, so it stands before them as it does in a real log.
const codexStart = `{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"cwd":"/work/app","source":"cli"}}` + "\n"

func write(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func add(t *testing.T, full, content string) {
	t.Helper()
	file, err := os.OpenFile(full, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func sync(t *testing.T, s *Store, root string) {
	t.Helper()
	source := LocalSource{Roots: tokenusage.Roots{Claude: filepath.Join(root, "claude"), Codex: filepath.Join(root, "codex")}}
	if err := s.Sync(context.Background(), LocalMachine, source, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

type totals struct{ input, output, read, write, responses int64 }

func sum(t *testing.T, s *Store) totals {
	t.Helper()
	items, err := s.Items(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var total totals
	for _, item := range items {
		total.input += item.Input
		total.output += item.Output
		total.read += item.CacheRead
		total.write += item.CacheWrite
		total.responses += item.Responses
	}
	return total
}

func claudePath(conversation string) string {
	return "claude/projects/-work-app/" + conversation + ".jsonl"
}

func TestARepeatedResponseCountsOnce(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA),
		claudeLine("m1", "r1", 2, 30, 400, 5)+claudeLine("m1", "r1", 2, 30, 400, 5)+claudeLine("m2", "r2", 1, 10, 100, 0))
	sync(t, s, root)
	if got := sum(t, s); got != (totals{3, 40, 500, 5, 2}) {
		t.Fatalf("got %+v", got)
	}
}

// The first rows of a response carry a placeholder output, and the real figure
// arrives on a later one, possibly in a later scan.
func TestAnOutputThatGrowsAddsOnlyTheDifference(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	full := write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 1, 400, 5))
	sync(t, s, root)
	add(t, full, claudeLine("m1", "r1", 2, 90, 400, 5))
	sync(t, s, root)
	if got := sum(t, s); got != (totals{2, 90, 400, 5, 1}) {
		t.Fatalf("got %+v", got)
	}
}

// Seen in real logs: the last row of a response is not always the largest.
func TestASmallerLaterOutputDoesNotLowerIt(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 90, 0, 0)+claudeLine("m1", "r1", 2, 1, 0, 0))
	sync(t, s, root)
	if got := sum(t, s); got.output != 90 {
		t.Fatalf("got %+v", got)
	}
}

// Resuming a conversation copies earlier responses into a new file.
func TestAResumedCopyInAnotherFileCountsOnce(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 30, 400, 5))
	write(t, root, claudePath(conversationB), claudeLine("m1", "r1", 2, 30, 400, 5)+claudeLine("m2", "r2", 1, 1, 1, 1))
	sync(t, s, root)
	if got := sum(t, s); got != (totals{3, 31, 401, 6, 2}) {
		t.Fatalf("got %+v", got)
	}
}

// Whichever file is read first gets the credit, and it belongs to the original.
func TestAResumedCopyIsCreditedToTheOriginal(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	original := write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 30, 400, 5))
	copied := write(t, root, claudePath(conversationB), claudeLine("m1", "r1", 2, 30, 400, 5))
	old, recent := time.Now().Add(-time.Hour), time.Now()
	if err := os.Chtimes(original, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(copied, recent, recent); err != nil {
		t.Fatal(err)
	}
	sync(t, s, root)
	items, err := s.Items(context.Background(), 0)
	if err != nil || len(items) != 1 || items[0].Conversation != conversationA {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
}

func TestAScanWithNothingNewChangesNothing(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 30, 400, 5))
	write(t, root, "codex/sessions/rollout-"+conversationB+".jsonl", codexStart+codexLine(1, 100, 10, 100, 10))
	sync(t, s, root)
	first := sum(t, s)
	sync(t, s, root)
	sync(t, s, root)
	if got := sum(t, s); got != first {
		t.Fatalf("got %+v, want %+v", got, first)
	}
}

// What was counted stays counted: Claude Code deletes transcripts after 30 days.
func TestADeletedLogKeepsItsNumbers(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	full := write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 2, 30, 400, 5))
	sync(t, s, root)
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	sync(t, s, root)
	if got := sum(t, s); got.output != 30 {
		t.Fatalf("got %+v", got)
	}
}

// Archiving a Codex conversation moves its rollout. Its rows carry no key, so
// reading it again from the start would count all of it a second time.
func TestAMovedCodexRolloutIsNotCountedAgain(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	name := "rollout-" + conversationA + ".jsonl"
	from := write(t, root, "codex/sessions/2026/10/20/"+name, codexStart+codexLine(1, 100, 10, 100, 10))
	sync(t, s, root)

	to := filepath.Join(root, "codex", "archived_sessions", name)
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	sync(t, s, root)
	if got := sum(t, s); got.input != 100 || got.output != 10 {
		t.Fatalf("got %+v", got)
	}

	// And it goes on being read where it now is.
	add(t, to, codexLine(2, 150, 15, 50, 5))
	sync(t, s, root)
	if got := sum(t, s); got.input != 150 || got.output != 15 {
		t.Fatalf("after growing: %+v", got)
	}
}

// A shorter file was rewritten. Claude's rows are recognised by their key.
func TestARewrittenClaudeLogIsReadAgainWithoutDoubling(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA),
		claudeLine("m1", "r1", 2, 30, 400, 5)+claudeLine("m2", "r2", 1, 1, 1, 1))
	sync(t, s, root)
	write(t, root, claudePath(conversationA), claudeLine("m3", "r3", 7, 7, 7, 7))
	sync(t, s, root)
	if got := sum(t, s); got != (totals{10, 38, 408, 13, 3}) {
		t.Fatalf("got %+v", got)
	}
}

// Codex rows carry no key, so a rollout that shrank must not be read again.
func TestAShrunkCodexRolloutIsNotCountedAgain(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	rel := "codex/sessions/rollout-" + conversationA + ".jsonl"
	write(t, root, rel, codexStart+codexLine(1, 100, 10, 100, 10)+codexLine(2, 200, 20, 100, 10))
	sync(t, s, root)
	write(t, root, rel, codexStart)
	sync(t, s, root)
	if got := sum(t, s); got.input != 200 {
		t.Fatalf("got %+v", got)
	}
}

// Two app windows can scan at once. The second to write finds the cursor moved
// and drops its copy, which matters for Codex rows, having no key to catch them.
func TestAReadAnotherWindowGotToFirstIsDropped(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	rel := "sessions/rollout-" + conversationA + ".jsonl"
	write(t, root, "codex/"+rel, codexStart+codexLine(1, 100, 10, 100, 10))

	roots := tokenusage.Roots{Codex: filepath.Join(root, "codex")}
	read := func() Applied {
		results := tokenusage.ReadBatch(roots, []tokenusage.ReadRequest{{Path: "codex:" + rel}}, tokenusage.Limits{})
		return Applied{FileID: "codex:rollout-" + conversationA + ".jsonl", Size: 1, ExpectedOffset: 0, Result: results[0]}
	}
	first, second := read(), read()
	ctx := context.Background()
	if err := s.Apply(ctx, LocalMachine, []Applied{first}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, LocalMachine, []Applied{second}); err != nil {
		t.Fatal(err)
	}
	if got := sum(t, s); got.input != 100 {
		t.Fatalf("got %+v", got)
	}
}

func TestTwoStoresOnOneFileWaitForEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	root := t.TempDir()
	for i := range 40 {
		write(t, root, claudePath(fmt.Sprintf("%036d", i)), claudeLine(fmt.Sprintf("m%d", i), "r", 1, 1, 1, 1))
	}
	first, err := Open(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	source := LocalSource{Roots: tokenusage.Roots{Claude: filepath.Join(root, "claude")}}
	errs := make(chan error, 2)
	for _, s := range []*Store{first, second} {
		go func() { errs <- s.Sync(context.Background(), LocalMachine, source, nil) }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("sync: %v", err)
		}
	}
	if got := sum(t, first); got.output != 40 || got.responses != 40 {
		t.Fatalf("got %+v", got)
	}
}

func TestMachinesAreKeptApart(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 1, 1, 1, 1))
	source := LocalSource{Roots: tokenusage.Roots{Claude: filepath.Join(root, "claude")}}
	ctx := context.Background()
	for _, machine := range []string{LocalMachine, "server-1"} {
		if err := s.Sync(ctx, machine, source, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.Items(ctx, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
}

func TestAPeriodStartsOnItsDay(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	write(t, root, claudePath(conversationA), claudeLine("m1", "r1", 1, 1, 1, 1))
	sync(t, s, root)
	ctx := context.Background()
	if items, _ := s.Items(ctx, 20261020); len(items) != 1 {
		t.Fatalf("the day itself was left out: %+v", items)
	}
	if items, _ := s.Items(ctx, 20261021); len(items) != 0 {
		t.Fatalf("an earlier day was included: %+v", items)
	}
}

// A day is the viewer's day: 23:30 UTC is already tomorrow in Budapest.
func TestADayFollowsTheLocalCalendar(t *testing.T) {
	moment := time.Date(2026, 10, 20, 23, 30, 0, 0, time.UTC).Unix()
	if got := dayOf(moment, time.UTC); got != 20261020 {
		t.Fatalf("UTC day = %d", got)
	}
	if got := dayOf(moment, time.FixedZone("CET", 3600)); got != 20261021 {
		t.Fatalf("CET day = %d", got)
	}
}

// A spawned thread that spawned another: all of it belongs to the conversation
// someone actually had.
func TestSpawnedThreadsAddUpUnderTheirRoot(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	meta := func(id, parent string) string {
		return fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"id":%q,"cwd":"/work/app","source":{"subagent":{"thread_spawn":{"parent_thread_id":%q}}}}}`, id, parent) + "\n"
	}
	rootID := "cccccccc-0000-0000-0000-000000000003"
	write(t, root, "codex/sessions/rollout-"+rootID+".jsonl",
		fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"id":%q,"cwd":"/work/app","source":"cli"}}`, rootID)+"\n"+codexLine(1, 100, 10, 100, 10))
	write(t, root, "codex/sessions/rollout-"+conversationA+".jsonl", meta(conversationA, rootID)+codexLine(1, 20, 2, 20, 2))
	write(t, root, "codex/sessions/rollout-"+conversationB+".jsonl", meta(conversationB, conversationA)+codexLine(1, 3, 1, 3, 1))
	sync(t, s, root)

	items, err := s.Items(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var mainInput, spawnedInput int64
	for _, item := range items {
		if item.Conversation != rootID {
			t.Fatalf("booked to %s: %+v", item.Conversation, item)
		}
		if item.Subagent {
			spawnedInput += item.Input
		} else {
			mainInput += item.Input
		}
	}
	if mainInput != 100 || spawnedInput != 23 {
		t.Fatalf("main %d, spawned %d", mainInput, spawnedInput)
	}
}

// A subagent often runs in a worktree of its own. The main thread says which
// project this was, whichever of them was read first.
func TestTheMainThreadsDirectoryWins(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	dirRow := func(cwd string) string {
		return fmt.Sprintf(`{"type":"assistant","requestId":"r-%s","timestamp":"2026-10-20T10:00:00Z","cwd":%q,"message":{"id":"m-%s","model":"x","usage":{"input_tokens":1}}}`, cwd, cwd, cwd) + "\n"
	}
	write(t, root, "claude/projects/-work-app/"+conversationA+"/subagents/agent-1.jsonl", dirRow("/work/app/.claude/worktrees/agent-1"))
	sync(t, s, root)
	write(t, root, claudePath(conversationA), dirRow("/work/app"))
	sync(t, s, root)
	write(t, root, "claude/projects/-work-app/"+conversationA+"/subagents/agent-2.jsonl", dirRow("/work/app/.claude/worktrees/agent-2"))
	sync(t, s, root)

	items, err := s.Items(context.Background(), 0)
	if err != nil || len(items) == 0 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	for _, item := range items {
		if item.Cwd != "/work/app" {
			t.Fatalf("cwd = %q", item.Cwd)
		}
	}
}

// A session holds only its current conversation, and starting another
// overwrites it. What was learned stays.
func TestAnOwnerIsRememberedAfterTheSessionMovesOn(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.RememberOwners(ctx, []Owner{{Machine: LocalMachine, Conversation: conversationA, Directory: "/work", SessionID: "s1", SessionName: "API", TabName: "tab"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberOwners(ctx, []Owner{{Machine: LocalMachine, Conversation: conversationB, Directory: "/work", SessionID: "s1", SessionName: "API"}}); err != nil {
		t.Fatal(err)
	}
	owners, err := s.Owners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := owners[OwnerKey(LocalMachine, conversationA)]
	if len(got) != 1 || got[0].SessionName != "API" || got[0].TabName != "tab" || got[0].Directory != "/work" {
		t.Fatalf("owners = %+v", got)
	}
}

// Seen here: two sessions storing one conversation id. Neither replaces the other.
func TestEveryClaimOnAConversationIsKept(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	claim := func(session string) Owner {
		return Owner{Machine: LocalMachine, Conversation: conversationA, Directory: "/" + session, SessionID: session, SessionName: session}
	}
	if err := s.RememberOwners(ctx, []Owner{claim("pbx"), claim("weberp")}); err != nil {
		t.Fatal(err)
	}
	owners, err := s.Owners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := owners[OwnerKey(LocalMachine, conversationA)]; len(got) != 2 {
		t.Fatalf("owners = %+v", got)
	}
}

// A failed scan must leave the time of the last good one, so the view can say
// how old the numbers are.
func TestAFailedScanKeepsTheLastGoodTime(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	when := time.Unix(1_800_000_000, 0)
	if err := s.RecordScan(ctx, "srv", when, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordScan(ctx, "srv", when.Add(time.Hour), fmt.Errorf("unreachable\nwith detail")); err != nil {
		t.Fatal(err)
	}
	scans, err := s.Scans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if scan := scans["srv"]; scan.LastUnix != when.Unix() || scan.Error != "unreachable" {
		t.Fatalf("scan = %+v", scan)
	}
	if err := s.RecordScan(ctx, "srv", when.Add(2*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	scans, _ = s.Scans(ctx)
	if scan := scans["srv"]; scan.LastUnix != when.Add(2*time.Hour).Unix() || scan.Error != "" {
		t.Fatalf("after recovery: %+v", scan)
	}
}

// A path with a space or an accent has to reach SQLite intact.
func TestADatabasePathWithAccentsOpens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Képernyő mappa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(filepath.Join(dir, "usage.db"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := opened.RecordScan(context.Background(), "m", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "usage.db")); err != nil {
		t.Fatalf("not created where asked: %v", err)
	}
}

// Everything committed stays, and the next scan picks up the rest.
func TestAScanCancelledPartWayLosesNothing(t *testing.T) {
	s, root := openTestStore(t), t.TempDir()
	for i := range 5 {
		write(t, root, claudePath(fmt.Sprintf("%036d", i)), claudeLine(fmt.Sprintf("m%d", i), "r", 1, 1, 1, 1))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := LocalSource{Roots: tokenusage.Roots{Claude: filepath.Join(root, "claude")}}
	if err := s.Sync(ctx, LocalMachine, source, nil); err == nil {
		t.Fatal("a cancelled sync reported success")
	}
	sync(t, s, root)
	if got := sum(t, s); got.responses != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestDirectoryOwnersAreReplacedPerMachine(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	entry := func(machine, directory, session string) DirectoryOwner {
		return DirectoryOwner{Machine: machine, Directory: directory, Owner: Owner{SessionID: session, SessionName: session}}
	}
	if err := s.ReplaceDirectoryOwners(ctx, "a", []DirectoryOwner{entry("a", "/one", "s1")}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDirectoryOwners(ctx, "b", []DirectoryOwner{entry("b", "/one", "s2")}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDirectoryOwners(ctx, "a", []DirectoryOwner{entry("a", "/two", "s3")}); err != nil {
		t.Fatal(err)
	}
	owners, err := s.DirectoryOwners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, owner := range owners {
		found[owner.Machine+owner.Directory] = owner.Owner.SessionID
	}
	if len(found) != 2 || found["a/two"] != "s3" || found["b/one"] != "s2" {
		t.Fatalf("owners = %+v", found)
	}
}

func TestAProjectLinkIsSetChangedAndRemoved(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, step := range []struct{ name, want string }{{"Sparrow", "Sparrow"}, {"MADisoCAD", "MADisoCAD"}, {"", ""}} {
		if err := s.SetProjectLink(ctx, "session:s1", step.name); err != nil {
			t.Fatal(err)
		}
		links, err := s.ProjectLinks(ctx)
		if err != nil || links["session:s1"] != step.want {
			t.Fatalf("after %q: %v, %v", step.name, links, err)
		}
	}
}
