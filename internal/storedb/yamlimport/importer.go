// Package yamlimport holds the one-time importer that moves the four legacy
// YAML stores (members, invites, tokens+roles, groups+memberships) into the
// unified store database whose schema internal/storedb owns. It lives in its
// own subpackage so storedb stays a leaf: the importer necessarily imports
// every store package (their loaders are the YAML format spec), while the
// store packages' tests need storedb's schema — a single package would be an
// import cycle.
package yamlimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

// Sources names the legacy YAML files the importer consumes — the same six
// paths the daemon resolves from its config (tokens.roles_file,
// tokens.tokens_file, and the four store files defaulting beside
// tokens_file). A missing file imports as whatever today's boot loads for a
// missing file: empty for members/invites/groups, the built-in default roles
// for a missing roles.yml.
type Sources struct {
	RolesFile       string
	TokensFile      string
	MembersFile     string
	InvitesFile     string
	GroupsFile      string
	MembershipsFile string
}

// list returns the sources in daemon load order.
func (s Sources) list() []string {
	return []string{s.RolesFile, s.TokensFile, s.GroupsFile, s.MembershipsFile, s.MembersFile, s.InvitesFile}
}

// importDescription is the schema_migrations row text stamped by Import.
const importDescription = "initial schema + yaml import"

// Import performs the one-time YAML→SQLite migration into database, which
// must have been opened with db.OpenStrict. It is single-threaded boot code:
// call it before any store is constructed.
//
// Semantics (research doc §5.1):
//
//   - If schema_migrations already records version 1, the database is the
//     source of truth: Import warns about any leftover *.yml (distinguishing
//     stale residue from operator-restored divergent content via the
//     import_journal sha256) and returns nil without touching anything.
//   - Otherwise every YAML source is parsed through the existing store
//     loaders — they are the only format spec — failing closed on everything
//     that fails daemon boot today (the loaders' own dangling-membership
//     fail-soft drop is inherited). Any load error aborts before a single
//     row is written.
//   - All INSERTs, the schema_migrations version row, and the import_journal
//     rows commit in ONE transaction (run under context.WithoutCancel), so
//     the import can never half-succeed.
//   - Only after COMMIT is each source renamed to <name>.migrated (the
//     rollback artifact; os.Rename preserves permissions). A rename failure
//     is loudly warned but not fatal — the version row already guarantees
//     idempotency.
//
// now is the injected clock (imported_at / applied_at stamps); log may be nil.
func Import(ctx context.Context, database *sql.DB, src Sources, now func() time.Time, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	// The write transaction must survive caller cancellation: an interrupted
	// COMMIT would be fine (rolls back), but a cancel between COMMIT and the
	// renames would leave consumed YAML behind, so the whole import runs
	// detached from the request context.
	ctx = context.WithoutCancel(ctx)

	if err := storedb.EnsureSchema(database); err != nil {
		return err
	}

	imported, err := alreadyImported(ctx, database)
	if err != nil {
		return err
	}
	if imported {
		warnLeftovers(ctx, database, src, log)
		return nil
	}

	// Parse ALL sources through the existing loaders before writing anything:
	// a poisoned file in any store must leave the database untouched and
	// every YAML file in place. The throwaway stores are read-only inputs —
	// all four are sink-less (write-through stores with no database attached
	// never touch the filesystem).
	tokenStore := tokens.NewStore()
	if err := tokenStore.LoadFromFiles(src.RolesFile, src.TokensFile); err != nil {
		return fmt.Errorf("storedb: import: %w", err)
	}

	groupStore := groups.NewStore()
	// Match the daemon exactly: memberships are keyed by the immutable member
	// UUID, so a legacy email-keyed memberships.yml must refuse to import
	// just as it refuses to boot today.
	groupStore.SetPersonKeyValidator(members.ValidateID)
	if err := groupStore.LoadFromFiles(src.GroupsFile, src.MembershipsFile); err != nil {
		return fmt.Errorf("storedb: import: %w", err)
	}

	memberStore := members.NewStore()
	if err := memberStore.LoadFromFile(src.MembersFile); err != nil {
		return fmt.Errorf("storedb: import: %w", err)
	}

	inviteStore := invites.NewStore()
	if err := inviteStore.LoadFromFile(src.InvitesFile); err != nil {
		return fmt.Errorf("storedb: import: %w", err)
	}

	// Hash the sources before writing so the journal records exactly the
	// content that was parsed. Missing files get no journal row.
	journal, err := hashSources(src)
	if err != nil {
		return err
	}

	tokenRows, hadDuplicates := tokenStore.ExportTokens()
	if hadDuplicates {
		log.Warn("storedb: import: tokens.yml contained duplicate user or token entries; keeping the last occurrence (matches today's effective behavior)",
			"source", src.TokensFile)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storedb: import: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertRoles(ctx, tx, tokenStore.ExportRoles()); err != nil {
		return err
	}
	if err := insertTokens(ctx, tx, tokenRows, log); err != nil {
		return err
	}
	if err := insertMembers(ctx, tx, memberStore.List(), log); err != nil {
		return err
	}
	if err := insertGroups(ctx, tx, groupStore.ListGroups(), log); err != nil {
		return err
	}
	if err := insertMemberships(ctx, tx, groupStore.ListMemberships(), log); err != nil {
		return err
	}
	if err := insertReadExclusions(ctx, tx, groupStore.ExportReadExclusions(), log); err != nil {
		return err
	}
	if err := insertInvites(ctx, tx, inviteStore.Export(), log); err != nil {
		return err
	}

	stamp := storedb.FormatTime(now())
	for _, j := range journal {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO import_journal (source, sha256, imported_at) VALUES (?, ?, ?)`,
			j.source, j.sha256, stamp); err != nil {
			return fmt.Errorf("storedb: import: journal %s: %w", j.source, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at, description) VALUES (?, ?, ?)`,
		storedb.SchemaVersion, stamp, importDescription); err != nil {
		return fmt.Errorf("storedb: import: schema_migrations: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storedb: import: commit: %w", err)
	}

	// Only after COMMIT: park each consumed source as the rollback artifact.
	for _, j := range journal {
		migrated := j.original + ".migrated"
		if err := os.Rename(j.original, migrated); err != nil {
			log.Warn("storedb: import committed but renaming a consumed YAML source failed; the database is authoritative — move the file aside manually",
				"source", j.original, "target", migrated, "error", err)
		}
	}
	log.Info("storedb: yaml import complete", "sources", len(journal))
	return nil
}

// alreadyImported reports whether schema_migrations records storedb.SchemaVersion.
func alreadyImported(ctx context.Context, database *sql.DB) (bool, error) {
	var n int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, storedb.SchemaVersion).Scan(&n); err != nil {
		return false, fmt.Errorf("storedb: import: read schema_migrations: %w", err)
	}
	return n > 0, nil
}

// warnLeftovers names every YAML source still present after an import,
// classifying it by content against the import_journal sha256: identical
// content is stale residue, divergent content means an operator restored a
// newer YAML that the database will NOT pick up.
func warnLeftovers(ctx context.Context, database *sql.DB, src Sources, log *slog.Logger) {
	for _, path := range src.list() {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue // missing (or unreadable) — nothing left over
		}
		abs := absPath(path)
		sum := sha256Hex(data)
		var recorded string
		err = database.QueryRowContext(ctx,
			`SELECT sha256 FROM import_journal WHERE source = ?`, abs).Scan(&recorded)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			log.Warn("storedb: leftover YAML file was never imported; the database is authoritative and the file is ignored",
				"source", path)
		case err != nil:
			log.Warn("storedb: leftover YAML file present but the import journal could not be read",
				"source", path, "error", err)
		case recorded == sum:
			log.Warn("storedb: leftover YAML file matches the imported content (stale residue; safe to remove)",
				"source", path)
		default:
			log.Warn("storedb: leftover YAML file DIFFERS from the imported content; the database is authoritative and the file is ignored — to re-import, move runeconsole.db aside first",
				"source", path)
		}
	}
}

// journalEntry pairs a source file with its content hash.
type journalEntry struct {
	original string // path as configured (rename target base)
	source   string // absolute path recorded in the journal
	sha256   string
}

// hashSources reads every existing source and returns its journal entry.
func hashSources(src Sources) ([]journalEntry, error) {
	var out []journalEntry
	for _, path := range src.list() {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("storedb: import: hash %s: %w", path, err)
		}
		out = append(out, journalEntry{original: path, source: absPath(path), sha256: sha256Hex(data)})
	}
	return out, nil
}

func insertRoles(ctx context.Context, tx *sql.Tx, roles []tokens.Role) error {
	for _, r := range roles {
		scope := r.Scope
		if scope == nil {
			scope = []string{}
		}
		scopeJSON, err := json.Marshal(scope)
		if err != nil {
			return fmt.Errorf("storedb: import: encode scope of role %q: %w", r.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO roles (name, scope, top_k, rate_limit) VALUES (?, ?, ?, ?)`,
			r.Name, string(scopeJSON), r.TopK, r.RateLimit); err != nil {
			return fmt.Errorf("storedb: import: role %q: %w", r.Name, err)
		}
	}
	return nil
}

func insertTokens(ctx context.Context, tx *sql.Tx, rows []tokens.Token, log *slog.Logger) error {
	for _, t := range rows {
		// issued_at keeps a legacy empty value as '' (satisfies NOT NULL and
		// preserves today's "unparseable == no expiry span" rotation
		// semantics); expires/last_used use NULL for "never"/"unset".
		// issued_at and expires stay DATE-ONLY (YYYY-MM-DD): day-granularity
		// expiry is a documented contract, deliberately NOT an instant.
		// last_used is an instant — the YAML loader never populates it, but
		// if a hand-edited legacy file ever carried one it is normalized to
		// the canonical millisecond form like every other instant.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tokens (user, token, role, issued_at, expires, last_used) VALUES (?, ?, ?, ?, ?, ?)`,
			t.User, t.Token, t.Role, t.IssuedAt, nullIfEmpty(t.Expires),
			nullIfEmpty(canonicalOrVerbatim(t.LastUsed, "tokens", "last_used", t.User, log))); err != nil {
			return fmt.Errorf("storedb: import: token for user %q: %w", t.User, err)
		}
	}
	return nil
}

func insertMembers(ctx context.Context, tx *sql.Tx, rows []members.Member, log *slog.Logger) error {
	for _, m := range rows {
		// Sanitize inconsistent legacy disabled_from markers instead of
		// failing the import (branch decision): today's loader never inspects
		// the field, so a marker on a non-disabled row, or a value outside
		// the restorable statuses, must not become a new rejection class.
		disabledFrom := m.DisabledFrom
		if disabledFrom != "" && (m.Status != members.StatusDisabled || !restorableStatus(disabledFrom)) {
			log.Warn("storedb: import: member has an inconsistent disabled_from marker; importing it as unset",
				"member_id", m.ID, "status", m.Status, "disabled_from", disabledFrom)
			disabledFrom = ""
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO members (id, email, display_name, status, disabled_from, created_at, session_expired_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Email, m.DisplayName, m.Status, nullIfEmpty(disabledFrom),
			canonicalOrVerbatim(m.CreatedAt, "members", "created_at", m.ID, log),
			nullIfEmpty(canonicalOrVerbatim(m.SessionExpiredAt, "members", "session_expired_at", m.ID, log))); err != nil {
			return fmt.Errorf("storedb: import: member %q: %w", m.ID, err)
		}
	}
	return nil
}

// restorableStatus reports whether s is a status a disabled member may be
// restored to (the members disabled_from CHECK domain).
func restorableStatus(s string) bool {
	switch s {
	case members.StatusRegistered, members.StatusInvited, members.StatusActive:
		return true
	default:
		return false
	}
}

func insertGroups(ctx context.Context, tx *sql.Tx, rows []groups.GroupInfo, log *slog.Logger) error {
	// ListGroups yields DFS order (parents before children) — no FK on
	// parent_id, but the deterministic order keeps failures reproducible.
	for _, g := range rows {
		// Group ids are FHE record tags: copied byte-verbatim, never reshaped.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, ?, ?, ?)`,
			g.ID, g.Name, g.ParentID,
			canonicalOrVerbatim(g.CreatedAt, "groups", "created_at", g.ID, log)); err != nil {
			return fmt.Errorf("storedb: import: group %q: %w", g.ID, err)
		}
	}
	return nil
}

func insertMemberships(ctx context.Context, tx *sql.Tx, rows []groups.Membership, log *slog.Logger) error {
	// Dangling memberships (two-file crash residue) were already dropped
	// fail-soft with a warning by the groups loader — the one fail-soft case
	// of the whole import — so every row here satisfies the group_id FK.
	for _, m := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memberships (user, group_id, role, granted_by, granted_at) VALUES (?, ?, ?, ?, ?)`,
			m.User, m.GroupID, string(m.Role), m.GrantedBy,
			canonicalOrVerbatim(m.GrantedAt, "memberships", "granted_at", m.User+" on "+m.GroupID, log)); err != nil {
			return fmt.Errorf("storedb: import: membership %q on %q: %w", m.User, m.GroupID, err)
		}
	}
	return nil
}

func insertReadExclusions(ctx context.Context, tx *sql.Tx, rows []groups.ReadExclusion, log *slog.Logger) error {
	// A dropped exclusion fails OPEN — the inherited read an admin removed
	// would come back after the migration — so these rows are as
	// security-relevant as the memberships they subtract from. Group-less
	// exclusions were already dropped with a warning by the groups loader
	// (same rationale as its dangling-membership fail-soft), so every row
	// here satisfies the group_id FK.
	for _, d := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO read_exclusions (user, group_id, removed_by, removed_at) VALUES (?, ?, ?, ?)`,
			d.User, d.GroupID, d.RemovedBy,
			canonicalOrVerbatim(d.RemovedAt, "read_exclusions", "removed_at", d.User+" on "+d.GroupID, log)); err != nil {
			return fmt.Errorf("storedb: import: read exclusion %q on %q: %w", d.User, d.GroupID, err)
		}
	}
	return nil
}

func insertInvites(ctx context.Context, tx *sql.Tx, rows []invites.Invite, log *slog.Logger) error {
	for _, inv := range rows {
		// Pending rows keep their sealed plaintext byte-verbatim ('' — the
		// aged-out scrub — becomes NULL). Non-pending rows always store NULL:
		// today every persist scrubs their token_value, and the schema CHECK
		// (token_value IS NULL OR status = 'pending') encodes that scrub.
		var tokenValue any
		if inv.Status == invites.StatusPending && inv.TokenValue != "" {
			tokenValue = inv.TokenValue
		}
		// expires_at is normalized to the canonical storedb.TimeFormat
		// (RFC3339 UTC, fixed three-digit milliseconds): the store's
		// aged-pending sweep compares the column TEXTUALLY, so an offset or
		// second-precision form from hand-edited YAML (legal for the loader,
		// same instant) would be swept at the wrong time — a negative offset
		// even prematurely, scrubbing a still-valid invite, and a bare-
		// seconds value interleaves wrongly with millisecond ones. The
		// loader already guaranteed parseability (LoadFromDB refuses
		// non-canonical rows at every later boot), so this stays fail-closed
		// — unlike the loader-unvalidated fields canonicalOrVerbatim serves;
		// only the rendering changes.
		expiresAt := inv.ExpiresAt
		if expiresAt != "" {
			canonical, err := storedb.CanonicalizeTime(expiresAt)
			if err != nil {
				return fmt.Errorf("storedb: import: invite %q: unparseable expires_at %q: %w", inv.Handle, expiresAt, err)
			}
			expiresAt = canonical
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, creation_path, created_at, expires_at, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			inv.Handle, inv.LeaseID, inv.MemberID, inv.Email, tokenValue, inv.Role,
			inv.CreationPath,
			canonicalOrVerbatim(inv.CreatedAt, "invites", "created_at", inv.Handle, log),
			nullIfEmpty(expiresAt), inv.Status); err != nil {
			return fmt.Errorf("storedb: import: invite %q: %w", inv.Handle, err)
		}
	}
	return nil
}

// nullIfEmpty maps the stores' "" empty-value convention to SQL NULL.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// canonicalOrVerbatim normalizes one timestamp value to the canonical
// storedb.TimeFormat for a column whose legacy loader accepts the field
// WITHOUT parsing it (members.created_at/session_expired_at,
// invites.created_at, groups.created_at, memberships.granted_at,
// read_exclusions.removed_at, tokens.last_used). Normalization matters
// because the canonical form is millisecond-precision: a legacy
// second-precision value textually interleaves WRONGLY with store-written
// millisecond values ("...05Z" sorts after "...05.999Z"), so every stored
// value must share the one canonical width for textual ORDER BY /
// comparison to stay chronological.
//
// Empty stays empty (the stores' ""/NULL convention). An unparseable
// non-empty value imports VERBATIM with a warning instead of failing the
// import: today's loaders tolerate such values for these fields, and the
// import must not add a new rejection class — ordering for such rows was
// already undefined in the YAML era, so preserving the bytes preserves
// exactly today's behavior. Fields the loader DOES require parseable
// (invites.expires_at) keep their fail-closed path in their insert func.
func canonicalOrVerbatim(value, table, column, rowRef string, log *slog.Logger) string {
	if value == "" {
		return ""
	}
	canonical, err := storedb.CanonicalizeTime(value)
	if err != nil {
		log.Warn("storedb: import: timestamp is not RFC3339; importing it verbatim (row ordering against it was already undefined in the YAML era)",
			"table", table, "column", column, "row", rowRef, "value", value)
		return value
	}
	return canonical
}

// absPath resolves path for the import journal; on failure the configured
// path is used as-is (still a stable key).
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// sha256Hex returns the lowercase hex SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
