package members

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// Store is the member registry: an in-memory read cache (two maps behind an
// RWMutex) over an optional SQLite write-through persistence sink. Reads are
// pure map lookups — zero SQL on the dataplane; every mutator commits its
// row to the database before the maps change, so the cache can never get
// ahead of durable state. A store with no sink attached (NewStore alone) is
// a pure in-memory registry — how unit tests and the one-time YAML importer
// use it.
type Store struct {
	mu      sync.RWMutex
	byID    map[string]*Member // keyed by immutable UUID
	byEmail map[string]*Member // keyed by unique email (person key)

	// db is the optional write-through persistence sink (the unified store
	// database, attached by LoadFromDB). nil = pure in-memory store.
	db *sql.DB

	now func() time.Time
}

// NewStore returns an empty in-memory member registry with the real UTC
// clock. Persistence is attached separately (LoadFromDB); without it every
// mutation stays in memory only.
func NewStore() *Store {
	return &Store{
		byID:    make(map[string]*Member),
		byEmail: make(map[string]*Member),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// LoadFromFile reads a member registry from a legacy YAML file. It is the
// one-time importer's input path (internal/storedb/yamlimport) — the daemon
// loads from the store database via LoadFromDB — and its validation set is
// the import contract: a missing file leaves the store empty (a fresh
// console has no members); a duplicate id/email, a malformed UUID, a bad
// status, or a non-email address fails the load, and with it the import.
func (s *Store) LoadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read members file %s: %w", path, err)
	}
	var doc struct {
		Members []Member `yaml:"members"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse members file %s: %w", path, err)
	}
	for i := range doc.Members {
		m := doc.Members[i]
		if m.ID == "" {
			return fmt.Errorf("members file %s: entry %d missing id", path, i)
		}
		if err := ValidateID(m.ID); err != nil {
			return fmt.Errorf("members file %s: entry %d: %w", path, i, err)
		}
		if err := validateMemberEmail(m.Email); err != nil {
			return fmt.Errorf("members file %s: entry %d: %w", path, i, err)
		}
		if !validStatus(m.Status) {
			return fmt.Errorf("members file %s: entry %d has invalid status %q", path, i, m.Status)
		}
		if _, dup := s.byID[m.ID]; dup {
			return fmt.Errorf("members file %s: duplicate member id '%s'", path, m.ID)
		}
		if _, dup := s.byEmail[m.Email]; dup {
			return fmt.Errorf("members file %s: %w", path, ErrDuplicateEmail{Email: m.Email})
		}
		cp := m
		s.byID[m.ID] = &cp
		s.byEmail[m.Email] = &cp
	}
	return nil
}

// LoadFromDB attaches database (the unified store database, opened with
// db.OpenStrict and carrying the storedb schema) as the write-through
// persistence sink and loads the in-memory indexes from its members table.
// NULL disabled_from / session_expired_at columns map to the stores' ""
// empty-value convention. Rows are trusted as-is: they were either written
// by this store's own mutators or funnelled through LoadFromFile's
// validation by the importer, with the schema CHECKs as a second layer.
func (s *Store) LoadFromDB(database *sql.DB) error {
	rows, err := database.Query(
		`SELECT id, email, display_name, status, disabled_from, created_at, session_expired_at FROM members`)
	if err != nil {
		return fmt.Errorf("members: load from db: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byID := make(map[string]*Member)
	byEmail := make(map[string]*Member)
	for rows.Next() {
		var m Member
		var disabledFrom, sessionExpiredAt sql.NullString
		if err := rows.Scan(&m.ID, &m.Email, &m.DisplayName, &m.Status,
			&disabledFrom, &m.CreatedAt, &sessionExpiredAt); err != nil {
			return fmt.Errorf("members: load from db: %w", err)
		}
		m.DisabledFrom = disabledFrom.String
		m.SessionExpiredAt = sessionExpiredAt.String
		// ONE shared *Member per row, same aliasing as LoadFromFile and Add:
		// mutators update the shared record and both indexes see it.
		cp := m
		byID[cp.ID] = &cp
		byEmail[cp.Email] = &cp
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("members: load from db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = byID
	s.byEmail = byEmail
	s.db = database
	return nil
}

// Add registers a new member with status "registered" — the lifecycle entry
// state, before any invite envelope is issued. The email must be unique and
// pass the person-key check; the id is a fresh immutable UUIDv4. Advancing to
// "invited" is a separate step (MarkInvited) driven by invite issuance.
func (s *Store) Add(email, displayName string) (*Member, error) {
	if err := validateMemberEmail(email); err != nil {
		return nil, ErrInvalidEmail{Email: email}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byEmail[email]; dup {
		return nil, ErrDuplicateEmail{Email: email}
	}
	id, err := newMemberID()
	if err != nil {
		return nil, err
	}
	m := &Member{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Status:      StatusRegistered,
		CreatedAt:   storedb.FormatTime(s.now()),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO members (id, email, display_name, status, disabled_from, created_at, session_expired_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Email, m.DisplayName, m.Status, nullIfEmpty(m.DisabledFrom),
			m.CreatedAt, nullIfEmpty(m.SessionExpiredAt))
		return err
	}); err != nil {
		return nil, err
	}
	s.byID[id] = m
	s.byEmail[email] = m
	out := *m
	return &out, nil
}

// Get returns a copy of the member with the given id.
func (s *Store) Get(id string) (*Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byID[id]
	if !ok {
		return nil, ErrMemberNotFound{Ref: id}
	}
	out := *m
	return &out, nil
}

// StatusByEmail returns the status of the member whose email (the unique
// person key) matches, with ok=false when no registry row exists for it —
// e.g. the owner or a CLI-issued service token whose user is not a member.
// It is the narrow lookup the dataplane member-status gate uses on every
// token-validated request, hence a bare RLock read and no copy-out.
func (s *Store) StatusByEmail(email string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byEmail[email]
	if !ok {
		return "", false
	}
	return m.Status, true
}

// LookupByEmail returns the immutable member id and status for the member
// whose email (the unique person key) matches, with ok=false when no registry
// row exists. It is the ONE registry hit the dataplane makes per
// token-validated request: the member-status gate consumes status and the
// groups-judge key resolution consumes id (token email → member UUID), so a
// bare RLock read and no copy-out, like StatusByEmail.
func (s *Store) LookupByEmail(email string) (id, status string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byEmail[email]
	if !ok {
		return "", "", false
	}
	return m.ID, m.Status, true
}

// GetByEmail returns a copy of the member with the given email.
func (s *Store) GetByEmail(email string) (*Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byEmail[email]
	if !ok {
		return nil, ErrMemberNotFound{Ref: email}
	}
	out := *m
	return &out, nil
}

// List returns all members sorted by CreatedAt then ID (copy-out).
func (s *Store) List() []Member {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Member, 0, len(s.byID))
	for _, m := range s.byID {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Update applies a partial (PATCH) change to a member's mutable attributes. A
// nil pointer leaves that field untouched. Email is NOT among the updatable
// fields — it is fixed at Add() time, because tokens are keyed by it and it is
// the join key to the member id (the group-membership key), so there is
// deliberately no way to express an email change here.
// All preconditions (status enum + transition) are checked before any mutation,
// so a rejected field leaves the record fully unchanged.
func (s *Store) Update(id string, displayName, status *string) (*Member, error) {
	if status != nil && !validStatus(*status) {
		return nil, ErrInvalidStatus{Status: *status}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return nil, ErrMemberNotFound{Ref: id}
	}
	// Pre-validate the status transition before mutating.
	if status != nil && !m.allowedStatusTransition(*status) {
		return nil, ErrInvalidStatus{Status: *status}
	}
	// All checks passed — compute the post-mutation row, commit it, then
	// apply it to the cache.
	next := *m
	if displayName != nil {
		next.DisplayName = *displayName
	}
	if status != nil && *status != m.Status {
		// Restore-to-prior-status bookkeeping: entering disabled records the
		// prior status; leaving it (only reachable toward that recorded
		// status) clears the marker. A disabled→disabled no-op is excluded
		// above so it cannot overwrite the marker with "disabled".
		if *status == StatusDisabled {
			next.DisabledFrom = m.Status
		} else {
			next.DisabledFrom = ""
		}
		next.Status = *status
	}
	if err := s.commitUpdate(m, next); err != nil {
		return nil, err
	}
	out := *m
	return &out, nil
}

// Activate transitions an invited member to active — the invite-accept hook
// (a successful invites.Unwrap flags the member here). It is idempotent for
// an already-active member; a disabled member cannot be activated this way.
func (s *Store) Activate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return ErrMemberNotFound{Ref: id}
	}
	if m.Status == StatusActive {
		return nil
	}
	if m.Status != StatusInvited {
		return ErrInvalidStatus{Status: StatusActive}
	}
	next := *m
	next.Status = StatusActive
	return s.commitUpdate(m, next)
}

// SetSessionExpired records that the member's console session token was
// explicitly destroyed now (console session deactivation). It stamps
// SessionExpiredAt so the console can show the moment the session ended; the
// member's Status is not changed here (status is derived from token presence).
func (s *Store) SetSessionExpired(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return ErrMemberNotFound{Ref: id}
	}
	next := *m
	next.SessionExpiredAt = storedb.FormatTime(s.now())
	return s.commitUpdate(m, next)
}

// Reinvite moves a member to "invited" when a fresh invite code is re-issued to
// someone who cannot currently connect — specifically a session-expired member
// (Status active but its token was destroyed). Unlike MarkInvited (which only
// advances registered->invited), this also allows the backward active->invited
// hop so the console shows "invite_pending" (a new code awaiting acceptance)
// instead of "online". It clears SessionExpiredAt since the member is no longer
// in the expired-session state. Idempotent when already invited; a disabled
// member must be restored first.
func (s *Store) Reinvite(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return ErrMemberNotFound{Ref: id}
	}
	if m.Status == StatusDisabled {
		return ErrInvalidStatus{Status: StatusInvited}
	}
	next := *m
	next.Status = StatusInvited
	next.SessionExpiredAt = ""
	return s.commitUpdate(m, next)
}

// MarkInvited advances a registered member to invited — the invite-issue hook
// (the caller flips the member here once its invite envelope is durably on
// disk, so the member state only ever reflects a real, persisted invite). It
// is idempotent for an already-invited member; an active or disabled member
// cannot be (re-)invited this way and is rejected.
func (s *Store) MarkInvited(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return ErrMemberNotFound{Ref: id}
	}
	if m.Status == StatusInvited {
		return nil
	}
	if m.Status != StatusRegistered {
		return ErrInvalidStatus{Status: StatusInvited}
	}
	next := *m
	next.Status = StatusInvited
	return s.commitUpdate(m, next)
}

// Remove hard-deletes a member from both indexes. It exists for the atomic
// register+grant rollback: if the group grant fails, the just-created member
// must leave no trace (a soft-delete would strand a "disabled" ghost and keep
// the email reserved). A missing id is ErrMemberNotFound.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return ErrMemberNotFound{Ref: id}
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM members WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return expectOneRow(res, id)
	}); err != nil {
		return err
	}
	delete(s.byID, id)
	delete(s.byEmail, m.Email)
	return nil
}

// validateMemberEmail enforces the person-key contract, replicating the rule
// internal/groups.validateUserEmail applies to its membership key: non-empty,
// a single interior '@', no whitespace. Full RFC validation is not the
// console's job.
func validateMemberEmail(email string) error {
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("email must not be empty")
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at != strings.LastIndexByte(email, '@') || at == len(email)-1 {
		return fmt.Errorf("email %q must be an address with a single interior '@'", email)
	}
	if strings.ContainsAny(email, " \t\n") {
		return fmt.Errorf("email %q must not contain whitespace", email)
	}
	return nil
}

// ── persistence (write-through to the unified store database) ───────

// persist runs fn inside one write transaction against the attached sink and
// commits it. It is called with the store write lock held, BEFORE the maps
// are touched: on any error the transaction rolls back and the caller
// returns without mutating, so the in-memory cache never gets ahead of the
// database. With no sink attached (pure in-memory store) it is a no-op.
// The mutator API takes no context, so the transaction runs on a background
// context — a caller hanging up mid-request can never cancel a COMMIT and
// desynchronize cache and database.
func (s *Store) persist(fn func(ctx context.Context, tx *sql.Tx) error) error {
	if s.db == nil {
		return nil
	}
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("members: persist begin: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("members: persist: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("members: persist commit: %w", err)
	}
	return nil
}

// commitUpdate persists next — the fully computed post-mutation row for the
// shared in-memory record m — and applies it to m only after a successful
// COMMIT. Must be called with the store write lock held. The UPDATE's SET
// list deliberately omits the email column: the store API cannot express an
// email change (Update has no email parameter), and leaving the column
// untouched keeps the members_email_immutable trigger armed as the DB-layer
// second line of defense.
func (s *Store) commitUpdate(m *Member, next Member) error {
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE members SET display_name = ?, status = ?, disabled_from = ?, session_expired_at = ?
			 WHERE id = ?`,
			next.DisplayName, next.Status, nullIfEmpty(next.DisabledFrom),
			nullIfEmpty(next.SessionExpiredAt), next.ID)
		if err != nil {
			return err
		}
		return expectOneRow(res, next.ID)
	}); err != nil {
		return err
	}
	*m = next
	return nil
}

// expectOneRow fails when a per-member UPDATE/DELETE did not touch exactly
// one row — the cache said the member exists, so anything else means cache
// and database have diverged and the mutation must not proceed.
func expectOneRow(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("member %s: %d rows affected, want 1", id, n)
	}
	return nil
}

// nullIfEmpty maps the store's "" empty-value convention to SQL NULL for the
// nullable columns (disabled_from, session_expired_at).
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
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

// newMemberID returns a canonical UUIDv4 string from crypto/rand. Hand-rolled
// to keep the console on stdlib-only direct dependencies (same as groups).
func newMemberID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ValidateID enforces the member-id format contract: a canonical UUIDv4-style
// 8-4-4-4-12 lowercase-hex string (the shape newMemberID emits). It is a pure
// format check with no store state, so member deployments can inject it as
// the groups person-key validator at boot, before any store loads.
func ValidateID(id string) error {
	if !isCanonicalUUID(id) {
		return fmt.Errorf("member id %q is not a canonical UUID", id)
	}
	return nil
}

// isCanonicalUUID reports whether s is a canonical 8-4-4-4-12 lowercase-hex
// UUID string (the shape newMemberID emits).
func isCanonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	return true
}
