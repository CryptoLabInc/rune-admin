package invites

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// Store holds wrapped invites behind two indexes (by handle, by lease): an
// in-memory read cache over an optional SQLite write-through persistence
// sink. Reads are pure map lookups; every mutator commits its row to the
// database (with the aged-pending sweep as the transaction's first
// statement) before the maps change, so the cache can never get ahead of
// durable state. A store with no sink attached (NewStore alone) is a pure
// in-memory store — how unit tests use it.
type Store struct {
	mu       sync.RWMutex
	byHandle map[string]*Invite
	byLease  map[string]*Invite

	// db is the optional write-through persistence sink (the unified store
	// database, attached by LoadFromDB). nil = pure in-memory store.
	db *sql.DB

	now func() time.Time
}

// NewStore returns an empty in-memory invite store with the real UTC clock.
// Persistence is attached separately (LoadFromDB); without it every mutation
// stays in memory only.
func NewStore() *Store {
	return &Store{
		byHandle: make(map[string]*Invite),
		byLease:  make(map[string]*Invite),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// LoadFromDB attaches database (the unified store database, opened with
// db.OpenStrict and carrying the storedb schema) as the write-through
// persistence sink and loads the in-memory indexes from its invites table.
// Before reading it runs the boot-time aged-pending sweep: any pending row
// whose expires_at has passed is flipped to expired and its sealed plaintext
// scrubbed, so a token that aged out while the daemon was down never
// survives a restart. NULL token_value/expires_at columns map to the
// store's "" empty-value convention. Rows are trusted as-is: they are
// written by this store's own mutators, with the schema CHECKs and the
// forward-only status trigger as a second layer.
func (s *Store) LoadFromDB(database *sql.DB) error {
	// Boot-time sweep (its own transaction, injected clock): the write-path
	// sweep only fires when something mutates, so an idle restart is the one
	// window it cannot cover.
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("invites: load from db: begin sweep: %w", err)
	}
	if err := sweepAgedPending(ctx, tx, s.now()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("invites: load from db: sweep aged pending: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("invites: load from db: commit sweep: %w", err)
	}

	rows, err := database.Query(
		`SELECT handle, lease_id, member_id, email, token_value, role, creation_path, created_at, expires_at, status FROM invites`)
	if err != nil {
		return fmt.Errorf("invites: load from db: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byHandle := make(map[string]*Invite)
	byLease := make(map[string]*Invite)
	for rows.Next() {
		var inv Invite
		var tokenValue, expiresAt sql.NullString
		if err := rows.Scan(&inv.Handle, &inv.LeaseID, &inv.MemberID, &inv.Email,
			&tokenValue, &inv.Role, &inv.CreationPath, &inv.CreatedAt, &expiresAt, &inv.Status); err != nil {
			return fmt.Errorf("invites: load from db: %w", err)
		}
		inv.TokenValue = tokenValue.String
		inv.ExpiresAt = expiresAt.String
		// Fail-closed parse check, pinned to the canonical form:
		// expires_at must be storedb.TimeFormat (RFC3339 UTC,
		// fixed three-digit milliseconds) exactly as this store writes it,
		// because sweepAgedPending compares the column TEXTUALLY
		// (lexicographic == chronological only within that one fixed-width
		// form; an offset, bare-seconds, or differently-fractioned variant
		// parses fine yet is swept at the wrong instant) and isExpiredAt
		// treats an unparseable value as never-expires. The schema has no
		// format CHECK, and a hand-edited row (the documented headless
		// sqlite3 path) must not silently mint an eternal or mis-sweepable
		// invite.
		if inv.ExpiresAt != "" {
			canonical, perr := storedb.CanonicalizeTime(inv.ExpiresAt)
			if perr != nil {
				return fmt.Errorf("invites: load from db: invite %s has unparseable expires_at %q: %w", inv.Handle, inv.ExpiresAt, perr)
			}
			if inv.ExpiresAt != canonical {
				return fmt.Errorf("invites: load from db: invite %s has non-canonical expires_at %q (want RFC3339 UTC with three-digit milliseconds, e.g. %q)", inv.Handle, inv.ExpiresAt, canonical)
			}
		}
		// ONE shared *Invite per row, same aliasing as Issue:
		// mutators update the shared record and both indexes see it.
		cp := inv
		byHandle[cp.Handle] = &cp
		byLease[cp.LeaseID] = &cp
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("invites: load from db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byHandle = byHandle
	s.byLease = byLease
	s.db = database
	return nil
}

// IssueParams carries everything needed to wrap one token. TokenValue is the
// already-minted plaintext (the caller mints it via the tokens store); this
// store only seals it, never generates it.
type IssueParams struct {
	MemberID     string
	Email        string
	Role         string
	TokenValue   string
	CreationPath string
	TTL          time.Duration
}

// Issue wraps TokenValue behind a fresh opaque handle and returns the
// secret-free clear bundle. The plaintext is sealed into the store only; it
// is never part of the return value.
//
// Issue is commit-before-return, mirroring Unwrap: the envelope row is
// committed to the store database before the bundle is handed back, because
// "invited" is defined as "the invite envelope is durable" (design-decisions
// §8.3), not merely "in memory" or "email sent". If the commit fails the
// error is returned with no bundle and the maps are untouched, so a failed
// Issue leaves no ghost envelope (the caller then revokes the minted token).
func (s *Store) Issue(p IssueParams) (*ClearBundle, error) {
	if p.MemberID == "" || p.Email == "" || p.Role == "" || p.TokenValue == "" {
		return nil, fmt.Errorf("invite: member_id, email, role and token_value are all required")
	}
	handle, err := randHex()
	if err != nil {
		return nil, err
	}
	lease, err := randHex()
	if err != nil {
		return nil, err
	}
	now := s.now()
	inv := &Invite{
		Handle:       handle,
		MemberID:     p.MemberID,
		Email:        p.Email,
		TokenValue:   p.TokenValue,
		Role:         p.Role,
		LeaseID:      lease,
		CreationPath: p.CreationPath,
		CreatedAt:    storedb.FormatTime(now),
		ExpiresAt:    storedb.FormatTime(now.Add(p.TTL)),
		Status:       StatusPending,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byHandle[handle]; dup {
		return nil, fmt.Errorf("invite: handle collision (retry)")
	}
	if _, dup := s.byLease[lease]; dup {
		return nil, fmt.Errorf("invite: lease collision (retry)")
	}
	if err := s.persist(now, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, creation_path, created_at, expires_at, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			inv.Handle, inv.LeaseID, inv.MemberID, inv.Email, inv.TokenValue,
			inv.Role, inv.CreationPath, inv.CreatedAt, inv.ExpiresAt, inv.Status)
		return err
	}); err != nil {
		return nil, fmt.Errorf("invite: persist envelope before release: %w", err)
	}
	s.byHandle[handle] = inv
	s.byLease[lease] = inv
	return inv.clearBundle(), nil
}

// Lookup returns the secret-free bundle for a pending, unexpired invite whose
// creation path matches. It never consumes and never exposes the token.
func (s *Store) Lookup(handle, creationPath string) (*ClearBundle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.byHandle[handle]
	if !ok {
		return nil, ErrInviteNotFound{Ref: handle}
	}
	if inv.CreationPath != creationPath {
		return nil, ErrCreationPathMismatch{}
	}
	if err := s.statusGateLocked(inv, s.now()); err != nil {
		return nil, err
	}
	return inv.clearBundle(), nil
}

// Unwrap consumes an invite exactly once and returns its token. The Go
// status/expiry gate runs first under the write lock; the consume itself is
// a SQL compare-and-swap in one transaction —
//
//	UPDATE invites SET status='consumed', token_value=NULL
//	 WHERE handle=? AND status='pending'
//
// with exactly one affected row required — and the token is returned only
// after COMMIT (synchronous(FULL) makes the commit durable before return).
// The write lock is kept as defense in depth and the schema's forward-only
// status trigger is the third layer. If the commit fails, the maps are
// untouched (the invite stays pending, retryable) and no token is released.
func (s *Store) Unwrap(handle string) (token, memberID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byHandle[handle]
	if !ok {
		return "", "", ErrInviteNotFound{Ref: handle}
	}
	// One clock reading for the whole mutation: the gate and persist's
	// leading sweep must agree on whether this row just aged out.
	now := s.now()
	if gErr := s.statusGateLocked(inv, now); gErr != nil {
		// Lazy age-out: a pending invite past its TTL is flipped to expired
		// and its plaintext scrubbed. The flip is its own transaction on this
		// error path (the CAS below deliberately checks status only, so the
		// expiry gate — and its write — stay in Go, ahead of it).
		if _, isExpired := gErr.(ErrInviteExpired); isExpired && inv.Status == StatusPending {
			if perr := s.persist(now, func(ctx context.Context, tx *sql.Tx) error {
				// persist's leading sweep already covers this row (it is an
				// aged-out pending row by definition); the targeted UPDATE —
				// an idempotent expired->expired after the sweep — keeps the
				// flip explicit and row-count verified.
				res, uerr := tx.ExecContext(ctx,
					`UPDATE invites SET status = ?, token_value = NULL WHERE handle = ?`,
					StatusExpired, handle)
				if uerr != nil {
					return uerr
				}
				return expectOneRow(res, handle)
			}); perr != nil {
				// The refusal stands regardless; the flip is retried on the
				// next access (or by the sweep in any later write).
				fmt.Fprintf(os.Stderr, "invites: persist age-out flip failed: %v\n", perr)
				return "", "", gErr
			}
			inv.Status = StatusExpired
			inv.TokenValue = ""
			return "", "", gErr
		}
		return "", "", gErr
	}
	// Consume: SQL CAS committed first, then the shared record is updated —
	// all under the write lock, so concurrent Unwraps observe the consumed
	// state and lose.
	tok := inv.TokenValue
	mid := inv.MemberID
	if err := s.persist(now, func(ctx context.Context, tx *sql.Tx) error {
		res, uerr := tx.ExecContext(ctx,
			`UPDATE invites SET status = 'consumed', token_value = NULL
			 WHERE handle = ? AND status = 'pending'`, handle)
		if uerr != nil {
			return uerr
		}
		return expectOneRow(res, handle)
	}); err != nil {
		return "", "", fmt.Errorf("invite: persist consumed state before release: %w", err)
	}
	inv.Status = StatusConsumed
	inv.TokenValue = ""
	return tok, mid, nil
}

// ReportCompromise flags a consumed invite (by lease id) as compromised. A
// pending invite is rejected (ErrNotConsumed) so an attacker cannot force
// re-issue by crying wolf on an unused invite (design-decisions DoS-A).
func (s *Store) ReportCompromise(leaseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byLease[leaseID]
	if !ok {
		return ErrInviteNotFound{Ref: leaseID}
	}
	if inv.Status != StatusConsumed {
		return ErrNotConsumed{LeaseID: leaseID}
	}
	if err := s.persist(s.now(), func(ctx context.Context, tx *sql.Tx) error {
		res, uerr := tx.ExecContext(ctx,
			`UPDATE invites SET status = ? WHERE lease_id = ?`, StatusCompromised, leaseID)
		if uerr != nil {
			return uerr
		}
		return expectOneRow(res, leaseID)
	}); err != nil {
		return err
	}
	inv.Status = StatusCompromised
	return nil
}

// RevokePending administratively voids a still-pending invite (the cancel /
// re-send / rollback path: void the old handle before issuing a fresh one).
// The sealed plaintext is cleared and the invite is marked revoked — a
// distinct terminal status, so listings can tell an admin cancellation from
// a TTL expiry — and a later Unwrap is refused exactly as for an expired
// invite. One nuance: if the invite already aged out (its TTL passed but
// nothing flipped it yet), it is recorded as expired, not revoked — time
// voided it first, and that is what the aged-pending sweep in the same
// transaction writes (expired → revoked is not a legal transition).
func (s *Store) RevokePending(handle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byHandle[handle]
	if !ok {
		return ErrInviteNotFound{Ref: handle}
	}
	if inv.Status != StatusPending {
		return fmt.Errorf("invite %q is not pending (status=%s); cannot revoke", handle, inv.Status)
	}
	// One clock reading decides both the target status and persist's sweep,
	// so an invite expiring between two reads can never make the sweep flip
	// the row first and turn the UPDATE into an illegal expired->revoked.
	now := s.now()
	target := StatusRevoked
	if isExpiredAt(inv, now) {
		target = StatusExpired
	}
	if err := s.persist(now, func(ctx context.Context, tx *sql.Tx) error {
		res, uerr := tx.ExecContext(ctx,
			`UPDATE invites SET status = ?, token_value = NULL WHERE handle = ?`, target, handle)
		if uerr != nil {
			return uerr
		}
		return expectOneRow(res, handle)
	}); err != nil {
		return err
	}
	inv.Status = target
	inv.TokenValue = ""
	return nil
}

// RevokeAllPendingForMember administratively voids EVERY still-pending invite
// of one member in a single transaction — the console's cancel-invitation
// action and the user-delete cascade, where "cancel the invitation" is one
// user action and must not half-apply across N per-handle transactions (a
// crash mid-loop would leave the member still invite_pending through the
// surviving codes). Aged-out pending invites are voided too, recorded as
// expired (time voided them first — same nuance as RevokePending); the rest
// become revoked. Returns how many invites were voided; 0 with a nil error
// means there was nothing pending.
func (s *Store) RevokeAllPendingForMember(memberID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	// Partition this member's pending invites with the SAME clock reading the
	// transaction's sweep uses: aged ones are flipped to expired by the sweep
	// (or, sink-less, by us below), live ones by the revoke UPDATE.
	var live, aged []*Invite
	for _, inv := range s.byHandle {
		if inv.MemberID != memberID || inv.Status != StatusPending {
			continue
		}
		if isExpiredAt(inv, now) {
			aged = append(aged, inv)
		} else {
			live = append(live, inv)
		}
	}
	if len(live) == 0 && len(aged) == 0 {
		return 0, nil
	}
	if err := s.persist(now, func(ctx context.Context, tx *sql.Tx) error {
		// persist's leading sweep already flipped the aged rows; this UPDATE
		// touches exactly the live ones (status still 'pending').
		res, uerr := tx.ExecContext(ctx,
			`UPDATE invites SET status = ?, token_value = NULL
			 WHERE member_id = ? AND status = ?`, StatusRevoked, memberID, StatusPending)
		if uerr != nil {
			return uerr
		}
		return expectRows(res, int64(len(live)), fmt.Sprintf("pending invites of member %s", memberID))
	}); err != nil {
		return 0, err
	}
	for _, inv := range live {
		inv.Status = StatusRevoked
		inv.TokenValue = ""
	}
	for _, inv := range aged {
		inv.Status = StatusExpired
		inv.TokenValue = ""
	}
	return len(live) + len(aged), nil
}

// statusGateLocked returns the terminal-state or expiry error for an invite,
// or nil when the invite is pending and still within its TTL as of now —
// the same clock reading a mutating caller then hands to persist, so gate
// and sweep can never disagree. Caller holds mu.
func (s *Store) statusGateLocked(inv *Invite, now time.Time) error {
	switch inv.Status {
	case StatusConsumed:
		return ErrInviteConsumed{Handle: inv.Handle}
	case StatusCompromised:
		return ErrInviteCompromised{Handle: inv.Handle}
	case StatusExpired, StatusRevoked:
		// Revoked behaves exactly like expired on the redemption surface
		// (ErrInviteExpired documents both); the distinct stored status
		// exists for listings, not for a different refusal.
		return ErrInviteExpired{Handle: inv.Handle}
	}
	if isExpiredAt(inv, now) {
		return ErrInviteExpired{Handle: inv.Handle}
	}
	return nil
}

// isExpiredAt reports whether now >= ExpiresAt. An empty/unparseable
// ExpiresAt is treated as never-expires.
func isExpiredAt(inv *Invite, now time.Time) bool {
	if inv.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	if err != nil {
		return false
	}
	return !now.Before(exp)
}

// ── persistence (write-through to the unified store database) ───────

// persist runs the aged-pending sweep and then fn inside one write
// transaction against the attached sink and commits it. It is called with
// the store write lock held, BEFORE the maps are touched: on any error the
// transaction rolls back and the caller returns without mutating, so the
// in-memory cache never gets ahead of the database. With no sink attached
// (pure in-memory store) it is a no-op.
// The caller passes the ONE clock reading its Go-side expiry gate used, so
// the in-transaction sweep can never disagree with the gate about whether a
// row just aged out (a second clock read crossing ExpiresAt between gate and
// sweep would flip the row first and make the per-row UPDATE an illegal
// transition, aborting the whole mutation).
// The mutator API takes no context, so the transaction runs on a background
// context — a caller hanging up mid-request can never cancel a COMMIT and
// desynchronize cache and database.
func (s *Store) persist(now time.Time, fn func(ctx context.Context, tx *sql.Tx) error) error {
	if s.db == nil {
		return nil
	}
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("invites: persist begin: %w", err)
	}
	if err := sweepAgedPending(ctx, tx, now); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("invites: persist: sweep aged pending: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("invites: persist: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("invites: persist commit: %w", err)
	}
	return nil
}

// sweepAgedPending scrubs every aged-out pending row: status flips to
// expired (a transition the forward-only trigger allows) and the sealed
// plaintext is nulled, so an expired token never lingers in the database.
// It replaces the old writeSnapshot behavior of blanking expired-pending
// tokens on every persist: it runs as the FIRST statement of every invite
// write transaction plus once at boot (LoadFromDB) — the per-handle lazy
// flip on access is kept on top. Comparison is textual on purpose: both
// sides are CANONICAL storedb.TimeFormat (RFC3339 UTC, fixed three-digit
// milliseconds — the fixed width is what makes lexicographic order time
// order), and now comes from the injected clock, never datetime('now'). The
// canonical form is enforced at every entry point: Issue writes it and
// LoadFromDB refuses to boot on a row that deviates from it. The in-memory
// records are NOT updated here (callers hold no reference to every aged
// row); reads stay correct because expiry is derived lazily from ExpiresAt.
func sweepAgedPending(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE invites SET status = 'expired', token_value = NULL
		 WHERE status = 'pending' AND expires_at IS NOT NULL AND expires_at <= ?`,
		storedb.FormatTime(now))
	return err
}

// expectRows fails when a statement did not touch exactly want rows — the
// cache said what exists in the required state, so anything else means cache
// and database have diverged (or a CAS lost) and the mutation must not
// proceed.
func expectRows(res sql.Result, want int64, ref string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != want {
		return fmt.Errorf("invite %s: %d rows affected, want %d", ref, n, want)
	}
	return nil
}

// expectOneRow fails when a per-invite UPDATE did not touch exactly one row
// (see expectRows).
func expectOneRow(res sql.Result, ref string) error {
	return expectRows(res, 1, ref)
}

// Shutdown does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns); kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Shutdown() {}

// Flush does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns); kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Flush() {}

// randHex returns 16 random bytes as a 32-char lowercase hex string.
func randHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
