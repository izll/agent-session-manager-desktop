// Package store keeps what tokenusage reads, per machine, in one small SQLite
// file.
//
// Kept apart from tokenusage because the helper on a server compiles that one
// in and has no use for a database.
//
// Only numbers, ids and directory names are stored. The logs themselves stay
// where they are, and this file outlives them: Claude Code removes transcripts
// after 30 days by default, and what was already counted stays counted.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"asmgr-desktop/tokenusage"

	_ "modernc.org/sqlite"
)

// LocalMachine is the id of the computer the app runs on. Servers use their id
// from the server list, which survives a rename.
const LocalMachine = "local"

const schema = `
CREATE TABLE IF NOT EXISTS files (
  machine TEXT NOT NULL,
  id      TEXT NOT NULL,
  size    INTEGER NOT NULL,
  cursor  TEXT NOT NULL,
  PRIMARY KEY (machine, id)
) WITHOUT ROWID;

-- What has been counted, so a repeated response is not counted twice. Only the
-- output is kept: it is the one figure that grows between a response's rows.
CREATE TABLE IF NOT EXISTS seen (
  machine TEXT NOT NULL,
  key     INTEGER NOT NULL,
  output  INTEGER NOT NULL,
  PRIMARY KEY (machine, key)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS usage (
  machine      TEXT NOT NULL,
  conversation TEXT NOT NULL,
  day          INTEGER NOT NULL,
  model        TEXT NOT NULL,
  subagent     INTEGER NOT NULL,
  input        INTEGER NOT NULL,
  output       INTEGER NOT NULL,
  cache_read   INTEGER NOT NULL,
  cache_write  INTEGER NOT NULL,
  responses    INTEGER NOT NULL,
  PRIMARY KEY (machine, conversation, day, model, subagent)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS usage_by_day ON usage (day);

CREATE TABLE IF NOT EXISTS conversations (
  machine      TEXT NOT NULL,
  id           TEXT NOT NULL,
  agent        TEXT NOT NULL,
  cwd          TEXT NOT NULL,
  cwd_is_main  INTEGER NOT NULL,
  last_unix    INTEGER NOT NULL,
  PRIMARY KEY (machine, id)
) WITHOUT ROWID;

-- A spawned Codex thread names the thread that spawned it.
CREATE TABLE IF NOT EXISTS parents (
  machine TEXT NOT NULL,
  child   TEXT NOT NULL,
  parent  TEXT NOT NULL,
  PRIMARY KEY (machine, child)
) WITHOUT ROWID;

-- Which session a conversation belonged to, remembered once seen: a session
-- holds only its current conversation id, and starting another overwrites it.
CREATE TABLE IF NOT EXISTS owners (
  conversation TEXT PRIMARY KEY,
  project_id   TEXT NOT NULL,
  project_name TEXT NOT NULL,
  session_id   TEXT NOT NULL,
  session_name TEXT NOT NULL,
  tab_name     TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS scans (
  machine   TEXT PRIMARY KEY,
  last_unix INTEGER NOT NULL,
  error     TEXT NOT NULL
) WITHOUT ROWID;
`

// Store is an open usage database.
type Store struct {
	db  *sql.DB
	loc *time.Location
}

// Open opens or creates the database.
func Open(path string, loc *time.Location) (*Store, error) {
	if loc == nil {
		loc = time.Local
	}
	// IMMEDIATE: a second app window may scan at the same moment, and Apply
	// compares a file's cursor before writing. A transaction that only took the
	// write lock on its first write could act on a cursor already moved on.
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(30000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Set("_txlock", "immediate")

	db, err := sql.Open("sqlite", databaseDSN(path)+"?"+query.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not prepare the usage database: %w", err)
	}
	return &Store{db: db, loc: loc}, nil
}

// databaseDSN builds the file URI. A Windows path carries a drive letter, which
// has to follow the triple slash, or the drive is read as the URI's host.
func databaseDSN(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return "file://" + (&url.URL{Path: clean}).EscapedPath()
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// FileState is what is known about a log file from earlier scans.
type FileState struct {
	Size   int64
	Cursor tokenusage.Cursor
}

// FileStates returns the known files of one machine.
func (s *Store) FileStates(ctx context.Context, machine string) (map[string]FileState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, size, cursor FROM files WHERE machine = ?`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]FileState{}
	for rows.Next() {
		var id, cursorJSON string
		var state FileState
		if err := rows.Scan(&id, &state.Size, &cursorJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(cursorJSON), &state.Cursor); err != nil {
			continue
		}
		states[id] = state
	}
	return states, rows.Err()
}

// Applied is one file's results with what the scan believed about it.
type Applied struct {
	FileID string
	// Size is recorded as the file's size once it has been read to the end, so
	// the next scan can skip it unless it has grown.
	Size int64
	// ExpectedOffset is the stored offset this read started from. Another
	// window that got there first has moved it, and then this read is dropped.
	ExpectedOffset int64
	Result         tokenusage.FileResult
}

type bucketKey struct {
	conversation string
	day          int
	model        string
	subagent     bool
}

type bucket struct {
	input, output, cacheRead, cacheWrite, responses int64
}

type conversationUpdate struct {
	agent, cwd string
	cwdIsMain  bool
	lastUnix   int64
}

// Apply stores one batch atomically: the rows and the cursors that say they
// were read commit together, so a crash cannot count anything twice or lose it.
func (s *Store) Apply(ctx context.Context, machine string, batch []Applied) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	readSeen, err := tx.PrepareContext(ctx, `SELECT output FROM seen WHERE machine = ? AND key = ?`)
	if err != nil {
		return err
	}
	defer readSeen.Close()
	writeSeen, err := tx.PrepareContext(ctx, `INSERT INTO seen (machine, key, output) VALUES (?, ?, ?)
		ON CONFLICT (machine, key) DO UPDATE SET output = excluded.output`)
	if err != nil {
		return err
	}
	defer writeSeen.Close()

	now := time.Now().Unix()
	buckets := map[bucketKey]*bucket{}
	conversations := map[string]*conversationUpdate{}

	for _, applied := range batch {
		var stored sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT cursor FROM files WHERE machine = ? AND id = ?`,
			machine, applied.FileID).Scan(&stored)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var storedOffset int64
		if stored.Valid {
			var cursor tokenusage.Cursor
			if json.Unmarshal([]byte(stored.String), &cursor) == nil {
				storedOffset = cursor.Offset
			}
		}
		if storedOffset != applied.ExpectedOffset {
			continue
		}

		result := applied.Result
		for _, row := range result.Rows {
			unix := row.Unix
			if unix == 0 {
				unix = now
			}
			added := bucket{
				input: row.Input, output: row.Output, cacheRead: row.CacheRead,
				cacheWrite: row.CacheWrite, responses: 1,
			}
			if row.Key != 0 {
				key := int64(row.Key)
				var seenOutput int64
				err := readSeen.QueryRowContext(ctx, machine, key).Scan(&seenOutput)
				switch {
				case err == sql.ErrNoRows:
					if _, err := writeSeen.ExecContext(ctx, machine, key, row.Output); err != nil {
						return err
					}
				case err != nil:
					return err
				case row.Output <= seenOutput:
					// Counted already. A later row of the same response can
					// carry a smaller output, and the larger one stands.
					continue
				default:
					// The response's first rows carry a placeholder output and
					// the real one arrives later, so only the difference is new.
					if _, err := writeSeen.ExecContext(ctx, machine, key, row.Output); err != nil {
						return err
					}
					added = bucket{output: row.Output - seenOutput}
				}
			}

			day := dayOf(unix, s.loc)
			keyOf := bucketKey{conversation: result.Conversation, day: day, model: row.Model, subagent: row.Subagent}
			total := buckets[keyOf]
			if total == nil {
				total = &bucket{}
				buckets[keyOf] = total
			}
			total.input += added.input
			total.output += added.output
			total.cacheRead += added.cacheRead
			total.cacheWrite += added.cacheWrite
			total.responses += added.responses

			update := conversations[result.Conversation]
			if update == nil {
				update = &conversationUpdate{agent: result.Agent}
				conversations[result.Conversation] = update
			}
			if unix > update.lastUnix {
				update.lastUnix = unix
			}
			// A subagent's directory is often its own worktree. The main
			// thread's says which project this was.
			if row.Cwd != "" && !update.cwdIsMain && (update.cwd == "" || !row.Subagent) {
				update.cwd = row.Cwd
				update.cwdIsMain = !row.Subagent
			}
		}

		if result.Parent != "" && result.Own != "" && result.Parent != result.Own {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO parents (machine, child, parent) VALUES (?, ?, ?)`,
				machine, result.Own, result.Parent); err != nil {
				return err
			}
		}

		cursorJSON, err := json.Marshal(result.Cursor)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO files (machine, id, size, cursor) VALUES (?, ?, ?, ?)
			ON CONFLICT (machine, id) DO UPDATE SET size = excluded.size, cursor = excluded.cursor`,
			machine, applied.FileID, applied.Size, string(cursorJSON)); err != nil {
			return err
		}
	}

	for key, total := range buckets {
		subagent := 0
		if key.subagent {
			subagent = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage
			(machine, conversation, day, model, subagent, input, output, cache_read, cache_write, responses)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (machine, conversation, day, model, subagent) DO UPDATE SET
			  input = input + excluded.input,
			  output = output + excluded.output,
			  cache_read = cache_read + excluded.cache_read,
			  cache_write = cache_write + excluded.cache_write,
			  responses = responses + excluded.responses`,
			machine, key.conversation, key.day, key.model, subagent,
			total.input, total.output, total.cacheRead, total.cacheWrite, total.responses); err != nil {
			return err
		}
	}

	for id, update := range conversations {
		isMain := 0
		if update.cwdIsMain {
			isMain = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversations (machine, id, agent, cwd, cwd_is_main, last_unix)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (machine, id) DO UPDATE SET
			  cwd = CASE WHEN conversations.cwd_is_main = 1 OR excluded.cwd = '' OR (conversations.cwd <> '' AND excluded.cwd_is_main = 0)
			             THEN conversations.cwd ELSE excluded.cwd END,
			  cwd_is_main = MAX(conversations.cwd_is_main, excluded.cwd_is_main),
			  last_unix = MAX(conversations.last_unix, excluded.last_unix)`,
			machine, id, update.agent, update.cwd, isMain, update.lastUnix); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// dayOf is a date as a number that sorts, in the viewer's own calendar: "today"
// has to mean the day they are having.
func dayOf(unix int64, loc *time.Location) int {
	year, month, day := time.Unix(unix, 0).In(loc).Date()
	return year*10000 + int(month)*100 + day
}

// DayOf exposes dayOf for the start of a period.
func (s *Store) DayOf(moment time.Time) int {
	return dayOf(moment.Unix(), s.loc)
}

// Owner says which session a conversation belonged to.
type Owner struct {
	Conversation string
	ProjectID    string
	ProjectName  string
	SessionID    string
	SessionName  string
	TabName      string
}

// RememberOwners stores what the sessions say now. Nothing is removed, so a
// conversation keeps its session after the session moves on or is deleted.
func (s *Store) RememberOwners(ctx context.Context, owners []Owner) error {
	if len(owners) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO owners
		(conversation, project_id, project_name, session_id, session_name, tab_name) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, owner := range owners {
		if owner.Conversation == "" {
			continue
		}
		if _, err := statement.ExecContext(ctx, owner.Conversation, owner.ProjectID, owner.ProjectName,
			owner.SessionID, owner.SessionName, owner.TabName); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Item is one conversation's use of one model, in a period.
type Item struct {
	Machine      string
	Conversation string
	Agent        string
	Cwd          string
	LastUnix     int64
	Model        string
	Subagent     bool
	Input        int64
	Output       int64
	CacheRead    int64
	CacheWrite   int64
	Responses    int64
	Owner        *Owner
}

// Items returns everything from a day on; fromDay 0 means all of it. A spawned
// thread's use is reported under the conversation at the top of its chain.
func (s *Store) Items(ctx context.Context, fromDay int) ([]Item, error) {
	parents, err := s.loadParents(ctx)
	if err != nil {
		return nil, err
	}
	owners, err := s.loadOwners(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT u.machine, u.conversation, u.model, u.subagent,
		  SUM(u.input), SUM(u.output), SUM(u.cache_read), SUM(u.cache_write), SUM(u.responses),
		  COALESCE(c.agent, ''), COALESCE(c.cwd, ''), COALESCE(c.last_unix, 0)
		FROM usage u
		LEFT JOIN conversations c ON c.machine = u.machine AND c.id = u.conversation
		WHERE u.day >= ?
		GROUP BY u.machine, u.conversation, u.model, u.subagent`, fromDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type mergeKey struct {
		machine, conversation, model string
		subagent                     bool
	}
	merged := map[mergeKey]*Item{}
	var order []mergeKey
	for rows.Next() {
		var item Item
		var subagent int
		if err := rows.Scan(&item.Machine, &item.Conversation, &item.Model, &subagent,
			&item.Input, &item.Output, &item.CacheRead, &item.CacheWrite, &item.Responses,
			&item.Agent, &item.Cwd, &item.LastUnix); err != nil {
			return nil, err
		}
		item.Subagent = subagent != 0

		root := rootOf(parents[item.Machine], item.Conversation)
		key := mergeKey{item.Machine, root, item.Model, item.Subagent || root != item.Conversation}
		existing := merged[key]
		if existing == nil {
			item.Conversation = root
			item.Subagent = key.subagent
			copied := item
			merged[key] = &copied
			order = append(order, key)
			continue
		}
		existing.Input += item.Input
		existing.Output += item.Output
		existing.CacheRead += item.CacheRead
		existing.CacheWrite += item.CacheWrite
		existing.Responses += item.Responses
		if item.LastUnix > existing.LastUnix {
			existing.LastUnix = item.LastUnix
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(order))
	for _, key := range order {
		item := merged[key]
		if owner, found := owners[item.Conversation]; found {
			copied := owner
			item.Owner = &copied
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) loadParents(ctx context.Context) (map[string]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT machine, child, parent FROM parents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parents := map[string]map[string]string{}
	for rows.Next() {
		var machine, child, parent string
		if err := rows.Scan(&machine, &child, &parent); err != nil {
			return nil, err
		}
		if parents[machine] == nil {
			parents[machine] = map[string]string{}
		}
		parents[machine][child] = parent
	}
	return parents, rows.Err()
}

func (s *Store) loadOwners(ctx context.Context) (map[string]Owner, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT conversation, project_id, project_name, session_id, session_name, tab_name FROM owners`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := map[string]Owner{}
	for rows.Next() {
		var owner Owner
		if err := rows.Scan(&owner.Conversation, &owner.ProjectID, &owner.ProjectName,
			&owner.SessionID, &owner.SessionName, &owner.TabName); err != nil {
			return nil, err
		}
		owners[owner.Conversation] = owner
	}
	return owners, rows.Err()
}

// rootOf follows a spawned thread up to the conversation that started the
// chain. Bounded, in case a damaged log names a thread as its own ancestor.
func rootOf(parents map[string]string, conversation string) string {
	current := conversation
	for range 32 {
		parent, found := parents[current]
		if !found || parent == "" {
			return current
		}
		current = parent
	}
	return conversation
}

// Scan is the outcome of the last look at a machine.
type Scan struct {
	Machine  string
	LastUnix int64
	Error    string
}

// RecordScan notes a scan. A failed one keeps the time of the last success, so
// the view can say how old the numbers are.
func (s *Store) RecordScan(ctx context.Context, machine string, when time.Time, scanErr error) error {
	if scanErr != nil {
		message := strings.SplitN(scanErr.Error(), "\n", 2)[0]
		_, err := s.db.ExecContext(ctx, `INSERT INTO scans (machine, last_unix, error) VALUES (?, 0, ?)
			ON CONFLICT (machine) DO UPDATE SET error = excluded.error`, machine, message)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO scans (machine, last_unix, error) VALUES (?, ?, '')`,
		machine, when.Unix())
	return err
}

// Scans returns the outcome of the last look at each machine.
func (s *Store) Scans(ctx context.Context) (map[string]Scan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT machine, last_unix, error FROM scans`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scans := map[string]Scan{}
	for rows.Next() {
		var scan Scan
		if err := rows.Scan(&scan.Machine, &scan.LastUnix, &scan.Error); err != nil {
			return nil, err
		}
		scans[scan.Machine] = scan
	}
	return scans, rows.Err()
}
