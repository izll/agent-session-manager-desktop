package tokenusage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConversation = "11111111-2222-3333-4444-555555555555"

func claudeRow(messageID, requestID string, input, output, read, write int64) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":%q,"timestamp":"2026-10-20T10:00:00Z","cwd":"/work/app",`+
		`"message":{"id":%q,"model":"claude-opus-5-5","content":[{"type":"text","text":"secret words"}],`+
		`"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		requestID, messageID, input, output, read, write)
}

func writeLog(t *testing.T, root, rel string, lines ...string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func appendLog(t *testing.T, full, text string) {
	t.Helper()
	file, err := os.OpenFile(full, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func readOne(t *testing.T, roots Roots, listed string, cursor Cursor) FileResult {
	t.Helper()
	results := ReadBatch(roots, []ReadRequest{{Path: listed, Cursor: cursor}}, Limits{})
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Err != "" {
		t.Fatalf("read %s: %s", listed, results[0].Err)
	}
	return results[0]
}

func TestListFilesFindsBothAgents(t *testing.T) {
	claude, codex := t.TempDir(), t.TempDir()
	writeLog(t, claude, "projects/-work-app/"+testConversation+".jsonl", "{}")
	writeLog(t, claude, "projects/-work-app/"+testConversation+"/subagents/agent-a1.jsonl", "{}")
	writeLog(t, claude, "projects/-work-app/notes.txt", "not a log")
	writeLog(t, codex, "sessions/2026/10/20/rollout-a.jsonl", "{}")
	writeLog(t, codex, "archived_sessions/rollout-b.jsonl", "{}")

	files, err := ListFiles(Roots{Claude: claude, Codex: codex})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, file := range files {
		ids[file.ID] = file.Path
	}
	want := map[string]string{
		"claude:projects/-work-app/" + testConversation + ".jsonl":                    "claude:projects/-work-app/" + testConversation + ".jsonl",
		"claude:projects/-work-app/" + testConversation + "/subagents/agent-a1.jsonl": "claude:projects/-work-app/" + testConversation + "/subagents/agent-a1.jsonl",
		"codex:rollout-a.jsonl": "codex:sessions/2026/10/20/rollout-a.jsonl",
		"codex:rollout-b.jsonl": "codex:archived_sessions/rollout-b.jsonl",
	}
	if len(ids) != len(want) {
		t.Fatalf("files = %v", ids)
	}
	for id, listed := range want {
		if ids[id] != listed {
			t.Errorf("%s listed as %q, want %q", id, ids[id], listed)
		}
	}
}

// An agent never used here has no directory, which is an ordinary state.
func TestListFilesWithoutLogsIsEmpty(t *testing.T) {
	files, err := ListFiles(Roots{Claude: filepath.Join(t.TempDir(), "nope"), Codex: filepath.Join(t.TempDir(), "nope")})
	if err != nil || len(files) != 0 {
		t.Fatalf("files = %v, err = %v", files, err)
	}
}

func TestClaudeRowsCarryNumbersOnly(t *testing.T) {
	root := t.TempDir()
	listed := "claude:projects/-work-app/" + testConversation + ".jsonl"
	writeLog(t, root, "projects/-work-app/"+testConversation+".jsonl",
		`{"type":"user","message":{"role":"user","content":"secret words"}}`,
		claudeRow("msg_1", "req_1", 2, 30, 400, 5),
	)
	result := readOne(t, Roots{Claude: root}, listed, Cursor{})
	if result.Conversation != testConversation || result.Agent != AgentClaude {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %+v", result.Rows)
	}
	row := result.Rows[0]
	if row.Input != 2 || row.Output != 30 || row.CacheRead != 400 || row.CacheWrite != 5 ||
		row.Model != "claude-opus-5-5" || row.Cwd != "/work/app" || row.Subagent || row.Key == 0 || row.Unix == 0 {
		t.Fatalf("row = %+v", row)
	}
	if strings.Contains(fmt.Sprintf("%+v", result), "secret") {
		t.Fatal("conversation text left the reader")
	}
}

func TestClaudeSubagentFileIsBookedToItsConversation(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + "/subagents/agent-a1.jsonl"
	writeLog(t, root, rel, claudeRow("msg_1", "req_1", 1, 1, 1, 1))
	result := readOne(t, Roots{Claude: root}, "claude:"+rel, Cursor{})
	if result.Conversation != testConversation || !result.Rows[0].Subagent {
		t.Fatalf("result = %+v", result)
	}
}

// Claude Code's stand-in for a turn that never reached the API.
func TestClaudeSyntheticModelIsNotCounted(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	line := strings.Replace(claudeRow("msg_1", "req_1", 1, 1, 1, 1), "claude-opus-5-5", "<synthetic>", 1)
	writeLog(t, root, rel, line)
	if result := readOne(t, Roots{Claude: root}, "claude:"+rel, Cursor{}); len(result.Rows) != 0 {
		t.Fatalf("rows = %+v", result.Rows)
	}
}

func TestTheSameResponseHasTheSameKey(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	writeLog(t, root, rel,
		claudeRow("msg_1", "req_1", 1, 0, 1, 1),
		claudeRow("msg_1", "req_1", 1, 50, 1, 1),
		claudeRow("msg_2", "req_2", 1, 1, 1, 1),
	)
	rows := readOne(t, Roots{Claude: root}, "claude:"+rel, Cursor{}).Rows
	if len(rows) != 3 || rows[0].Key != rows[1].Key || rows[0].Key == rows[2].Key {
		t.Fatalf("rows = %+v", rows)
	}
}

// The agent may be half way through writing the last line.
func TestAnUnfinishedLineWaitsForItsNewline(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	full := writeLog(t, root, rel, claudeRow("msg_1", "req_1", 1, 1, 1, 1))
	whole := claudeRow("msg_2", "req_2", 1, 2, 1, 1)
	appendLog(t, full, whole[:40])

	first := readOne(t, Roots{Claude: root}, "claude:"+rel, Cursor{})
	if len(first.Rows) != 1 {
		t.Fatalf("rows = %+v", first.Rows)
	}

	appendLog(t, full, whole[40:]+"\n")
	second := readOne(t, Roots{Claude: root}, "claude:"+rel, first.Cursor)
	if len(second.Rows) != 1 || second.Rows[0].Output != 2 {
		t.Fatalf("rows after the line was finished = %+v", second.Rows)
	}
}

// Claude Code logs whole API requests as one line, many megabytes long.
func TestAnOverlongLineIsSteppedOver(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	long := `{"type":"api-request-blob","usage":"` + strings.Repeat("x", maxLineBytes+1024) + `"}`
	full := writeLog(t, root, rel, long, claudeRow("msg_1", "req_1", 1, 7, 1, 1))
	result := readOne(t, Roots{Claude: root}, "claude:"+rel, Cursor{})
	if len(result.Rows) != 1 || result.Rows[0].Output != 7 {
		t.Fatalf("rows = %+v", result.Rows)
	}
	info, _ := os.Stat(full)
	if result.Cursor.Offset != info.Size() {
		t.Fatalf("offset %d, file size %d", result.Cursor.Offset, info.Size())
	}
}

// The path comes in over the wire to a process running with the user's rights.
func TestAPathThatIsNotALogIsRefused(t *testing.T) {
	claude, codex := t.TempDir(), t.TempDir()
	secret := filepath.Join(filepath.Dir(claude), "secret.jsonl")
	_ = os.WriteFile(secret, []byte(claudeRow("m", "r", 1, 1, 1, 1)+"\n"), 0o644)
	t.Cleanup(func() { os.Remove(secret) })

	for _, listed := range []string{
		"claude:projects/../../secret.jsonl",
		"claude:settings.json",
		"claude:projects/a.txt",
		"codex:auth.json",
		"codex:sessions/../auth.jsonl",
		"/etc/passwd",
		"secret.jsonl",
		"",
	} {
		results := ReadBatch(Roots{Claude: claude, Codex: codex}, []ReadRequest{{Path: listed}}, Limits{})
		if len(results) != 1 || results[0].Err == "" || len(results[0].Rows) != 0 {
			t.Errorf("%q was read: %+v", listed, results)
		}
	}
}

func TestOneUnreadableFileDoesNotStopTheBatch(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	writeLog(t, root, rel, claudeRow("msg_1", "req_1", 1, 1, 1, 1))
	results := ReadBatch(Roots{Claude: root}, []ReadRequest{
		{Path: "claude:projects/-work-app/gone.jsonl"},
		{Path: "claude:" + rel},
	}, Limits{})
	if len(results) != 2 || results[0].Err == "" || results[1].Err != "" || len(results[1].Rows) != 1 {
		t.Fatalf("results = %+v", results)
	}
}

func TestLimitsStopAndResumeWithoutLoss(t *testing.T) {
	root := t.TempDir()
	rel := "projects/-work-app/" + testConversation + ".jsonl"
	var lines []string
	for i := range 10 {
		lines = append(lines, claudeRow(fmt.Sprintf("msg_%d", i), "req", 1, 1, 1, 1))
	}
	other := "projects/-work-app/" + strings.Repeat("9", 36) + ".jsonl"
	writeLog(t, root, rel, lines...)
	writeLog(t, root, other, lines[0])

	roots := Roots{Claude: root}
	requests := []ReadRequest{{Path: "claude:" + rel}, {Path: "claude:" + other}}
	results := ReadBatch(roots, requests, Limits{MaxRows: 4})
	if len(results) != 1 || !results[0].More || len(results[0].Rows) != 4 {
		t.Fatalf("first batch = %+v", results)
	}

	total := len(results[0].Rows)
	cursor := results[0].Cursor
	for range 10 {
		next := ReadBatch(roots, []ReadRequest{{Path: "claude:" + rel, Cursor: cursor}}, Limits{MaxRows: 4})
		total += len(next[0].Rows)
		cursor = next[0].Cursor
		if !next[0].More {
			break
		}
	}
	if total != 10 {
		t.Fatalf("read %d rows of 10", total)
	}
}

// --- Codex ---

const codexChild = "aaaaaaaa-0000-0000-0000-000000000001"
const codexParent = "bbbbbbbb-0000-0000-0000-000000000002"

func codexMetaLine(id, cwd, source string) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"id":%q,"cwd":%q,"source":%s}}`, id, cwd, source)
}

func codexTurn(model string) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:01.000Z","type":"turn_context","payload":{"model":%q,"cwd":"/work/app"}}`, model)
}

func codexCount(minute int, totalIn, totalCached, totalOut, lastIn, lastCached, lastOut int64) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-20T10:%02d:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{`+
		`"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":0,"output_tokens":%d},`+
		`"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":0,"output_tokens":%d}}}}`,
		minute, totalIn, totalCached, totalOut, lastIn, lastCached, lastOut)
}

func sumRows(rows []Row) Row {
	var sum Row
	for _, row := range rows {
		sum.Input += row.Input
		sum.Output += row.Output
		sum.CacheRead += row.CacheRead
		sum.CacheWrite += row.CacheWrite
	}
	return sum
}

func TestCodexCountsWhatEachEventAdded(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/2026/10/20/rollout-2026-10-20T10-00-00-" + codexChild + ".jsonl"
	writeLog(t, root, rel,
		codexMetaLine(codexChild, "/work/app", `"cli"`),
		codexTurn("gpt-6"),
		codexCount(1, 100, 40, 10, 100, 40, 10),
		// Only the rate limits moved: the same totals logged again.
		codexCount(2, 100, 40, 10, 100, 40, 10),
		codexCount(3, 300, 140, 30, 200, 100, 20),
	)
	result := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})
	if result.Conversation != codexChild || result.Agent != AgentCodex {
		t.Fatalf("result = %+v", result)
	}
	sum := sumRows(result.Rows)
	// 300 went in, 140 of it from the cache, which Claude reports separately.
	if sum.Input != 160 || sum.CacheRead != 140 || sum.Output != 30 {
		t.Fatalf("sum = %+v", sum)
	}
	if result.Rows[0].Model != "gpt-6" || result.Rows[0].Cwd != "/work/app" {
		t.Fatalf("row = %+v", result.Rows[0])
	}
}

// No baseline and a total above its last means a conversation carried over from
// elsewhere. Only what this event added is new.
func TestCodexFirstRowCountsItsLastOnly(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	writeLog(t, root, rel, codexMetaLine(codexChild, "/work/app", `"cli"`), codexCount(1, 5000, 0, 500, 100, 0, 10))
	sum := sumRows(readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{}).Rows)
	if sum.Input != 100 || sum.Output != 10 {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestCodexRestartedCounterCountsItsLast(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	writeLog(t, root, rel,
		codexMetaLine(codexChild, "/work/app", `"cli"`),
		codexCount(1, 1000, 0, 100, 1000, 0, 100),
		codexCount(2, 50, 0, 5, 50, 0, 5),
	)
	sum := sumRows(readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{}).Rows)
	if sum.Input != 1050 || sum.Output != 105 {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestCodexSpawnedThreadIsBookedToItsParent(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	source := fmt.Sprintf(`{"subagent":{"thread_spawn":{"parent_thread_id":%q,"depth":1}}}`, codexParent)
	writeLog(t, root, rel,
		codexMetaLine(codexChild, "/work/app", source),
		codexCount(1, 10, 0, 1, 10, 0, 1),
	)
	result := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})
	if result.Conversation != codexParent || result.Own != codexChild || result.Parent != codexParent || !result.Rows[0].Subagent {
		t.Fatalf("result = %+v", result)
	}
}

// The directory and model are said once, near the top, and the running total
// is the baseline: all of it has to survive between reads.
func TestCodexResumedReadKeepsItsContext(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	full := writeLog(t, root, rel,
		codexMetaLine(codexChild, "/work/app", `"cli"`),
		codexTurn("gpt-6"),
		codexCount(1, 100, 0, 10, 100, 0, 10),
	)
	first := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})

	appendLog(t, full, codexCount(2, 130, 0, 12, 30, 0, 2)+"\n")
	second := readOne(t, Roots{Codex: root}, "codex:"+rel, first.Cursor)
	if len(second.Rows) != 1 {
		t.Fatalf("rows = %+v", second.Rows)
	}
	row := second.Rows[0]
	if row.Input != 30 || row.Output != 2 || row.Model != "gpt-6" || row.Cwd != "/work/app" || second.Conversation != codexChild {
		t.Fatalf("second read = %+v", second)
	}
}

// Codex logs a counter after every event; a busy machine has millions of them.
func TestCodexRowsOfOneHourBecomeOne(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	writeLog(t, root, rel,
		codexMetaLine(codexChild, "/work/app", `"cli"`),
		codexCount(1, 10, 0, 1, 10, 0, 1),
		codexCount(2, 20, 0, 2, 10, 0, 1),
		codexCount(3, 30, 0, 3, 10, 0, 1),
	)
	rows := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{}).Rows
	if len(rows) != 1 || rows[0].Input != 30 || rows[0].Output != 3 {
		t.Fatalf("rows = %+v", rows)
	}
}

// Seen in real rollouts: raw control characters inside a string, which the JSON
// grammar forbids. The rest of the line is fine, and it holds the directory.
func TestCodexLineWithControlCharactersIsStillRead(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	line := fmt.Sprintf("{\"timestamp\":\"2026-10-20T10:00:00.000Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":\"/work/app\",\"instructions\":\"a\x01b\"}}", codexChild)
	writeLog(t, root, rel, line, codexCount(1, 10, 0, 1, 10, 0, 1))
	if result := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{}); result.Rows[0].Cwd != "/work/app" {
		t.Fatalf("result = %+v", result)
	}
}

// codexReplay is a fork's copy of its parent's history: every line stamped within
// the same moment, as the real logs have it.
func codexReplay(id string) []string {
	return []string{
		codexMetaLine(id, "/work/app", `"cli"`),
		`{"timestamp":"2026-10-20T10:00:00.010Z","type":"session_meta","payload":{"id":"parent","cwd":"/elsewhere"}}`,
		`{"timestamp":"2026-10-20T10:00:00.020Z","type":"turn_context","payload":{"model":"gpt-5","cwd":"/elsewhere"}}`,
		`{"timestamp":"2026-10-20T10:00:00.030Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"output_tokens":100},"last_token_usage":{"input_tokens":1000,"output_tokens":100}}}}`,
		`{"timestamp":"2026-10-20T10:00:00.040Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":3000,"output_tokens":300},"last_token_usage":{"input_tokens":2000,"output_tokens":200}}}}`,
	}
}

// Measured: a fork copies its parent's token_count lines, billions of tokens on a
// long conversation, and only the pause that follows tells them from real work.
func TestAForksReplayedHistoryIsNotCounted(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	lines := append(codexReplay(codexChild),
		codexTurn("gpt-6"),
		// The fork's own first response: its total builds on the replayed one.
		codexCount(5, 3050, 0, 305, 50, 0, 5),
	)
	writeLog(t, root, rel, lines...)
	result := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})
	sum := sumRows(result.Rows)
	if sum.Input != 50 || sum.Output != 5 {
		t.Fatalf("sum = %+v", sum)
	}
	if result.Rows[0].Model != "gpt-6" || result.Rows[0].Cwd != "/work/app" {
		t.Fatalf("the replay renamed this thread: %+v", result.Rows[0])
	}
}

// The burst can end mid-read, so where it stands has to survive between reads.
func TestAReplaySplitAcrossReadsIsStillNotCounted(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	replay := codexReplay(codexChild)
	full := writeLog(t, root, rel, replay[:4]...)
	first := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})
	if len(first.Rows) != 0 {
		t.Fatalf("rows during the replay = %+v", first.Rows)
	}
	appendLog(t, full, replay[4]+"\n")
	second := readOne(t, Roots{Codex: root}, "codex:"+rel, first.Cursor)
	if len(second.Rows) != 0 {
		t.Fatalf("rows later in the replay = %+v", second.Rows)
	}
	appendLog(t, full, codexCount(5, 3050, 0, 305, 50, 0, 5)+"\n")
	third := readOne(t, Roots{Codex: root}, "codex:"+rel, second.Cursor)
	if sum := sumRows(third.Rows); sum.Input != 50 || sum.Output != 5 {
		t.Fatalf("sum after the replay = %+v", sum)
	}
}

// A log with no clock cannot be told apart, and counting a copy beats losing
// everything that follows.
func TestALogWithoutTimestampsIsCounted(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	writeLog(t, root, rel, `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":1},"last_token_usage":{"input_tokens":10,"output_tokens":1}}}}`)
	if sum := sumRows(readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{}).Rows); sum.Input != 10 {
		t.Fatalf("sum = %+v", sum)
	}
}

// A fork someone asked for is a conversation of its own, which a session can hold.
func TestAForkSomeoneAskedForIsItsOwnConversation(t *testing.T) {
	root := t.TempDir()
	rel := "sessions/rollout-" + codexChild + ".jsonl"
	line := fmt.Sprintf(`{"timestamp":"2026-10-20T10:00:00.000Z","type":"session_meta","payload":{"id":%q,"cwd":"/work/app","forked_from_id":%q,"source":"cli"}}`, codexChild, codexParent)
	writeLog(t, root, rel, line, codexCount(1, 10, 0, 1, 10, 0, 1))
	result := readOne(t, Roots{Codex: root}, "codex:"+rel, Cursor{})
	if result.Conversation != codexChild || result.Parent != "" || result.Rows[0].Subagent {
		t.Fatalf("result = %+v", result)
	}
}

// A log directory moved to a bigger disk and linked back.
func TestASymlinkedLogDirectoryIsListed(t *testing.T) {
	real, holder := t.TempDir(), t.TempDir()
	writeLog(t, real, "-work-app/"+testConversation+".jsonl", "{}")
	if err := os.Symlink(real, filepath.Join(holder, "projects")); err != nil {
		t.Skip("symlinks need privileges here")
	}
	files, err := ListFiles(Roots{Claude: holder})
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %v, err = %v", files, err)
	}
	if result := readOne(t, Roots{Claude: holder}, files[0].Path, Cursor{}); result.Err != "" {
		t.Fatalf("read: %+v", result)
	}
}

// No time zone's midnight falls inside a quarter of an hour.
func TestMergedRowsNeverCrossAQuarterHour(t *testing.T) {
	rows := appendRow(nil, Row{Unix: 899, Input: 1})
	rows = appendRow(rows, Row{Unix: 900, Input: 1})
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
}
