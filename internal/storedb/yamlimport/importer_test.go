package yamlimport

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// openTestDB opens a fresh strict database under t.TempDir (never :memory: —
// the pool hands each connection its own private in-memory DB).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(filepath.Join(t.TempDir(), "runeconsole.db"))
	if err != nil {
		t.Fatalf("OpenStrict: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// countRows returns SELECT COUNT(*) FROM table.
func countRows(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// Fixture identities shared across importer tests. Member ids must be
// canonical UUID-shaped (members.ValidateID gates both the members loader
// and, injected, the memberships loader); group ids are deliberately NOT
// UUIDs — today's loader accepts them and they must import byte-verbatim.
const (
	aliceID = "11111111-1111-1111-1111-111111111111"
	bobID   = "22222222-2222-2222-2222-222222222222"
	carolID = "33333333-3333-3333-3333-333333333333"

	pendingHandle  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pendingLease   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pendingToken   = "evt_dddddddddddddddddddddddddddddddd"
	consumedHandle = "cccccccccccccccccccccccccccccccc"
	consumedLease  = "dddddddddddddddddddddddddddddddd"
)

var testStamp = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func testClock() time.Time { return testStamp }

// testLogger returns a logger capturing warnings for assertion.
func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func writeSource(t *testing.T, path, body string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
}

const happyRolesYAML = `roles:
  admin:
    scope: [get_public_key, decrypt_scores, decrypt_metadata, manage_tokens]
    top_k: 50
    rate_limit: 150/60s
  analyst:
    scope: [decrypt_scores]
    top_k: 7
    rate_limit: 10/60s
`

const happyTokensYAML = `tokens:
  - user: alice@corp.com
    token: evt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    role: admin
    issued_at: "2026-01-02"
  - user: bob@corp.com
    token: evt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    role: member
    created: "2026-01-03"
    expires: "2026-12-31"
  - user: legacy@corp.com
    token: evt_cccccccccccccccccccccccccccccccc
    role: member
`

var happyMembersYAML = `members:
  - id: ` + aliceID + `
    email: alice@corp.com
    display_name: Alice
    status: active
    created_at: "2026-01-01T00:00:00Z"
  - id: ` + bobID + `
    email: bob@corp.com
    display_name: Bob
    status: disabled
    disabled_from: invited
    created_at: "2026-01-02T00:00:00Z"
    session_expired_at: "2026-01-05T00:00:00Z"
  - id: ` + carolID + `
    email: carol@corp.com
    display_name: Carol
    status: active
    disabled_from: invited
    created_at: "2026-01-03T00:00:00Z"
`

var happyInvitesYAML = `invites:
  - handle: ` + pendingHandle + `
    member_id: ` + aliceID + `
    email: alice@corp.com
    token_value: ` + pendingToken + `
    role: member
    lease_id: ` + pendingLease + `
    creation_path: admin.member.invite
    created_at: "2026-01-04T00:00:00Z"
    expires_at: "2026-01-05T00:00:00Z"
    status: pending
  - handle: ` + consumedHandle + `
    member_id: ` + bobID + `
    email: bob@corp.com
    token_value: ""
    role: member
    lease_id: ` + consumedLease + `
    creation_path: admin.member.invite
    created_at: "2026-01-03T00:00:00Z"
    expires_at: ""
    status: consumed
`

const happyGroupsYAML = `groups:
  - id: aaaa
    name: Engineering
    created_at: "2026-01-01T00:00:00Z"
  - id: bbbb
    name: Backend
    parent_id: aaaa
    created_at: "2026-01-02T00:00:00Z"
`

var happyMembershipsYAML = `memberships:
  - user: ` + aliceID + `
    group_id: aaaa
    role: write
    granted_by: local-admin:owner@corp.com
    granted_at: "2026-01-02T00:00:00Z"
`

// seedSources writes the happy-path YAML corpus into dir and returns the
// matching Sources. invites.yml must be 0600 (the loader fail-closes on
// looser perms).
func seedSources(t *testing.T, dir string) Sources {
	t.Helper()
	src := Sources{
		RolesFile:       filepath.Join(dir, "roles.yml"),
		TokensFile:      filepath.Join(dir, "tokens.yml"),
		MembersFile:     filepath.Join(dir, "members.yml"),
		InvitesFile:     filepath.Join(dir, "invites.yml"),
		GroupsFile:      filepath.Join(dir, "groups.yml"),
		MembershipsFile: filepath.Join(dir, "memberships.yml"),
	}
	writeSource(t, src.RolesFile, happyRolesYAML, 0o600)
	writeSource(t, src.TokensFile, happyTokensYAML, 0o600)
	writeSource(t, src.MembersFile, happyMembersYAML, 0o600)
	writeSource(t, src.InvitesFile, happyInvitesYAML, 0o600)
	writeSource(t, src.GroupsFile, happyGroupsYAML, 0o600)
	writeSource(t, src.MembershipsFile, happyMembershipsYAML, 0o600)
	return src
}

// assertNothingImported asserts a failed import wrote no rows, stamped no
// version, and renamed no source.
func assertNothingImported(t *testing.T, database *sql.DB, src Sources) {
	t.Helper()
	for _, table := range []string{"schema_migrations", "import_journal", "members", "invites", "roles", "tokens", "groups", "memberships"} {
		if n := countRows(t, database, table); n != 0 {
			t.Errorf("%s has %d rows after failed import, want 0", table, n)
		}
	}
	for _, path := range src.list() {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("source %s missing after failed import (renamed?): %v", path, err)
		}
		if _, err := os.Stat(path + ".migrated"); err == nil {
			t.Errorf("%s.migrated exists after failed import", path)
		}
	}
}

func TestImportHappyPath(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	log, logs := testLogger()

	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Version row stamped with the injected clock.
	var appliedAt, description string
	if err := database.QueryRow(
		`SELECT applied_at, description FROM schema_migrations WHERE version = 1`).Scan(&appliedAt, &description); err != nil {
		t.Fatalf("version row: %v", err)
	}
	if want := storedb.FormatTime(testStamp); appliedAt != want {
		t.Errorf("applied_at = %q, want %q (injected clock, canonical ms form)", appliedAt, want)
	}

	// Roles: the two file roles plus the merged built-in 'member' default —
	// exactly what today's boot loads from this file.
	if n := countRows(t, database, "roles"); n != 3 {
		t.Errorf("roles rows = %d, want 3 (admin, analyst, member)", n)
	}
	var scope string
	var topK int
	if err := database.QueryRow(`SELECT scope, top_k FROM roles WHERE name = 'analyst'`).Scan(&scope, &topK); err != nil {
		t.Fatal(err)
	}
	if scope != `["decrypt_scores"]` || topK != 7 {
		t.Errorf("analyst = (%q, %d), want ([\"decrypt_scores\"], 7)", scope, topK)
	}

	// Tokens: plaintext secret verbatim; legacy 'created' coalesced into
	// issued_at; empty issued_at imported as '' (decision 9); '' expires -> NULL.
	type tokRow struct {
		token, role, issued string
		expires, lastUsed   sql.NullString
	}
	readTok := func(user string) tokRow {
		var r tokRow
		if err := database.QueryRow(
			`SELECT token, role, issued_at, expires, last_used FROM tokens WHERE user = ?`, user).
			Scan(&r.token, &r.role, &r.issued, &r.expires, &r.lastUsed); err != nil {
			t.Fatalf("token row %s: %v", user, err)
		}
		return r
	}
	alice := readTok("alice@corp.com")
	if alice.token != "evt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || alice.issued != "2026-01-02" || alice.expires.Valid {
		t.Errorf("alice token row = %+v", alice)
	}
	bob := readTok("bob@corp.com")
	if bob.issued != "2026-01-03" || !bob.expires.Valid || bob.expires.String != "2026-12-31" {
		t.Errorf("bob token row = %+v (want created coalesced into issued_at)", bob)
	}
	legacy := readTok("legacy@corp.com")
	if legacy.issued != "" || legacy.expires.Valid || legacy.lastUsed.Valid {
		t.Errorf("legacy token row = %+v (want issued_at '' as-is)", legacy)
	}

	// Members: full fidelity; timestamps normalized to the canonical
	// millisecond form (legacy second-precision values must not textually
	// interleave with store-written ms values); carol's inconsistent
	// disabled_from sanitized to NULL with a warning naming the row
	// (decision 7).
	var disabledFrom, sessionExpired sql.NullString
	var status string
	if err := database.QueryRow(
		`SELECT status, disabled_from, session_expired_at FROM members WHERE id = ?`, bobID).
		Scan(&status, &disabledFrom, &sessionExpired); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" || disabledFrom.String != "invited" || sessionExpired.String != "2026-01-05T00:00:00.000Z" {
		t.Errorf("bob member row = (%q, %v, %v), want session_expired_at normalized to canonical ms", status, disabledFrom, sessionExpired)
	}
	var memberCreatedAt string
	if err := database.QueryRow(
		`SELECT created_at FROM members WHERE id = ?`, aliceID).Scan(&memberCreatedAt); err != nil {
		t.Fatal(err)
	}
	if memberCreatedAt != "2026-01-01T00:00:00.000Z" {
		t.Errorf("alice created_at = %q, want the canonical ms rendering of the fixture value", memberCreatedAt)
	}
	if err := database.QueryRow(
		`SELECT status, disabled_from FROM members WHERE id = ?`, carolID).Scan(&status, &disabledFrom); err != nil {
		t.Fatal(err)
	}
	if status != "active" || disabledFrom.Valid {
		t.Errorf("carol member row = (%q, %v), want sanitized NULL disabled_from", status, disabledFrom)
	}
	if !strings.Contains(logs.String(), carolID) {
		t.Errorf("sanitize warning does not name carol's row: %s", logs.String())
	}

	// Groups: ids byte-verbatim (non-UUID ids are legal), '' root sentinel.
	var parentID string
	if err := database.QueryRow(`SELECT parent_id FROM groups WHERE id = 'aaaa'`).Scan(&parentID); err != nil {
		t.Fatalf("group 'aaaa' not imported byte-verbatim: %v", err)
	}
	if parentID != "" {
		t.Errorf("root parent_id = %q, want ''", parentID)
	}
	if err := database.QueryRow(`SELECT parent_id FROM groups WHERE id = 'bbbb'`).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if parentID != "aaaa" {
		t.Errorf("child parent_id = %q, want aaaa", parentID)
	}
	if n := countRows(t, database, "memberships"); n != 1 {
		t.Errorf("memberships rows = %d, want 1", n)
	}

	// Invites: pending keeps the sealed plaintext byte-verbatim; consumed is
	// scrubbed to NULL.
	var tokenValue sql.NullString
	if err := database.QueryRow(`SELECT token_value FROM invites WHERE handle = ?`, pendingHandle).Scan(&tokenValue); err != nil {
		t.Fatal(err)
	}
	if !tokenValue.Valid || tokenValue.String != pendingToken {
		t.Errorf("pending token_value = %v, want %q", tokenValue, pendingToken)
	}
	if err := database.QueryRow(`SELECT token_value FROM invites WHERE handle = ?`, consumedHandle).Scan(&tokenValue); err != nil {
		t.Fatal(err)
	}
	if tokenValue.Valid {
		t.Errorf("consumed token_value = %q, want NULL", tokenValue.String)
	}

	// Journal: one row per source, keyed by absolute path, hashing the
	// original content, stamped by the injected clock.
	if n := countRows(t, database, "import_journal"); n != 6 {
		t.Errorf("import_journal rows = %d, want 6", n)
	}
	wantSum := sha256Hex([]byte(happyMembersYAML))
	var gotSum, importedAt string
	abs, _ := filepath.Abs(src.MembersFile)
	if err := database.QueryRow(
		`SELECT sha256, imported_at FROM import_journal WHERE source = ?`, abs).Scan(&gotSum, &importedAt); err != nil {
		t.Fatalf("journal row for members.yml (abs source key): %v", err)
	}
	if gotSum != wantSum {
		t.Errorf("members.yml sha256 = %q, want %q", gotSum, wantSum)
	}
	if want := storedb.FormatTime(testStamp); importedAt != want {
		t.Errorf("imported_at = %q, want %q (canonical ms form)", importedAt, want)
	}

	// Rename-after-commit: sources parked as .migrated with content and
	// permissions preserved.
	for _, path := range src.list() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("source %s still present after import", path)
		}
		if _, err := os.Stat(path + ".migrated"); err != nil {
			t.Errorf("missing %s.migrated: %v", path, err)
		}
	}
	migrated, err := os.ReadFile(src.InvitesFile + ".migrated")
	if err != nil {
		t.Fatal(err)
	}
	if string(migrated) != happyInvitesYAML {
		t.Error("invites.yml.migrated content differs from the original")
	}
	info, err := os.Stat(src.InvitesFile + ".migrated")
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("invites.yml.migrated mode = %04o, want preserved 0600", perm)
	}
}

func TestImportMissingFilesImportsEmpty(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := Sources{
		RolesFile:       filepath.Join(dir, "roles.yml"),
		TokensFile:      filepath.Join(dir, "tokens.yml"),
		MembersFile:     filepath.Join(dir, "members.yml"),
		InvitesFile:     filepath.Join(dir, "invites.yml"),
		GroupsFile:      filepath.Join(dir, "groups.yml"),
		MembershipsFile: filepath.Join(dir, "memberships.yml"),
	}
	log, _ := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import with no YAML: %v", err)
	}

	// Matching today's missing-file boot behavior: empty stores except the
	// built-in default roles a missing roles.yml seeds.
	for _, table := range []string{"members", "invites", "tokens", "groups", "memberships", "import_journal"} {
		if n := countRows(t, database, table); n != 0 {
			t.Errorf("%s rows = %d, want 0", table, n)
		}
	}
	if n := countRows(t, database, "roles"); n != 2 {
		t.Errorf("roles rows = %d, want 2 (built-in admin/member defaults)", n)
	}
	if n := countRows(t, database, "schema_migrations"); n != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", n)
	}
	// The importer must never materialize YAML seed files (the loader's
	// missing-file persist is cancelled before its debounce fires).
	time.Sleep(150 * time.Millisecond)
	for _, path := range []string{src.RolesFile, src.TokensFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("importer materialized %s", path)
		}
	}
}

func TestImportIdempotent(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	log, _ := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatal(err)
	}
	members1 := countRows(t, database, "members")

	// Second run: version row present, so it is a no-op.
	log2, logs2 := testLogger()
	if err := Import(context.Background(), database, src, testClock, log2); err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if n := countRows(t, database, "members"); n != members1 {
		t.Errorf("members rows changed on second import: %d -> %d", members1, n)
	}
	if n := countRows(t, database, "schema_migrations"); n != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", n)
	}
	if _, err := os.Stat(src.MembersFile + ".migrated.migrated"); err == nil {
		t.Error("second import re-renamed a .migrated file")
	}
	if strings.Contains(logs2.String(), "leftover") {
		t.Errorf("no YAML left over, yet leftover warning logged: %s", logs2.String())
	}

	// An operator restoring a DIVERGENT members.yml must be warned that the
	// database stays authoritative — and the file must not be re-imported.
	writeSource(t, src.MembersFile, happyMembersYAML+`  - id: 44444444-4444-4444-4444-444444444444
    email: dave@corp.com
    status: registered
    created_at: "2026-02-01T00:00:00Z"
`, 0o600)
	// A byte-identical leftover is classified as stale residue instead.
	writeSource(t, src.GroupsFile, happyGroupsYAML, 0o600)

	log3, logs3 := testLogger()
	if err := Import(context.Background(), database, src, testClock, log3); err != nil {
		t.Fatalf("third Import: %v", err)
	}
	if n := countRows(t, database, "members"); n != members1 {
		t.Errorf("restored YAML was re-imported: members %d -> %d", members1, countRows(t, database, "members"))
	}
	out := logs3.String()
	if !strings.Contains(out, "DIFFERS") || !strings.Contains(out, "members.yml") {
		t.Errorf("divergent leftover not flagged: %s", out)
	}
	if !strings.Contains(out, "matches the imported content") || !strings.Contains(out, "groups.yml") {
		t.Errorf("identical leftover not classified as stale residue: %s", out)
	}
}

func TestImportAtomicOnPoisonedLastStore(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	// Poison the last-loaded store (invites): duplicate handle.
	writeSource(t, src.InvitesFile, happyInvitesYAML+`  - handle: `+pendingHandle+`
    member_id: `+carolID+`
    email: carol@corp.com
    token_value: evt_ffffffffffffffffffffffffffffffff
    role: member
    lease_id: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    creation_path: admin.member.invite
    created_at: "2026-01-06T00:00:00Z"
    expires_at: ""
    status: pending
`, 0o600)

	log, _ := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err == nil {
		t.Fatal("Import accepted a poisoned invites.yml")
	}
	assertNothingImported(t, database, src)
}

func TestImportMustRejectCorpus(t *testing.T) {
	poison := map[string]func(src Sources, t *testing.T){
		"duplicate member emails": func(src Sources, t *testing.T) {
			writeSource(t, src.MembersFile, happyMembersYAML+`  - id: 44444444-4444-4444-4444-444444444444
    email: alice@corp.com
    status: registered
    created_at: "2026-02-01T00:00:00Z"
`, 0o600)
		},
		"duplicate invite handles": func(src Sources, t *testing.T) {
			writeSource(t, src.InvitesFile, happyInvitesYAML+`  - handle: `+consumedHandle+`
    member_id: `+carolID+`
    email: carol@corp.com
    token_value: ""
    role: member
    lease_id: ffffffffffffffffffffffffffffffff
    creation_path: admin.member.invite
    created_at: "2026-01-06T00:00:00Z"
    expires_at: ""
    status: consumed
`, 0o600)
		},
		"unknown group parent": func(src Sources, t *testing.T) {
			writeSource(t, src.GroupsFile, `groups:
  - id: orphan
    name: Orphan
    parent_id: ghost
    created_at: "2026-01-01T00:00:00Z"
`, 0o600)
		},
		"group cycle": func(src Sources, t *testing.T) {
			writeSource(t, src.GroupsFile, `groups:
  - id: g1
    name: A
    parent_id: g2
    created_at: "2026-01-01T00:00:00Z"
  - id: g2
    name: B
    parent_id: g1
    created_at: "2026-01-01T00:00:00Z"
`, 0o600)
		},
		"tree depth over 8": func(src Sources, t *testing.T) {
			var b strings.Builder
			b.WriteString("groups:\n")
			for i := 1; i <= 9; i++ {
				b.WriteString("  - id: g" + string(rune('0'+i)) + "\n")
				b.WriteString("    name: L" + string(rune('0'+i)) + "\n")
				if i > 1 {
					b.WriteString("    parent_id: g" + string(rune('0'+i-1)) + "\n")
				}
				b.WriteString("    created_at: \"2026-01-01T00:00:00Z\"\n")
			}
			writeSource(t, src.GroupsFile, b.String(), 0o600)
		},
	}
	for name, poisonFn := range poison {
		t.Run(name, func(t *testing.T) {
			database := openTestDB(t)
			dir := t.TempDir()
			src := seedSources(t, dir)
			poisonFn(src, t)
			log, _ := testLogger()
			if err := Import(context.Background(), database, src, testClock, log); err == nil {
				t.Fatalf("Import accepted %s", name)
			}
			assertNothingImported(t, database, src)
		})
	}
}

func TestImportDedupesLegacyDuplicateTokens(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	// Duplicate user (keep-last wins) plus a duplicate token string across
	// users (later owner wins) — both load silently today; the importer
	// dedupes keep-last with a warning (decision 10).
	writeSource(t, src.TokensFile, `tokens:
  - user: dup@corp.com
    token: evt_11111111111111111111111111111111
    role: member
    issued_at: "2026-01-01"
  - user: dup@corp.com
    token: evt_22222222222222222222222222222222
    role: member
    issued_at: "2026-01-02"
  - user: early@corp.com
    token: evt_33333333333333333333333333333333
    role: member
    issued_at: "2026-01-03"
  - user: late@corp.com
    token: evt_33333333333333333333333333333333
    role: member
    issued_at: "2026-01-04"
`, 0o600)

	log, logs := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !strings.Contains(logs.String(), "duplicate") {
		t.Errorf("duplicate-token warning missing: %s", logs.String())
	}
	var token string
	if err := database.QueryRow(`SELECT token FROM tokens WHERE user = 'dup@corp.com'`).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if token != "evt_22222222222222222222222222222222" {
		t.Errorf("dup user kept %q, want the last occurrence", token)
	}
	var user string
	if err := database.QueryRow(`SELECT user FROM tokens WHERE token = 'evt_33333333333333333333333333333333'`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if user != "late@corp.com" {
		t.Errorf("dup token string kept user %q, want the last occurrence", user)
	}
	if n := countRows(t, database, "tokens"); n != 2 {
		t.Errorf("tokens rows = %d, want 2 (dup keep-last, late; early's shadowed row dropped)", n)
	}
}

// TestImportCarriesReadExclusions pins that excluded_reads rows migrate with
// the memberships they subtract from: a dropped exclusion fails OPEN — the
// inherited read an admin removed would come back after the migration.
func TestImportCarriesReadExclusions(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	writeSource(t, src.MembershipsFile, happyMembershipsYAML+`excluded_reads:
  - user: `+aliceID+`
    group_id: bbbb
    removed_by: local-admin:owner@corp.com
    removed_at: "2026-01-03T00:00:00Z"
`, 0o600)
	log, _ := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import: %v", err)
	}
	var removedBy, removedAt string
	if err := database.QueryRow(
		`SELECT removed_by, removed_at FROM read_exclusions WHERE user = ? AND group_id = 'bbbb'`,
		aliceID).Scan(&removedBy, &removedAt); err != nil {
		t.Fatalf("exclusion row missing after import: %v", err)
	}
	if removedBy != "local-admin:owner@corp.com" || removedAt != "2026-01-03T00:00:00.000Z" {
		t.Errorf("exclusion row = (%q, %q), want removed_by verbatim and removed_at normalized to canonical ms", removedBy, removedAt)
	}
	// The FK cascades with the group: deleting bbbb kills the exclusion row
	// in the same statement, mirroring the store's map purge.
	if _, err := database.Exec(`DELETE FROM groups WHERE id = 'bbbb'`); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM read_exclusions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("read_exclusions rows after group delete = %d, want 0 (ON DELETE CASCADE)", n)
	}
}

// TestImportNormalizesOffsetExpiresAt pins the canonical-form invariant the
// invites store's textual aged-pending sweep depends on: an offset-form
// expires_at from hand-edited YAML (legal for the loader, same instant) is
// stored re-rendered as storedb.TimeFormat — RFC3339 UTC with fixed
// three-digit milliseconds — never byte-verbatim.
func TestImportNormalizesOffsetExpiresAt(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	writeSource(t, src.InvitesFile, `invites:
  - handle: `+pendingHandle+`
    member_id: `+aliceID+`
    email: alice@corp.com
    token_value: `+pendingToken+`
    role: member
    lease_id: `+pendingLease+`
    creation_path: admin.member.invite
    created_at: "2026-01-04T00:00:00Z"
    expires_at: "2027-01-05T09:00:00+09:00"
    status: pending
`, 0o600)
	log, _ := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import: %v", err)
	}
	var stored string
	if err := database.QueryRow(`SELECT expires_at FROM invites WHERE handle = ?`, pendingHandle).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := "2027-01-05T00:00:00.000Z"; stored != want {
		t.Errorf("expires_at = %q, want normalized %q", stored, want)
	}
}

// TestImportKeepsUnparseableLoaderToleratedTimestampVerbatim pins the
// no-new-rejection-class rule for timestamp columns today's loaders accept
// without parsing (here members.created_at): an unparseable non-empty value
// imports byte-verbatim with a warning — the import must not fail on data
// that boots today, and its ordering was already undefined in the YAML era.
func TestImportKeepsUnparseableLoaderToleratedTimestampVerbatim(t *testing.T) {
	database := openTestDB(t)
	dir := t.TempDir()
	src := seedSources(t, dir)
	writeSource(t, src.MembersFile, `members:
  - id: `+aliceID+`
    email: alice@corp.com
    status: active
    created_at: "not-a-timestamp"
`, 0o600)
	log, logs := testLogger()
	if err := Import(context.Background(), database, src, testClock, log); err != nil {
		t.Fatalf("Import rejected an unparseable created_at the members loader tolerates: %v", err)
	}
	var createdAt string
	if err := database.QueryRow(
		`SELECT created_at FROM members WHERE id = ?`, aliceID).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	if createdAt != "not-a-timestamp" {
		t.Errorf("created_at = %q, want the unparseable fixture value verbatim", createdAt)
	}
	if !strings.Contains(logs.String(), "verbatim") || !strings.Contains(logs.String(), "created_at") {
		t.Errorf("verbatim-import warning missing: %s", logs.String())
	}
}
