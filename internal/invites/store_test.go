package invites

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

func issueTest(t *testing.T, s *Store, token string) *ClearBundle {
	t.Helper()
	b, err := s.Issue(IssueParams{
		MemberID:     "member-1",
		Email:        "u@corp.com",
		Role:         "member",
		TokenValue:   token,
		CreationPath: "test.path",
		TTL:          30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// openTestDB opens the strict store database at path with schema v1
// installed. Restart tests reopen the same path with a fresh handle; never
// ":memory:" (the pool hands each connection its own private database).
func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatalf("OpenStrict: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storedb.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

// newTestDB opens a fresh store database under t.TempDir.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTestDB(t, filepath.Join(t.TempDir(), "runeconsole.db"))
}

// newDBStore builds a store attached to database (the daemon construction
// shape: NewStore + LoadFromDB).
func newDBStore(t *testing.T, database *sql.DB) *Store {
	t.Helper()
	s := NewStore()
	if err := s.LoadFromDB(database); err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	return s
}

// inviteRow reads one invite row's status and token_value straight from the
// database, bypassing the in-memory cache.
func inviteRow(t *testing.T, database *sql.DB, handle string) (status string, token sql.NullString) {
	t.Helper()
	if err := database.QueryRow(
		`SELECT status, token_value FROM invites WHERE handle = ?`, handle).Scan(&status, &token); err != nil {
		t.Fatalf("read invite row %s: %v", handle, err)
	}
	return status, token
}

func TestIssueThenLookupExposesNoSecret(t *testing.T) {
	s := NewStore()
	b := issueTest(t, s, "evt_secretABC")
	if b.Handle == "" || b.LeaseID == "" {
		t.Errorf("bundle missing handle/lease: %+v", b)
	}
	// The clear bundle must not carry the token in any field.
	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), "evt_secretABC") {
		t.Errorf("clear bundle leaked the token: %s", raw)
	}

	got, err := s.Lookup(b.Handle, "test.path")
	if err != nil {
		t.Fatalf("Lookup = %v", err)
	}
	if got.Handle != b.Handle {
		t.Errorf("Lookup handle = %q, want %q", got.Handle, b.Handle)
	}
	// A creation-path mismatch is refused.
	if _, err := s.Lookup(b.Handle, "wrong.path"); !errors.As(err, new(ErrCreationPathMismatch)) {
		t.Errorf("path mismatch = %v, want ErrCreationPathMismatch", err)
	}
}

func TestUnwrapReturnsTokenAndClearsIt(t *testing.T) {
	s := NewStore()
	b := issueTest(t, s, "evt_tok10")
	tok, mid, err := s.Unwrap(b.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "evt_tok10" {
		t.Errorf("token = %q, want evt_tok10", tok)
	}
	if mid != "member-1" {
		t.Errorf("memberID = %q, want member-1", mid)
	}
	// The sealed plaintext is gone from the store the instant it is consumed.
	if s.byHandle[b.Handle].TokenValue != "" {
		t.Errorf("TokenValue not cleared: %q", s.byHandle[b.Handle].TokenValue)
	}
	if s.byHandle[b.Handle].Status != StatusConsumed {
		t.Errorf("status = %q, want consumed", s.byHandle[b.Handle].Status)
	}
}

func TestUnwrapTwiceFails(t *testing.T) {
	s := NewStore()
	b := issueTest(t, s, "evt_tok11")
	if _, _, err := s.Unwrap(b.Handle); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Unwrap(b.Handle)
	if !errors.As(err, new(ErrInviteConsumed)) {
		t.Errorf("second unwrap = %v, want ErrInviteConsumed", err)
	}
}

// TestUnwrapCommitsBeforeReturn: the consumed state is committed to the
// store database the instant Unwrap returns — the row is consumed with its
// plaintext nulled, and a restart (fresh store on the same path) refuses a
// second unwrap.
func TestUnwrapCommitsBeforeReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	b := issueTest(t, s, "evt_persist12")
	tok, _, err := s.Unwrap(b.Handle)
	if err != nil || tok != "evt_persist12" {
		t.Fatalf("Unwrap = (%q, %v)", tok, err)
	}

	// The database exactly as it stands after Unwrap returned.
	status, token := inviteRow(t, d1, b.Handle)
	if status != StatusConsumed || token.Valid {
		t.Errorf("row after Unwrap = (%q, valid=%v), want (consumed, NULL token)", status, token.Valid)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: the consumed state must be durable.
	s2 := newDBStore(t, openTestDB(t, path))
	if _, _, err := s2.Unwrap(b.Handle); !errors.As(err, new(ErrInviteConsumed)) {
		t.Errorf("reloaded unwrap = %v, want ErrInviteConsumed (consumed state must be durable)", err)
	}
}

// TestConcurrentUnwrapExactlyOneWinner is the exactly-once proof, now over
// the SQL CAS: 64 concurrent Unwraps on one handle yield exactly one winner
// holding the right token and 63 ErrInviteConsumed refusals, and the
// database row lands consumed with its plaintext nulled (the CAS's
// rows-affected==1 admitted exactly one writer).
func TestConcurrentUnwrapExactlyOneWinner(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	b := issueTest(t, s, "evt_race")

	const n = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, consumed := 0, 0
	var otherErrs []error
	var winnerToken string
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tok, _, err := s.Unwrap(b.Handle)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				winnerToken = tok
			case errors.As(err, new(ErrInviteConsumed)):
				consumed++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1", successes)
	}
	if consumed != n-1 {
		t.Errorf("consumed refusals = %d, want %d", consumed, n-1)
	}
	if len(otherErrs) != 0 {
		t.Errorf("unexpected errors: %v", otherErrs)
	}
	if winnerToken != "evt_race" {
		t.Errorf("winner token = %q, want evt_race", winnerToken)
	}
	status, token := inviteRow(t, database, b.Handle)
	if status != StatusConsumed || token.Valid {
		t.Errorf("row after race = (%q, valid=%v), want (consumed, NULL token)", status, token.Valid)
	}
}

// TestConcurrentUnwrapAllConsumedSurviveReopen: many distinct invites are
// unwrapped concurrently, each committing before return; a restart from the
// same database path must find them all consumed. (The YAML predecessor
// additionally pinned snapshot rename ordering; per-row transactions have no
// ordering to defend, but the durability invariant is still worth the pin.)
func TestConcurrentUnwrapAllConsumedSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)

	const n = 48
	handles := make([]string, n)
	for i := 0; i < n; i++ {
		b := issueTest(t, s, "evt_conc_"+strconv.Itoa(i))
		handles[i] = b.Handle
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var unwrapErrs []error
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			<-start
			if _, _, err := s.Unwrap(h); err != nil {
				mu.Lock()
				unwrapErrs = append(unwrapErrs, err)
				mu.Unlock()
			}
		}(handles[i])
	}
	close(start)
	wg.Wait()
	if len(unwrapErrs) != 0 {
		t.Fatalf("unexpected unwrap errors: %v", unwrapErrs)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reload exactly as the database stands the instant every Unwrap
	// returned, and confirm no invite reverted to pending.
	s2 := newDBStore(t, openTestDB(t, path))
	for i, h := range handles {
		if _, _, err := s2.Unwrap(h); !errors.As(err, new(ErrInviteConsumed)) {
			t.Errorf("handle %d (%s): reloaded unwrap = %v, want ErrInviteConsumed (consumed state must be durable)", i, h, err)
		}
	}
}

func TestExpiredInviteRefusesUnwrapAndLookup(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	s := NewStore()
	s.now = func() time.Time { return base }
	b := issueTest(t, s, "evt_exp14") // ExpiresAt = base + 30m

	// Advance the injected clock past expiry.
	s.now = func() time.Time { return base.Add(31 * time.Minute) }

	if _, _, err := s.Unwrap(b.Handle); !errors.As(err, new(ErrInviteExpired)) {
		t.Errorf("expired unwrap = %v, want ErrInviteExpired", err)
	}
	if _, err := s.Lookup(b.Handle, "test.path"); !errors.As(err, new(ErrInviteExpired)) {
		t.Errorf("expired lookup = %v, want ErrInviteExpired", err)
	}
}

func TestReportCompromiseOnlyOnConsumed(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)

	// Consumed invite: compromise is accepted, in memory and in the row.
	consumed := issueTest(t, s, "evt_c15")
	if _, _, err := s.Unwrap(consumed.Handle); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportCompromise(consumed.LeaseID); err != nil {
		t.Errorf("ReportCompromise on consumed = %v, want nil", err)
	}
	if s.byLease[consumed.LeaseID].Status != StatusCompromised {
		t.Errorf("status = %q, want compromised", s.byLease[consumed.LeaseID].Status)
	}
	if status, _ := inviteRow(t, database, consumed.Handle); status != StatusCompromised {
		t.Errorf("row status = %q, want compromised", status)
	}

	// Pending invite: compromise is refused (DoS-A defense).
	pending := issueTest(t, s, "evt_c15b")
	if err := s.ReportCompromise(pending.LeaseID); !errors.As(err, new(ErrNotConsumed)) {
		t.Errorf("ReportCompromise on pending = %v, want ErrNotConsumed", err)
	}

	// Unknown lease.
	if err := s.ReportCompromise("no-such-lease"); !errors.As(err, new(ErrInviteNotFound)) {
		t.Errorf("ReportCompromise unknown = %v, want ErrInviteNotFound", err)
	}
}

func TestRevokePendingBlocksUnwrap(t *testing.T) {
	s := NewStore()
	b := issueTest(t, s, "evt_rev16")
	if err := s.RevokePending(b.Handle); err != nil {
		t.Fatal(err)
	}
	if s.byHandle[b.Handle].TokenValue != "" {
		t.Errorf("revoked invite still holds token: %q", s.byHandle[b.Handle].TokenValue)
	}
	if s.byHandle[b.Handle].Status != StatusRevoked {
		t.Errorf("status after revoke = %q, want revoked", s.byHandle[b.Handle].Status)
	}
	// Revoked refuses redemption exactly like expired.
	if _, _, err := s.Unwrap(b.Handle); !errors.As(err, new(ErrInviteExpired)) {
		t.Errorf("unwrap after revoke = %v, want ErrInviteExpired", err)
	}
	if _, err := s.Lookup(b.Handle, "test.path"); !errors.As(err, new(ErrInviteExpired)) {
		t.Errorf("lookup after revoke = %v, want ErrInviteExpired", err)
	}
	// A consumed/terminal invite cannot be revoke-pending'd — including an
	// already-revoked one ('revoked' is terminal).
	if err := s.RevokePending(b.Handle); err == nil {
		t.Error("RevokePending on revoked invite should fail")
	}
	other := issueTest(t, s, "evt_rev16b")
	if _, _, err := s.Unwrap(other.Handle); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokePending(other.Handle); err == nil {
		t.Error("RevokePending on consumed invite should fail")
	}
}

// TestRevokePendingWritesRevokedDurably: RevokePending commits 'revoked'
// (the distinct admin-cancel status) with the plaintext nulled before it
// returns, and a restart keeps refusing the invite — the invariant the old
// debounce+shutdown-flush test defended, now a plain commit-before-return.
func TestRevokePendingWritesRevokedDurably(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	b := issueTest(t, s, "evt_revsd21")
	if err := s.RevokePending(b.Handle); err != nil {
		t.Fatal(err)
	}
	status, token := inviteRow(t, d1, b.Handle)
	if status != StatusRevoked || token.Valid {
		t.Errorf("row after revoke = (%q, valid=%v), want (revoked, NULL token)", status, token.Valid)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	if got := s2.byHandle[b.Handle]; got.Status != StatusRevoked || got.TokenValue != "" {
		t.Errorf("reloaded invite = status %q token %q, want revoked/empty", got.Status, got.TokenValue)
	}
	if _, _, err := s2.Unwrap(b.Handle); !errors.As(err, new(ErrInviteExpired)) {
		t.Errorf("reloaded revoked invite unwrap = %v, want ErrInviteExpired (revoke must be durable)", err)
	}
}

// TestRevokePendingOnAgedOutInviteRecordsExpired: revoking an invite whose
// TTL already passed records 'expired', not 'revoked' — time voided it
// first, the sweep in the same transaction says so, and expired->revoked is
// not a legal transition. The revoke still succeeds (the caller wanted the
// invite void, and it is), preserving the cancel endpoint's semantics.
func TestRevokePendingOnAgedOutInviteRecordsExpired(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	database := newTestDB(t)
	s := newDBStore(t, database)
	s.now = func() time.Time { return base }
	b := issueTest(t, s, "evt_lateRevoke") // expires base+30m

	s.now = func() time.Time { return base.Add(31 * time.Minute) }
	if err := s.RevokePending(b.Handle); err != nil {
		t.Fatalf("revoke of aged-out pending invite = %v, want nil", err)
	}
	if got := s.byHandle[b.Handle]; got.Status != StatusExpired || got.TokenValue != "" {
		t.Errorf("aged-out revoke in memory = status %q token %q, want expired/empty", got.Status, got.TokenValue)
	}
	status, token := inviteRow(t, database, b.Handle)
	if status != StatusExpired || token.Valid {
		t.Errorf("aged-out revoke row = (%q, valid=%v), want (expired, NULL token)", status, token.Valid)
	}
}

// TestStatusForwardOnlyTrigger: the invites_status_forward_only trigger is
// the third defense layer — pending may advance to revoked, but a revoked
// row can never be resurrected, not even by raw SQL.
func TestStatusForwardOnlyTrigger(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	b := issueTest(t, s, "evt_trig")
	// pending -> revoked is a legal transition (the store just exercised the
	// UPDATE path; assert the row to be explicit).
	if err := s.RevokePending(b.Handle); err != nil {
		t.Fatalf("pending->revoked rejected: %v", err)
	}
	// revoked -> anything is rejected by the trigger.
	for _, next := range []string{StatusPending, StatusConsumed, StatusExpired, StatusCompromised} {
		_, err := database.Exec(`UPDATE invites SET status = ? WHERE handle = ?`, next, b.Handle)
		if err == nil || !strings.Contains(err.Error(), "invalid invite status transition") {
			t.Errorf("revoked->%s = %v, want trigger abort", next, err)
		}
	}
}

// TestPendingInviteSurvivesReopenWithSealedToken: a live pending invite
// keeps its sealed token across a restart — the one row class whose
// plaintext legitimately persists.
func TestPendingInviteSurvivesReopenWithSealedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	b1 := issueTest(t, s, "evt_keep17a")
	issueTest(t, s, "evt_keep17b")
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	tok, _, err := s2.Unwrap(b1.Handle)
	if err != nil || tok != "evt_keep17a" {
		t.Errorf("reloaded unwrap = (%q, %v), want (evt_keep17a, nil)", tok, err)
	}
}

// TestUnwrapAgeOutScrubsSealedToken guards the lazy-expiry error path: when
// an Unwrap finds a pending invite past its TTL, it flips it to expired AND
// scrubs the sealed evt_ plaintext — in memory and, via the flip's own
// transaction, in the database row.
func TestUnwrapAgeOutScrubsSealedToken(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	database := newTestDB(t)
	s := newDBStore(t, database)
	s.now = func() time.Time { return base }
	b := issueTest(t, s, "evt_ageout20") // ExpiresAt = base + 30m

	s.now = func() time.Time { return base.Add(31 * time.Minute) }
	if _, _, err := s.Unwrap(b.Handle); !errors.As(err, new(ErrInviteExpired)) {
		t.Fatalf("aged unwrap = %v, want ErrInviteExpired", err)
	}
	if got := s.byHandle[b.Handle]; got.Status != StatusExpired || got.TokenValue != "" {
		t.Errorf("after age-out: status=%q tokenValue=%q, want expired/empty", got.Status, got.TokenValue)
	}
	status, token := inviteRow(t, database, b.Handle)
	if status != StatusExpired || token.Valid {
		t.Errorf("row after age-out = (%q, valid=%v), want (expired, NULL token)", status, token.Valid)
	}
}

// TestWriteSweepScrubsAgedPendingToken locks in the decision-8 write-path
// sweep, the successor of the whole-snapshot scrub: an invite that ages out
// but is NEVER unwrapped (the common case — the invitee never clicks) has
// its stored plaintext nulled by the next invite write transaction of ANY
// kind, while a still-live pending invite keeps its token.
func TestWriteSweepScrubsAgedPendingToken(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	database := newTestDB(t)
	s := newDBStore(t, database)
	s.now = func() time.Time { return base }
	a := issueTest(t, s, "evt_neverclicked30") // invite A, expires base+30m

	// A ages out; nobody unwraps it. A later, unrelated Issue runs the sweep
	// as its transaction's first statement — which must scrub A's row.
	s.now = func() time.Time { return base.Add(31 * time.Minute) }
	b, err := s.Issue(IssueParams{
		MemberID: "m2", Email: "b@corp.com", Role: "member",
		TokenValue: "evt_live31", CreationPath: "test.path", TTL: 30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, token := inviteRow(t, database, a.Handle)
	if status != StatusExpired || token.Valid {
		t.Errorf("aged-out row after sweep = (%q, valid=%v), want (expired, NULL token)", status, token.Valid)
	}
	status, token = inviteRow(t, database, b.Handle)
	if status != StatusPending || !token.Valid || token.String != "evt_live31" {
		t.Errorf("live pending row = (%q, %q), want (pending, evt_live31)", status, token.String)
	}
}

// TestBootSweepScrubsAgedPendingToken: the boot half of decision 8 — an
// invite that aged out while the daemon was down is flipped to expired and
// scrubbed by LoadFromDB itself, before the maps are built.
func TestBootSweepScrubsAgedPendingToken(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := NewStore()
	s.now = func() time.Time { return base }
	if err := s.LoadFromDB(d1); err != nil {
		t.Fatal(err)
	}
	b := issueTest(t, s, "evt_downtime40") // expires base+30m
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// "Restart" past the expiry: the boot sweep must scrub the row.
	d2 := openTestDB(t, path)
	s2 := NewStore()
	s2.now = func() time.Time { return base.Add(31 * time.Minute) }
	if err := s2.LoadFromDB(d2); err != nil {
		t.Fatal(err)
	}
	if got := s2.byHandle[b.Handle]; got.Status != StatusExpired || got.TokenValue != "" {
		t.Errorf("boot-swept invite in memory = status %q token %q, want expired/empty", got.Status, got.TokenValue)
	}
	status, token := inviteRow(t, d2, b.Handle)
	if status != StatusExpired || token.Valid {
		t.Errorf("boot-swept row = (%q, valid=%v), want (expired, NULL token)", status, token.Valid)
	}
}

// TestSweepOrdersMillisecondExpiryWithinOneSecond is the fixed-width
// ordering property the millisecond format exists for: three pending
// invites expiring at .100/.200/.900 WITHIN THE SAME SECOND, swept at a now
// of .500, must split exactly at the sweep instant — .100/.200 expired and
// scrubbed, .900 still pending with its sealed token. Under the old
// second-precision rendering all four instants collapse to the same string
// and `expires_at <= now` would have swept the still-live .900 invite too;
// under a MIXED-width column the comparison is not even ordered. The sweep
// compares TEXT, so this only holds while every value is the one canonical
// fixed-width form.
func TestSweepOrdersMillisecondExpiryWithinOneSecond(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	database := newTestDB(t)
	s := newDBStore(t, database)

	issueAt := func(ttl time.Duration, token string) *ClearBundle {
		t.Helper()
		s.now = func() time.Time { return base }
		b, err := s.Issue(IssueParams{
			MemberID: "m1", Email: "u@corp.com", Role: "member",
			TokenValue: token, CreationPath: "test.path", TTL: ttl,
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	early1 := issueAt(100*time.Millisecond, "evt_ms100")
	early2 := issueAt(200*time.Millisecond, "evt_ms200")
	late := issueAt(900*time.Millisecond, "evt_ms900")

	// A write at base+500ms runs the sweep between the three expiries.
	s.now = func() time.Time { return base.Add(500 * time.Millisecond) }
	if _, err := s.Issue(IssueParams{
		MemberID: "m2", Email: "v@corp.com", Role: "member",
		TokenValue: "evt_trigger", CreationPath: "test.path", TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	for _, expired := range []*ClearBundle{early1, early2} {
		status, token := inviteRow(t, database, expired.Handle)
		if status != StatusExpired || token.Valid {
			t.Errorf("invite expiring before the sweep instant = (%q, valid=%v), want (expired, NULL token)", status, token.Valid)
		}
	}
	status, token := inviteRow(t, database, late.Handle)
	if status != StatusPending || !token.Valid || token.String != "evt_ms900" {
		t.Errorf("invite expiring .400s after the sweep instant = (%q, %q), want it untouched (pending, evt_ms900)", status, token.String)
	}
}

// TestConsumedTokenLeavesNoPlaintextInDBFiles is the byte-level scrub proof
// (successor of the YAML byte-grep): after a consume, a WAL checkpoint
// truncation must leave no trace of the token plaintext in the database file
// or its -wal/-shm sidecars — secure_delete(ON) zeroes the freed content.
func TestConsumedTokenLeavesNoPlaintextInDBFiles(t *testing.T) {
	const secret = "evt_scrubme_57f1c2aa90bb4cd8"
	dir := t.TempDir()
	path := filepath.Join(dir, "runeconsole.db")
	database := openTestDB(t, path)
	s := newDBStore(t, database)
	b := issueTest(t, s, secret)
	if _, _, err := s.Unwrap(b.Handle); err != nil {
		t.Fatal(err)
	}
	// Fold the WAL (which carried the Issue INSERT's plaintext frames) back
	// into the main file and truncate it.
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", p, err)
		}
		if strings.Contains(string(data), secret) {
			t.Errorf("%s still contains the consumed token plaintext", p)
		}
	}
}

// TestMutatorSQLFailureLeavesMapsUnchanged pins the write-through
// discipline: when the transaction cannot commit, the mutator returns the
// error and the in-memory cache is untouched — the cache never gets ahead
// of the database, and no token is released on a failed consume.
func TestMutatorSQLFailureLeavesMapsUnchanged(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	pending := issueTest(t, s, "evt_dead1")
	consumed := issueTest(t, s, "evt_dead2")
	if _, _, err := s.Unwrap(consumed.Handle); err != nil {
		t.Fatal(err)
	}
	// Kill the sink: every transaction from here on fails.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Issue(IssueParams{
		MemberID: "m9", Email: "x@corp.com", Role: "member",
		TokenValue: "evt_ghost", CreationPath: "test.path", TTL: time.Hour,
	}); err == nil {
		t.Fatal("Issue with a dead sink succeeded")
	}
	if len(s.byHandle) != 2 {
		t.Errorf("failed Issue leaked into the cache: %d handles, want 2", len(s.byHandle))
	}
	if tok, _, err := s.Unwrap(pending.Handle); err == nil {
		t.Fatalf("Unwrap with a dead sink released token %q", tok)
	}
	if got := s.byHandle[pending.Handle]; got.Status != StatusPending || got.TokenValue != "evt_dead1" {
		t.Errorf("failed Unwrap mutated the cache: status=%q token=%q", got.Status, got.TokenValue)
	}
	if err := s.RevokePending(pending.Handle); err == nil {
		t.Fatal("RevokePending with a dead sink succeeded")
	}
	if got := s.byHandle[pending.Handle]; got.Status != StatusPending {
		t.Errorf("failed RevokePending mutated the cache: status=%q", got.Status)
	}
	if err := s.ReportCompromise(consumed.LeaseID); err == nil {
		t.Fatal("ReportCompromise with a dead sink succeeded")
	}
	if got := s.byLease[consumed.LeaseID]; got.Status != StatusConsumed {
		t.Errorf("failed ReportCompromise mutated the cache: status=%q", got.Status)
	}
}

// TestLoadFromDBRejectsNonCanonicalExpiresAt pins the boot-time fail-closed
// check on a stored expires_at, held to the canonical storedb.TimeFormat
// (RFC3339 UTC, fixed three-digit milliseconds) the textual aged-pending
// sweep depends on: an offset variant parses fine yet would be swept at the
// wrong instant (a negative offset even prematurely, scrubbing a still-valid
// invite); a bare-seconds value — the pre-migration canonical form —
// textually sorts AFTER every same-second ms value ('Z' > '.') and so
// interleaves wrongly with store-written rows; an unparseable value would
// silently mean never-expires. All refuse boot.
func TestLoadFromDBRejectsNonCanonicalExpiresAt(t *testing.T) {
	for name, expiresAt := range map[string]string{
		"offset":       "2027-01-05T09:00:00+09:00",
		"bare-seconds": "2027-01-05T00:00:00Z",
		"unparseable":  "someday",
	} {
		t.Run(name, func(t *testing.T) {
			database := newTestDB(t)
			s := newDBStore(t, database)
			b := issueTest(t, s, "evt_canon")
			if _, err := database.Exec(
				`UPDATE invites SET expires_at = ? WHERE handle = ?`, expiresAt, b.Handle); err != nil {
				t.Fatalf("seed hand-edited row: %v", err)
			}
			if err := NewStore().LoadFromDB(database); err == nil {
				t.Fatalf("LoadFromDB accepted expires_at %q, want refusal", expiresAt)
			}
		})
	}
}

// TestRevokeAllPendingForMemberIsOneTransaction pins the console cancel
// semantics: every pending code of the member is voided in ONE transaction
// (a per-handle loop could half-apply on a crash), aged-out codes recorded
// as expired, other members' invites untouched.
func TestRevokeAllPendingForMemberIsOneTransaction(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	base := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }

	issueFor := func(member, token string, ttl time.Duration) *ClearBundle {
		t.Helper()
		b, err := s.Issue(IssueParams{
			MemberID: member, Email: member + "@corp.com", Role: "member",
			TokenValue: token, CreationPath: "test.path", TTL: ttl,
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	longA := issueFor("member-1", "evt_longa", 24*time.Hour)
	longB := issueFor("member-1", "evt_longb", 24*time.Hour)
	short := issueFor("member-1", "evt_short", 10*time.Minute)
	other := issueFor("member-2", "evt_other", 24*time.Hour)
	s.now = func() time.Time { return base.Add(time.Hour) } // short aged out, longs live

	n, err := s.RevokeAllPendingForMember("member-1")
	if err != nil {
		t.Fatalf("RevokeAllPendingForMember: %v", err)
	}
	if n != 3 {
		t.Errorf("voided = %d, want 3 (two revoked + one aged)", n)
	}
	wantStatus := map[string]string{
		longA.Handle: StatusRevoked,
		longB.Handle: StatusRevoked,
		short.Handle: StatusExpired,
		other.Handle: StatusPending,
	}
	for handle, want := range wantStatus {
		status, token := inviteRow(t, database, handle)
		if status != want {
			t.Errorf("invite %s status = %q, want %q", handle, status, want)
		}
		if want != StatusPending && token.Valid {
			t.Errorf("invite %s kept its sealed token after voiding", handle)
		}
	}
	// Idempotent: nothing pending remains for member-1.
	if n, err := s.RevokeAllPendingForMember("member-1"); err != nil || n != 0 {
		t.Errorf("second call = (%d, %v), want (0, nil)", n, err)
	}
	// The untouched member still cancels independently.
	if n, err := s.RevokeAllPendingForMember("member-2"); err != nil || n != 1 {
		t.Errorf("member-2 cancel = (%d, %v), want (1, nil)", n, err)
	}
}
