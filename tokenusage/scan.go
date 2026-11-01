// Package tokenusage reads what coding agents recorded about their token use.
//
// Claude Code and Codex log every response with its token counts, in plain
// files under the user's home. This package turns those files into rows of
// numbers and nothing else: no prompt, no answer, no file content leaves it.
//
// It imports nothing heavy on purpose. The helper on a server compiles it in,
// so a server parses its own logs and sends back numbers, rather than the app
// copying gigabytes of transcripts — which can hold credentials — over SSH.
//
// Reading is incremental and resumable. The caller keeps a Cursor per file and
// hands it back, so the helper can stay stateless.
package tokenusage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Agent names, as they appear in session.AgentType.
const (
	AgentClaude = "claude"
	AgentCodex  = "codex"
)

const (
	claudePrefix = "claude:"
	codexPrefix  = "codex:"
)

// maxLineBytes bounds one log line in memory. Claude Code logs whole API
// requests as single lines, and those run to many megabytes; none of them
// carries usage. A longer line is stepped over rather than read.
const maxLineBytes = 16 << 20

// Roots says where each agent keeps its logs.
type Roots struct {
	Claude string
	Codex  string
}

// DefaultRoots returns this machine's log directories, honouring the variables
// that relocate them.
func DefaultRoots() (Roots, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Roots{}, err
	}
	roots := Roots{
		Claude: filepath.Join(home, ".claude"),
		Codex:  filepath.Join(home, ".codex"),
	}
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		roots.Claude = dir
	}
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		roots.Codex = dir
	}
	return roots, nil
}

// FileInfo is one log file, as listed.
type FileInfo struct {
	// ID stays the same for the file's whole life.
	//
	// Not the path, for Codex: archiving a conversation moves its rollout from
	// sessions/ to archived_sessions/, and a path-keyed cursor would read the
	// moved file from the start and count all of it again. The file name holds
	// the conversation's own id, so it is unique on its own.
	ID    string `json:"i"`
	Path  string `json:"p"`
	Size  int64  `json:"s"`
	ModMs int64  `json:"m"`
}

// ListFiles returns every log file. A directory that does not exist means the
// agent is not used here, which is no error.
func ListFiles(roots Roots) ([]FileInfo, error) {
	var files []FileInfo
	if roots.Claude != "" {
		found, err := walkLogs(filepath.Join(roots.Claude, "projects"), claudePrefix+"projects/", func(rel string) string {
			return claudePrefix + "projects/" + rel
		})
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	if roots.Codex != "" {
		for _, dir := range []string{"sessions", "archived_sessions"} {
			found, err := walkLogs(filepath.Join(roots.Codex, dir), codexPrefix+dir+"/", func(rel string) string {
				return codexPrefix + path.Base(rel)
			})
			if err != nil {
				return nil, err
			}
			files = append(files, found...)
		}
	}
	return files, nil
}

func walkLogs(dir, pathPrefix string, idOf func(rel string) string) ([]FileInfo, error) {
	// WalkDir does not follow a symlink it is pointed at, and a log directory
	// moved to a bigger disk and linked back is an ordinary setup.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	var files []FileInfo
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory must not hide all the others, and one
			// that does not exist is an agent not used here.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		files = append(files, FileInfo{
			ID:    idOf(rel),
			Path:  pathPrefix + rel,
			Size:  info.Size(),
			ModMs: info.ModTime().UnixMilli(),
		})
		return nil
	})
	return files, err
}

// Totals are Codex's running counters for a conversation.
type Totals struct {
	Input      int64 `json:"i"`
	Cached     int64 `json:"c"`
	CacheWrite int64 `json:"w"`
	Output     int64 `json:"o"`
}

// Cursor is where reading a file stopped, and what must be remembered from the
// part already read. Claude repeats everything on every line; Codex says the
// directory, the model and the parent thread once, near the top.
type Cursor struct {
	Offset       int64   `json:"o"`
	Conversation string  `json:"c,omitempty"`
	Parent       string  `json:"p,omitempty"`
	Subagent     bool    `json:"s,omitempty"`
	Cwd          string  `json:"d,omitempty"`
	Model        string  `json:"m,omitempty"`
	Previous     *Totals `json:"t,omitempty"`
	// Working and LastLineMs find where a rollout's replayed history ends; see
	// codexParser.observeLine.
	Working    bool  `json:"w,omitempty"`
	LastLineMs int64 `json:"l,omitempty"`
}

// Row is one response's token counts, or for Codex a run of them.
type Row struct {
	// Key identifies the response when the log may repeat it. Zero means the
	// row is already known to be unique.
	Key        uint64 `json:"k,omitempty"`
	Unix       int64  `json:"t"`
	Model      string `json:"m,omitempty"`
	Cwd        string `json:"d,omitempty"`
	Subagent   bool   `json:"s,omitempty"`
	Input      int64  `json:"i,omitempty"`
	Output     int64  `json:"o,omitempty"`
	CacheRead  int64  `json:"r,omitempty"`
	CacheWrite int64  `json:"w,omitempty"`
}

// ReadRequest asks for one file from a cursor.
type ReadRequest struct {
	Path   string `json:"p"`
	Cursor Cursor `json:"c"`
}

// Limits bound one batch, so that the answer fits in one message and a slow
// disk answers within a call's timeout.
type Limits struct {
	MaxRows  int   `json:"maxRows,omitempty"`
	MaxBytes int64 `json:"maxBytes,omitempty"`
}

// DefaultLimits suit a call over a helper. A row is about 120 bytes of JSON, so
// the row cap keeps an answer well under the protocol's message limit.
var DefaultLimits = Limits{MaxRows: 50_000, MaxBytes: 128 << 20}

// FileResult is what reading one file produced.
type FileResult struct {
	Path  string `json:"p"`
	Agent string `json:"a"`
	// Conversation is what the rows are booked to: the file's own, or for a
	// spawned Codex thread the thread that spawned it.
	Conversation string `json:"c,omitempty"`
	// Own and Parent are the file's own id and its parent's. A spawned thread
	// can spawn another, so the store follows these up to the conversation
	// someone actually had.
	Own    string `json:"n,omitempty"`
	Parent string `json:"e,omitempty"`
	Cursor Cursor `json:"u"`
	// Name is what the conversation was called on purpose (agent-name), and Title
	// the caption Claude Code wrote itself from its first message, which goes stale:
	// a two-month conversation still bears the words of its opening question.
	// Background says it ran detached (claude --bg) rather than in a terminal.
	Name       string `json:"b,omitempty"`
	Title      string `json:"h,omitempty"`
	Background bool   `json:"g,omitempty"`
	// ContinuedIn lists the conversations this file says it was carried on in.
	// Moving a Claude conversation to the background starts a new one.
	ContinuedIn []string `json:"o,omitempty"`
	Rows        []Row    `json:"r,omitempty"`
	// More says the limits stopped this file before its end.
	More bool `json:"x,omitempty"`
	// Err is why this file could not be read. Per file, because one vanished or
	// locked log must not fail everything read alongside it.
	Err string `json:"f,omitempty"`
}

// ReadBatch reads files in order until the limits run out. A file not reached
// is simply absent from the answer, so the caller asks for it again.
func ReadBatch(roots Roots, requests []ReadRequest, limits Limits) []FileResult {
	if limits.MaxRows <= 0 {
		limits.MaxRows = DefaultLimits.MaxRows
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = DefaultLimits.MaxBytes
	}

	results := make([]FileResult, 0, len(requests))
	rows := 0
	var read int64
	for _, request := range requests {
		remaining := Limits{MaxRows: limits.MaxRows - rows, MaxBytes: limits.MaxBytes - read}
		if remaining.MaxRows <= 0 || remaining.MaxBytes <= 0 {
			break
		}
		result, consumed := readFile(roots, request, remaining)
		results = append(results, result)
		rows += len(result.Rows)
		read += consumed
	}
	return results
}

// resolvePath turns a listed path back into a file, refusing anything that is
// not one of the logs. The path comes in over the wire.
func resolvePath(roots Roots, listed string) (agent, full string, err error) {
	var root, rel string
	switch {
	case strings.HasPrefix(listed, claudePrefix):
		agent, root, rel = AgentClaude, roots.Claude, strings.TrimPrefix(listed, claudePrefix)
		if !strings.HasPrefix(rel, "projects/") {
			return "", "", fmt.Errorf("not a log path: %s", listed)
		}
	case strings.HasPrefix(listed, codexPrefix):
		agent, root, rel = AgentCodex, roots.Codex, strings.TrimPrefix(listed, codexPrefix)
		if !strings.HasPrefix(rel, "sessions/") && !strings.HasPrefix(rel, "archived_sessions/") {
			return "", "", fmt.Errorf("not a log path: %s", listed)
		}
	default:
		return "", "", fmt.Errorf("not a log path: %s", listed)
	}
	if root == "" || !strings.HasSuffix(rel, ".jsonl") || path.Clean(rel) != rel {
		return "", "", fmt.Errorf("not a log path: %s", listed)
	}
	return agent, filepath.Join(root, filepath.FromSlash(rel)), nil
}

func readFile(roots Roots, request ReadRequest, limits Limits) (FileResult, int64) {
	result := FileResult{Path: request.Path, Cursor: request.Cursor}
	agent, full, err := resolvePath(roots, request.Path)
	if err != nil {
		result.Err = err.Error()
		return result, 0
	}
	result.Agent = agent

	file, err := os.Open(full)
	if err != nil {
		result.Err = err.Error()
		return result, 0
	}
	defer file.Close()
	if _, err := file.Seek(request.Cursor.Offset, io.SeekStart); err != nil {
		result.Err = err.Error()
		return result, 0
	}

	var parse lineParser
	if agent == AgentClaude {
		parse = newClaudeParser(request.Path)
	} else {
		parse = newCodexParser(request.Path, &result.Cursor)
	}

	reader := bufio.NewReaderSize(file, 1<<20)
	var consumed int64
	for {
		line, size, complete, err := readLine(reader)
		if err != nil && err != io.EOF {
			result.Err = err.Error()
			break
		}
		// A line with no newline yet is one the agent is still writing. It is
		// left for next time rather than parsed half-way.
		if !complete {
			break
		}
		consumed += size
		result.Cursor.Offset += size
		if line != nil {
			if row, ok := parse.parse(line); ok {
				result.Rows = appendRow(result.Rows, row)
			}
		}
		if len(result.Rows) >= limits.MaxRows || consumed >= limits.MaxBytes {
			result.More = true
			break
		}
	}
	parse.finish(&result)
	return result, consumed
}

// readLine returns one line and how many bytes it took. A line over the limit
// comes back nil, with its size still counted so the cursor moves past it.
func readLine(reader *bufio.Reader) (line []byte, size int64, complete bool, err error) {
	var buffer []byte
	tooLong := false
	for {
		chunk, readErr := reader.ReadSlice('\n')
		size += int64(len(chunk))
		if !tooLong {
			if int64(len(buffer))+int64(len(chunk)) > maxLineBytes {
				tooLong, buffer = true, nil
			} else {
				buffer = append(buffer, chunk...)
			}
		}
		switch {
		case readErr == nil:
			if tooLong {
				return nil, size, true, nil
			}
			return buffer, size, true, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		default:
			return nil, size, false, readErr
		}
	}
}

// appendRow joins a keyless row to the one before it when they fall in the same
// quarter of an hour with the same model. Codex logs a counter after every
// event, millions of lines on a busy machine, and only the sum per day is ever
// shown. A quarter because no time zone's midnight falls inside one.
func appendRow(rows []Row, row Row) []Row {
	if row.Key == 0 && len(rows) > 0 {
		last := &rows[len(rows)-1]
		if last.Key == 0 && last.Model == row.Model && last.Subagent == row.Subagent &&
			last.Cwd == row.Cwd && last.Unix/900 == row.Unix/900 {
			last.Input += row.Input
			last.Output += row.Output
			last.CacheRead += row.CacheRead
			last.CacheWrite += row.CacheWrite
			return rows
		}
	}
	return append(rows, row)
}

type lineParser interface {
	parse(line []byte) (Row, bool)
	finish(result *FileResult)
}

// --- Claude Code ---

type claudeParser struct {
	conversation string
	subagent     bool
	name         string
	title        string
	background   bool
	continuedIn  []string
}

// newClaudeParser reads the conversation from where the file lies:
//
//	projects/<dir>/<conversation>.jsonl
//	projects/<dir>/<conversation>/subagents/agent-<id>.jsonl
//
// The file name, not the line's sessionId: a resumed conversation can carry
// over lines that name the one it came from.
func newClaudeParser(listed string) *claudeParser {
	parts := strings.Split(strings.TrimPrefix(listed, claudePrefix+"projects/"), "/")
	parser := &claudeParser{}
	if len(parts) >= 2 {
		parser.conversation = strings.TrimSuffix(parts[1], ".jsonl")
		parser.subagent = len(parts) > 2
	}
	return parser
}

type claudeLine struct {
	Type        string `json:"type"`
	RequestID   string `json:"requestId"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	usageMarker        = []byte(`"usage"`)
	agentNameMarker    = []byte(`{"type":"agent-name"`)
	aiTitleMarker      = []byte(`{"type":"ai-title"`)
	continuedInMarker  = []byte(`{"type":"continued-in"`)
	backgroundKindMark = []byte(`"sessionKind":"bg"`)
)

// readFacts takes what a file says about its conversation, as opposed to what any
// one response cost. These are short lines of their own, so a prefix finds them
// without decoding the long ones.
func (p *claudeParser) readFacts(line []byte) bool {
	switch {
	case bytes.HasPrefix(line, agentNameMarker):
		var entry struct {
			AgentName string `json:"agentName"`
		}
		if decodeLine(line, &entry) && entry.AgentName != "" {
			p.name = entry.AgentName
		}
	case bytes.HasPrefix(line, aiTitleMarker):
		var entry struct {
			AITitle string `json:"aiTitle"`
		}
		if decodeLine(line, &entry) && entry.AITitle != "" {
			p.title = entry.AITitle
		}
	case bytes.HasPrefix(line, continuedInMarker):
		var entry struct {
			ContinuedIn string `json:"continuedInSessionId"`
		}
		if decodeLine(line, &entry) && entry.ContinuedIn != "" {
			p.continuedIn = append(p.continuedIn, entry.ContinuedIn)
		}
	default:
		return false
	}
	return true
}

func (p *claudeParser) parse(line []byte) (Row, bool) {
	if p.readFacts(line) || !bytes.Contains(line, usageMarker) {
		return Row{}, false
	}
	if !p.subagent && bytes.Contains(line, backgroundKindMark) {
		p.background = true
	}
	var entry claudeLine
	if !decodeLine(line, &entry) {
		return Row{}, false
	}
	message := entry.Message
	// "<synthetic>" is Claude Code's own placeholder for a turn that never
	// reached the API, such as an error shown as a reply.
	if entry.Type != "assistant" || message == nil || message.Usage == nil || message.Model == "<synthetic>" {
		return Row{}, false
	}
	usage := message.Usage
	// One response is logged once per content block, and a resumed conversation
	// copies earlier ones into its new file. Measured here: 34,497 rows for
	// 18,673 responses in a single conversation.
	return Row{
		Key:        responseKey(message.ID, entry.RequestID, line),
		Unix:       parseUnix(entry.Timestamp),
		Model:      message.Model,
		Cwd:        entry.Cwd,
		Subagent:   p.subagent || entry.IsSidechain,
		Input:      usage.Input,
		Output:     usage.Output,
		CacheRead:  usage.CacheRead,
		CacheWrite: usage.CacheWrite,
	}, true
}

func (p *claudeParser) finish(result *FileResult) {
	result.Conversation = p.conversation
	result.Own = p.conversation
	// A subagent's own name is not the conversation's.
	if p.subagent {
		return
	}
	result.Name = p.name
	result.Title = p.title
	result.Background = p.background
	result.ContinuedIn = p.continuedIn
}

// responseKey is never zero, which means "no key". A line with no id at all
// falls back on its own text, so only a truly repeated line is dropped.
func responseKey(messageID, requestID string, line []byte) uint64 {
	hash := fnv.New64a()
	if messageID == "" && requestID == "" {
		hash.Write(line)
	} else {
		hash.Write([]byte(messageID))
		hash.Write([]byte{0})
		hash.Write([]byte(requestID))
	}
	if key := hash.Sum64(); key != 0 {
		return key
	}
	return 1
}

// --- Codex ---

type codexParser struct {
	cursor *Cursor
}

// newCodexParser falls back on the file name for the conversation, in case the
// session_meta line was never written: rollout-<time>-<uuid>.jsonl.
func newCodexParser(listed string, cursor *Cursor) *codexParser {
	if cursor.Conversation == "" {
		name := strings.TrimSuffix(path.Base(listed), ".jsonl")
		if len(name) > 36 {
			cursor.Conversation = name[len(name)-36:]
		}
	}
	return &codexParser{cursor: cursor}
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	ID           string          `json:"id"`
	Cwd          string          `json:"cwd"`
	ForkedFromID string          `json:"forked_from_id"`
	Source       json.RawMessage `json:"source"`
}

type codexSource struct {
	Subagent *struct {
		ThreadSpawn *struct {
			ParentThreadID string `json:"parent_thread_id"`
		} `json:"thread_spawn"`
	} `json:"subagent"`
}

type codexCounters struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
}

type codexEvent struct {
	Type string `json:"type"`
	Info *struct {
		Total codexCounters `json:"total_token_usage"`
		Last  codexCounters `json:"last_token_usage"`
	} `json:"info"`
}

var (
	codexMetaMarker    = []byte(`"session_meta"`)
	codexContextMarker = []byte(`"turn_context"`)
	codexTokenMarker   = []byte(`"token_count"`)
)

// replayGap is how long a pause in the log means real work has begun.
const replayGap = 500 * time.Millisecond

var codexTimestampPrefix = []byte(`{"timestamp":"`)

// observeLine finds where a rollout's replayed history ends.
//
// A thread forked from another starts its file by writing out the parent's whole
// history, token_count lines included, stamped with the moment of the fork. Those
// tokens were already counted in the parent — billions of them on a long-running
// conversation — and nothing on the line says they are a copy.
//
// What gives them away is the clock. The copy is written in one burst, while no
// real response can arrive without a model call in between, so a token_count
// before the file's first pause is not work done here. Checked against 945 forks
// on a real machine: this finds the same boundary as comparing with the parent's
// rows in all of them, and sees a replay in none of 226 rollouts that are not
// forks, at any threshold from 0.25 to 1 second.
func (p *codexParser) observeLine(line []byte) {
	if p.cursor.Working {
		return
	}
	stamp := codexLineMillis(line)
	if stamp == 0 {
		// A log that does not say when anything happened cannot be told apart.
		// Better to count a copy than to lose everything that follows.
		p.cursor.Working = true
		return
	}
	if p.cursor.LastLineMs != 0 && stamp-p.cursor.LastLineMs >= replayGap.Milliseconds() {
		p.cursor.Working = true
	}
	p.cursor.LastLineMs = stamp
}

// codexLineMillis reads a line's time without decoding the line, which during
// a replay can mean tens of thousands of them. Any other layout falls back on
// decoding it.
func codexLineMillis(line []byte) int64 {
	var text string
	if bytes.HasPrefix(line, codexTimestampPrefix) {
		rest := line[len(codexTimestampPrefix):]
		if end := bytes.IndexByte(rest, '"'); end > 0 {
			text = string(rest[:end])
		}
	} else {
		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if decodeLine(line, &entry) {
			text = entry.Timestamp
		}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

func (p *codexParser) parse(line []byte) (Row, bool) {
	p.observeLine(line)
	isToken := bytes.Contains(line, codexTokenMarker)
	if !isToken && !bytes.Contains(line, codexMetaMarker) && !bytes.Contains(line, codexContextMarker) {
		return Row{}, false
	}
	var entry codexLine
	if !decodeLine(line, &entry) {
		return Row{}, false
	}

	switch entry.Type {
	case "session_meta":
		p.readMeta(entry.Payload)
	case "turn_context":
		var context struct {
			Model string `json:"model"`
			Cwd   string `json:"cwd"`
		}
		if json.Unmarshal(entry.Payload, &context) == nil {
			if context.Model != "" {
				p.cursor.Model = context.Model
			}
			if p.cursor.Cwd == "" {
				p.cursor.Cwd = context.Cwd
			}
		}
	case "event_msg":
		if isToken {
			return p.readTokens(entry)
		}
	}
	return Row{}, false
}

// readMeta takes only the first session_meta. A resumed or spawned thread can
// repeat its parent's further down, and those must not rename this one.
func (p *codexParser) readMeta(payload json.RawMessage) {
	if p.cursor.Parent != "" || p.cursor.Cwd != "" {
		return
	}
	var meta codexMeta
	if json.Unmarshal(payload, &meta) != nil {
		return
	}
	p.cursor.Cwd = meta.Cwd
	if meta.ID != "" {
		p.cursor.Conversation = meta.ID
	}

	// A thread spawned by another is that thread's subagent, not a conversation
	// someone had, and its tokens belong to the one that spawned it. A fork
	// someone asked for is a conversation of its own: it can continue in another
	// session, which would otherwise be credited with nothing.
	var source codexSource
	if len(meta.Source) > 0 && json.Unmarshal(meta.Source, &source) == nil && source.Subagent != nil {
		p.cursor.Subagent = true
		p.cursor.Parent = meta.ForkedFromID
		if spawn := source.Subagent.ThreadSpawn; spawn != nil && spawn.ParentThreadID != "" {
			p.cursor.Parent = spawn.ParentThreadID
		}
	}
}

// readTokens turns the running total into what this event added.
//
// The difference of totals rather than last_token_usage, because Codex repeats
// a token_count line unchanged whenever only the rate limits moved: 91,770 such
// repeats measured here, each of which would be counted again.
//
// Last is the right answer in the two cases a difference is not: a first row with
// no baseline before it, and a total that went down, which means the counter was
// restarted.
func (p *codexParser) readTokens(entry codexLine) (Row, bool) {
	var event codexEvent
	if json.Unmarshal(entry.Payload, &event) != nil || event.Type != "token_count" || event.Info == nil {
		return Row{}, false
	}
	total := event.Info.Total
	current := &Totals{Input: total.Input, Cached: total.Cached, CacheWrite: total.CacheWrite, Output: total.Output}
	previous := p.cursor.Previous
	p.cursor.Previous = current
	// Part of a replay: it moves the baseline and adds nothing.
	if !p.cursor.Working {
		return Row{}, false
	}

	var added codexCounters
	if previous == nil || current.Input < previous.Input || current.Output < previous.Output {
		added = event.Info.Last
	} else {
		added = codexCounters{
			Input:      current.Input - previous.Input,
			Cached:     current.Cached - previous.Cached,
			CacheWrite: current.CacheWrite - previous.CacheWrite,
			Output:     current.Output - previous.Output,
		}
	}
	if added.Input == 0 && added.Output == 0 && added.Cached == 0 && added.CacheWrite == 0 {
		return Row{}, false
	}

	// Codex's input includes what came from the cache; Claude's does not. Told
	// apart here, so the two can be added up and compared.
	fresh := added.Input - added.Cached
	if fresh < 0 {
		fresh = 0
	}
	return Row{
		Unix:       parseUnix(entry.Timestamp),
		Model:      p.cursor.Model,
		Cwd:        p.cursor.Cwd,
		Subagent:   p.cursor.Subagent,
		Input:      fresh,
		Output:     added.Output,
		CacheRead:  added.Cached,
		CacheWrite: added.CacheWrite,
	}, true
}

func (p *codexParser) finish(result *FileResult) {
	own := p.cursor.Conversation
	result.Own = own
	result.Conversation = own
	if p.cursor.Parent != "" {
		result.Conversation = p.cursor.Parent
		result.Parent = p.cursor.Parent
	}
}

// --- shared ---

// decodeLine decodes one JSON line. Some Codex lines carry raw control
// characters inside strings, which the JSON grammar forbids and the decoder
// refuses; those are blanked and tried again, since the numbers are unharmed.
func decodeLine(line []byte, target any) bool {
	if json.Unmarshal(line, target) == nil {
		return true
	}
	cleaned := bytes.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, line)
	return json.Unmarshal(cleaned, target) == nil
}

// parseUnix reads a log timestamp. Zero means there was none; the store counts
// such a row on the day it was read rather than losing it.
func parseUnix(timestamp string) int64 {
	if timestamp == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}
