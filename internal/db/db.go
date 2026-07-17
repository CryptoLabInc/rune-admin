// Package db opens the SQLite database that backs the console session store.
// The file holds the runespace-cloud session bearer token at rest, so it is
// kept owner-only (0600); SQLite derives the -wal/-shm sidecar permissions
// from it.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo
)

// Open opens the SQLite database at path with WAL and a busy timeout, then
// verifies the connection. Tests should pass a file under t.TempDir() —
// ":memory:" is unsafe here because the database/sql pool hands every pool
// connection its own private in-memory database.
func Open(path string) (*sql.DB, error) {
	onDisk := !strings.Contains(path, ":memory:")
	if onDisk {
		// Pre-create with owner-only perms so a brand-new file is never briefly
		// world-readable.
		if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err == nil {
			_ = f.Close()
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	if onDisk {
		// Tighten a pre-existing file that may have been created with looser perms.
		_ = os.Chmod(path, 0o600)
	}
	return database, nil
}

// strictJournalSizeLimit caps how large the -wal sidecar may stay after a
// checkpoint or journal reset. secure_delete zeroes freed pages inside the
// main file, but scrubbed secrets (invite/token plaintext) can still linger
// in WAL frames until the sidecar is truncated — a modest limit shortens that
// residue window. 4 MiB is orders of magnitude above this database's working
// set (small human-scale rows) so it never throttles normal operation.
const strictJournalSizeLimit = 4 << 20 // 4 MiB

// OpenStrict opens the unified store database (runeconsole.db) with the
// hardened posture the YAML-store migration requires on top of Open's
// pragmas (busy_timeout, WAL, foreign_keys):
//
//   - _txlock=immediate (DSN parameter, not a pragma): database/sql has no
//     per-transaction BEGIN IMMEDIATE API, so this is the only lever that
//     makes every driver-level BEGIN take the write lock up front. Without
//     it a DEFERRED read→write promotion bypasses busy_timeout and fails
//     instantly with SQLITE_BUSY.
//   - synchronous(FULL): per-commit fsync — preserves the invites store's
//     "durable before the response escapes" contract at the DB layer.
//   - secure_delete(ON): scrubbed invite/token plaintext must not linger in
//     free pages (or in VACUUM INTO backups, which skip free pages).
//   - journal_size_limit: see strictJournalSizeLimit.
//   - SetMaxOpenConns(1): reads are served from in-memory caches and writes
//     are serialized by store mutexes, so a pool adds only re-entrancy risk.
//   - PRAGMA integrity_check must report exactly "ok" or the open is refused.
//
// Unlike Open, an existing database file — or a -wal/-shm sidecar — whose
// permissions grant any group/other access is refused outright instead of
// silently chmod'ed: this file holds invite and token plaintext, so looser
// perms mean a secret may already be exposed (fail closed, mirroring the
// invites.yml load check). The 0600 pre-create for brand-new files is kept.
//
// Tests should pass a file under t.TempDir(); ":memory:"-style paths skip
// all file handling but are unsafe with more than one pool connection.
func OpenStrict(path string) (*sql.DB, error) {
	onDisk := !strings.Contains(path, ":memory:")
	if onDisk {
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			info, err := os.Stat(p)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("db: stat %s: %w", p, err)
			}
			if perm := info.Mode().Perm(); perm&0o077 != 0 {
				return nil, fmt.Errorf("db: %s has too-permissive mode %04o (must be 0600, owner-only)", p, perm)
			}
		}
		// Pre-create with owner-only perms so a brand-new file is never briefly
		// world-readable (sidecars inherit their permissions from it).
		if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err == nil {
			_ = f.Close()
		}
	}
	dsn := fmt.Sprintf("file:%s?_txlock=immediate"+
		"&_pragma=busy_timeout(5000)"+
		"&_pragma=journal_mode(WAL)"+
		"&_pragma=foreign_keys(ON)"+
		"&_pragma=synchronous(FULL)"+
		"&_pragma=secure_delete(ON)"+
		"&_pragma=journal_size_limit(%d)", path, strictJournalSizeLimit)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := integrityCheck(database); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("db: integrity check of %s failed, refusing to open: %w", path, err)
	}
	return database, nil
}

// integrityCheck runs PRAGMA integrity_check and returns an error unless it
// yields exactly one row reading "ok".
func integrityCheck(database *sql.DB) error {
	rows, err := database.Query("PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var results []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		results = append(results, line)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("integrity_check reported %q", strings.Join(results, "; "))
	}
	return nil
}
