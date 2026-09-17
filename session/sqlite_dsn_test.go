package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// The databases this package opens belong to running agents. Opening one
// writable is not a theoretical risk: a write from here lands in the middle of
// the conversation the user is having.
//
// This is the test that would have caught the driver change. A bare path with
// "?mode=ro" appended reads fine and writes fine — the parameter is taken for
// part of the filename, and nothing reports it. Only the "file:" form makes the
// driver parse it.
func TestReadOnlyDSNRefusesWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	seedSQLiteStore(t, path)

	db, err := sql.Open("sqlite", readOnlySQLiteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow("SELECT a FROM t").Scan(&got); err != nil {
		t.Fatalf("the store cannot be read at all: %v", err)
	}
	if got != "original" {
		t.Fatalf("read back %q", got)
	}

	if _, err := db.Exec("INSERT INTO t VALUES ('intruder')"); err == nil {
		t.Error("a write succeeded against a store opened read-only — the agent's " +
			"own database is writable from here")
	}
}

// The prefix is the whole mechanism, so it is pinned rather than left to a
// reader to infer from behaviour.
func TestReadOnlyDSNIsAURI(t *testing.T) {
	dsn := readOnlySQLiteDSN("/home/user/.cursor/chats/abc/store.db")

	if !strings.HasPrefix(dsn, "file://") {
		t.Errorf("DSN = %q; without the file: prefix mode=ro is read as part of "+
			"the filename and the store opens writable", dsn)
	}
	if !strings.HasSuffix(dsn, "?mode=ro") {
		t.Errorf("DSN = %q; the read-only parameter is gone", dsn)
	}
}

// A store under a directory with a space or an accent in its name is ordinary —
// "Program Files", or a Hungarian user's Documents folder. Concatenated into a
// URI unescaped, those produce a DSN that either fails to open or points
// somewhere else.
func TestReadOnlyDSNEscapesThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my projects", "árvíztűrő")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "store.db")
	seedSQLiteStore(t, path)

	dsn := readOnlySQLiteDSN(path)
	if strings.Contains(dsn, " ") {
		t.Errorf("DSN = %q still contains a raw space", dsn)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow("SELECT a FROM t").Scan(&got); err != nil {
		t.Fatalf("a store under a path with a space and an accent could not be "+
			"read: %v (dsn %s)", err, dsn)
	}
}

// seedSQLiteStore writes a small database the way an agent would leave one.
func seedSQLiteStore(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (a TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES ('original')"); err != nil {
		t.Fatal(err)
	}
}
