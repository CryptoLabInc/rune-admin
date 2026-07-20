package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openStrictTemp opens a fresh OpenStrict database under t.TempDir and
// registers cleanup. Never ":memory:" — the pool semantics differ.
func openStrictTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "strict.db")
	database, err := OpenStrict(path)
	if err != nil {
		t.Fatalf("OpenStrict: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, path
}

func TestOpenStrictAppliesStrictPragmas(t *testing.T) {
	database, _ := openStrictTemp(t)

	var journalMode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	intPragmas := map[string]int64{
		"synchronous":        2, // FULL
		"secure_delete":      1, // ON
		"foreign_keys":       1, // ON
		"busy_timeout":       5000,
		"journal_size_limit": strictJournalSizeLimit,
	}
	for pragma, want := range intPragmas {
		var got int64
		if err := database.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %d, want %d", pragma, got, want)
		}
	}
}

func TestOpenStrictCreatesFileOwnerOnly(t *testing.T) {
	_, path := openStrictTemp(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("new db file mode = %04o, want 0600", perm)
	}
}

// TestOpenNamesThePathWhenItCannotCreate pins the error text, not just the
// failure. Both openers pre-create the file to fix its mode before the driver
// sees it; when that fails they used to fall through and let the driver report
// a bare "unable to open database file", which names neither the path nor the
// reason. A missing parent directory stands in for the whole class (wrong
// ownership, read-only mount, full disk) — the operator needs the path back.
func TestOpenNamesThePathWhenItCannotCreate(t *testing.T) {
	for name, open := range map[string]func(string) (*sql.DB, error){
		"Open":       Open,
		"OpenStrict": OpenStrict,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "absent-dir", "store.db")
			database, err := open(path)
			if err == nil {
				_ = database.Close()
				t.Fatal("open under a missing directory succeeded, want an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("err = %v, want it to name %q", err, path)
			}
			if !strings.Contains(err.Error(), "no such file or directory") {
				t.Errorf("err = %v, want it to carry the underlying cause", err)
			}
		})
	}
}

func TestOpenStrictRefusesLoosePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loose.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStrict(path); err == nil {
		t.Fatal("OpenStrict accepted a 0644 database file, want refusal")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the offending file: %v", err)
	}
	// No silent chmod: the file must be left as found.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("refused file was chmod'ed to %04o, want untouched 0644", perm)
	}

	// Tightening the mode makes the same path acceptable.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := OpenStrict(path)
	if err != nil {
		t.Fatalf("OpenStrict after chmod 0600: %v", err)
	}
	_ = database.Close()
}

func TestOpenStrictRefusesLooseSidecar(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.db")
			// Materialize a valid 0600 main file first.
			database, err := OpenStrict(path)
			if err != nil {
				t.Fatal(err)
			}
			_ = database.Close()

			// A leftover sidecar may exist after Close; chmod explicitly so the
			// loose mode is guaranteed regardless.
			sidecar := path + suffix
			if err := os.WriteFile(sidecar, nil, 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(sidecar, 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenStrict(path); err == nil {
				t.Fatalf("OpenStrict accepted a 0640 %s sidecar, want refusal", suffix)
			} else if !strings.Contains(err.Error(), sidecar) {
				t.Errorf("error does not name the offending sidecar: %v", err)
			}
		})
	}
}

func TestOpenStrictIntegrityCheckPassesAcrossReopen(t *testing.T) {
	database, path := openStrictTemp(t)
	if _, err := database.Exec("CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('x')"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	// The reopen runs PRAGMA integrity_check internally; a healthy database
	// must open and still hold the committed row.
	reopened, err := OpenStrict(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	var v string
	if err := reopened.QueryRow("SELECT v FROM t").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "x" {
		t.Errorf("v = %q, want x", v)
	}
}

// TestOpenStrictTxLockImmediate proves _txlock=immediate is in effect: a
// transaction that has not executed any statement yet must already hold the
// write lock, so a second connection's BEGIN IMMEDIATE fails busy instead of
// succeeding (a DEFERRED begin would take no lock at all).
func TestOpenStrictTxLockImmediate(t *testing.T) {
	database, path := openStrictTemp(t)

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	// Independent probe connection with a short busy timeout so the test does
	// not wait out OpenStrict's 5s.
	probe, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(100)", path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Close() }()
	probe.SetMaxOpenConns(1)
	conn, err := probe.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err == nil {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		t.Fatal("BEGIN IMMEDIATE on a second connection succeeded while an OpenStrict transaction was idle — _txlock=immediate is not in effect")
	}

	// Releasing the transaction frees the write lock for the probe.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("BEGIN IMMEDIATE after rollback: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}
