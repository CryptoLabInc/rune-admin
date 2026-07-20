package tokens

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// Store is the token + role registry: an in-memory read cache (four maps
// behind an RWMutex) over an optional SQLite write-through persistence sink.
// Validate — the per-RPC hot path — is pure map work with zero synchronous
// SQL; every mutator commits its rows to the database before the maps
// change, so the cache can never get ahead of durable state. The one
// exception is the last_used stamp, which is persisted asynchronously
// (see runLastUsedWriter). A store with no sink attached (NewStore alone) is
// a pure in-memory registry — how unit tests use it.
type Store struct {
	mu           sync.RWMutex
	tokens       map[string]*Token // keyed by token string
	tokensByUser map[string]*Token // keyed by username

	// db is the optional write-through persistence sink (the unified store
	// database, attached by LoadFromDB). nil = pure in-memory store.
	db *sql.DB

	// lastUsedCh feeds the async last_used writer; nil until LoadFromDB
	// attaches a sink (a sink-less store never starts the writer).
	lastUsedCh chan lastUsedEvent

	now func() time.Time
}

// NewStore returns an empty in-memory token registry with the real UTC
// clock. Persistence is attached separately (LoadFromDB); without it every
// mutation stays in memory only.
func NewStore() *Store {
	return &Store{
		tokens:       make(map[string]*Token),
		tokensByUser: make(map[string]*Token),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// LoadFromDB attaches database (the unified store database, opened with
// db.OpenStrict and carrying the storedb schema) as the write-through
// persistence sink, loads the in-memory token index from the tokens table,
// and starts the async last_used writer. NULL expires/last_used columns map
// to the store's "" empty-value convention. Rows are trusted as-is: they are
// written by this store's own mutators.
func (s *Store) LoadFromDB(database *sql.DB) error {
	ctx := context.Background()

	byToken := make(map[string]*Token)
	byUser := make(map[string]*Token)
	tokRows, err := database.QueryContext(ctx,
		`SELECT user, token, issued_at, expires, last_used, activated_at FROM tokens`)
	if err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}
	defer func() { _ = tokRows.Close() }()
	for tokRows.Next() {
		var t Token
		var expires, lastUsed, activatedAt sql.NullString
		if err := tokRows.Scan(&t.User, &t.Token, &t.IssuedAt, &expires, &lastUsed, &activatedAt); err != nil {
			return fmt.Errorf("tokens: load from db: %w", err)
		}
		t.Expires = expires.String
		t.LastUsed = lastUsed.String
		t.ActivatedAt = activatedAt.String
		// ONE shared *Token per row, same aliasing as AddToken: mutators
		// update the shared record and both indexes see it.
		cp := t
		byToken[cp.Token] = &cp
		byUser[cp.User] = &cp
	}
	if err := tokRows.Err(); err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = byToken
	s.tokensByUser = byUser
	s.db = database
	if s.lastUsedCh == nil {
		s.lastUsedCh = make(chan lastUsedEvent, lastUsedQueueDepth)
		go s.runLastUsedWriter(s.lastUsedCh)
	}
	return nil
}

// LoadDefaultsWithDemoToken seeds the store with the demo token. Useful for
// dev/CI bootstraps that don't ship persisted state; memory-only (it never
// touches an attached sink).
func (s *Store) LoadDefaultsWithDemoToken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := &Token{
		User:     "demo",
		Token:    DemoToken,
		IssuedAt: s.now().Format(dateFormat),
	}
	s.tokens[tok.Token] = tok
	s.tokensByUser[tok.User] = tok
}

// Validate authenticates a token string and returns its user. It is the
// per-RPC hot path: lookup → expiry → last-used stamp, all against the
// in-memory maps with ZERO synchronous SQL. Authorization is the group RBAC
// judge's job; the token itself carries only identity.
func (s *Store) Validate(tokenStr string) (string, error) {
	s.mu.Lock()
	tok, ok := s.tokens[tokenStr]
	if !ok {
		s.mu.Unlock()
		return "", ErrTokenNotFound{}
	}
	if tok.IsExpiredAt(s.now()) {
		user := tok.User
		s.mu.Unlock()
		return "", ErrTokenExpired{User: user}
	}
	// Stamp last-access (throttled) while we still hold the lock and the token
	// pointer. A burst of data-plane RPCs from one member only rewrites the
	// timestamp once per throttle window. The durable copy is handed to the
	// async writer after unlocking — the hot path never waits on SQL.
	stampAt, stamped := s.stampLastUsedLocked(tok)
	user := tok.User
	secret := tok.Token
	stampVal := tok.LastUsed
	queue := s.lastUsedCh
	s.mu.Unlock()
	if stamped && queue != nil {
		select {
		case queue <- lastUsedEvent{user: user, token: secret, stamp: stampVal, at: stampAt}:
		default:
			// Queue full: drop rather than block the hot path. The in-memory
			// value (which every read serves) is already updated; the next
			// stamped Validate re-enqueues.
		}
	}
	return user, nil
}

// lastUsedThrottle bounds how often a token's in-memory LastUsed timestamp
// is rewritten, and therefore how fresh the console's last-access display
// can be. 10 seconds matches lastUsedPersistInterval — the team explicitly
// lowered last-access freshness from the historical 60s — while still
// keeping the hot data-plane path from thrashing the timestamp on every RPC.
const lastUsedThrottle = 10 * time.Second

// stampLastUsedLocked sets tok.LastUsed to now when at least lastUsedThrottle
// has elapsed since the previous stamp, returning the stamp time and whether
// it changed (so the caller enqueues the async DB write). The stamp is the
// canonical storedb.TimeFormat rendering; the prev-parse stays the plain
// RFC3339 layout, which consumes the fractional seconds. Caller holds s.mu.
func (s *Store) stampLastUsedLocked(tok *Token) (time.Time, bool) {
	now := s.now()
	if tok.LastUsed != "" {
		if prev, err := time.Parse(time.RFC3339, tok.LastUsed); err == nil && now.Sub(prev) < lastUsedThrottle {
			return time.Time{}, false
		}
	}
	tok.LastUsed = storedb.FormatTime(now)
	return now, true
}

// GetUsername returns the user a token string belongs to, or "" when the
// token is unknown — best-effort audit attribution, never an auth decision.
func (s *Store) GetUsername(tokenStr string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if tok, ok := s.tokens[tokenStr]; ok {
		return tok.User
	}
	return ""
}

// MarkActivated stamps the user's token activated_at to now: the agent has
// self-reported reaching terminal active (ReportActivation) — fully configured
// and serving, not merely authenticated. This is the signal that advances a
// member from invite_redeemed to online. Idempotent (a re-report just rewrites
// the stamp) and off any hot path (agents report activation rarely), so the row
// is persisted synchronously before the in-memory copy changes, keeping the
// store's cache-never-leads-DB invariant. An unknown user is a no-op. The write
// is keyed on user AND token so a rotation between the agent's report and this
// write matches zero rows (the fresh, unactivated secret keeps its NULL stamp).
func (s *Store) MarkActivated(user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.tokensByUser[user]
	if !ok {
		return nil
	}
	now := storedb.FormatTime(s.now())
	secret := tok.Token
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE tokens SET activated_at = ? WHERE user = ? AND token = ?`, now, user, secret)
		return err
	}); err != nil {
		return fmt.Errorf("tokens: mark activated for user %q: %w", user, err)
	}
	tok.ActivatedAt = now
	return nil
}

// AddToken mints a token for user with an optional expiry in days (nil = never
// expires). The user must not already hold a token (one token per user). The
// row is committed to the store database before the maps change; the returned
// *Token is a copy.
func (s *Store) AddToken(user string, expiresDays *int) (*Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tokensByUser[user]; ok {
		return nil, fmt.Errorf("token already exists for user '%s'", user)
	}
	tokStr, err := newTokenString()
	if err != nil {
		return nil, err
	}
	today := s.now()
	tok := &Token{
		User:     user,
		Token:    tokStr,
		IssuedAt: today.Format(dateFormat),
		Expires:  expiryDate(today, expiresDays),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO tokens (user, token, issued_at, expires, last_used, activated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			tok.User, tok.Token, tok.IssuedAt, nullIfEmpty(tok.Expires), nullIfEmpty(tok.LastUsed), nullIfEmpty(tok.ActivatedAt))
		return err
	}); err != nil {
		return nil, err
	}
	s.tokens[tok.Token] = tok
	s.tokensByUser[tok.User] = tok
	out := *tok
	return &out, nil
}

// RevokeToken deletes user's token. It returns (true, nil) when a token was
// revoked, (false, nil) when none existed, and (false, err) when the token
// exists but the database delete failed — in that case the credential is
// left fully in place (memory and database still agree) and STAYS LIVE, so
// callers running a security cascade (user delete, disable) must treat the
// error as "revocation did not happen" and abort or surface it, never as
// "there was nothing to revoke".
func (s *Store) RevokeToken(user string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.tokensByUser[user]
	if !ok {
		return false, nil
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE user = ?`, user)
		if err != nil {
			return err
		}
		return expectOneRow(res, user)
	}); err != nil {
		return false, fmt.Errorf("tokens: revoke for user %q: %w", user, err)
	}
	delete(s.tokensByUser, user)
	delete(s.tokens, tok.Token)
	return true, nil
}

// RotateToken replaces user's token secret with a fresh one, re-anchoring the
// expiry span from today (unparseable old dates = the rotated token never
// expires). The row is updated in the store database before the maps change;
// the returned *Token is a copy.
func (s *Store) RotateToken(user string) (*Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.tokensByUser[user]
	if !ok {
		return nil, fmt.Errorf("no token found for user '%s'", user)
	}
	var expiresDays *int
	if old.Expires != "" {
		issued, errIss := time.Parse(dateFormat, old.IssuedAt)
		exp, errExp := time.Parse(dateFormat, old.Expires)
		if errIss == nil && errExp == nil {
			d := int(exp.Sub(issued).Hours() / 24)
			expiresDays = &d
		}
	}
	tokStr, err := newTokenString()
	if err != nil {
		return nil, err
	}
	today := s.now()
	newTok := &Token{
		User:     user,
		Token:    tokStr,
		IssuedAt: today.Format(dateFormat),
		Expires:  expiryDate(today, expiresDays),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE tokens SET token = ?, issued_at = ?, expires = ?, last_used = NULL, activated_at = NULL
			 WHERE user = ?`,
			newTok.Token, newTok.IssuedAt, nullIfEmpty(newTok.Expires), user)
		if err != nil {
			return err
		}
		return expectOneRow(res, user)
	}); err != nil {
		return nil, err
	}
	delete(s.tokens, old.Token)
	s.tokens[newTok.Token] = newTok
	s.tokensByUser[user] = newTok
	out := *newTok
	return &out, nil
}

// RotateAllTokens rotates every user's token (one transaction per user, in
// user order). On a mid-loop error the tokens rotated so far are returned
// with the error — the operation is not atomic across users, matching the
// legacy behavior.
func (s *Store) RotateAllTokens() ([]*Token, error) {
	s.mu.RLock()
	users := make([]string, 0, len(s.tokensByUser))
	for u := range s.tokensByUser {
		users = append(users, u)
	}
	s.mu.RUnlock()
	sort.Strings(users)

	result := make([]*Token, 0, len(users))
	for _, u := range users {
		tok, err := s.RotateToken(u)
		if err != nil {
			return result, err
		}
		result = append(result, tok)
	}
	return result, nil
}

// TokenInfo is a secret-free token listing row.
type TokenInfo struct {
	User    string `json:"user" yaml:"user"`
	Expires string `json:"expires" yaml:"expires"`
	// LastUsed is the token's last-access timestamp (canonical
	// storedb.TimeFormat: RFC3339 UTC with milliseconds), empty when the
	// token has never authenticated a request. Expired reports whether the
	// token is past its expiry as of the read. Both back the console's user
	// liveness fields (lastAccessAt / session-expired status).
	LastUsed string `json:"last_used" yaml:"last_used"`
	// ActivatedAt is set once the agent self-reports terminal activation
	// (ReportActivation), empty until then. It is what advances a member from
	// invite_redeemed to online: a live token that has authenticated but never
	// activated is redeemed, not online.
	ActivatedAt string `json:"activated_at" yaml:"activated_at"`
	Expired     bool   `json:"expired" yaml:"expired"`
}

// ListTokens returns a secret-free, user-sorted listing of every token.
// Expires "" renders "never".
func (s *Store) ListTokens() []TokenInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]string, 0, len(s.tokensByUser))
	for u := range s.tokensByUser {
		users = append(users, u)
	}
	sort.Strings(users)

	out := make([]TokenInfo, 0, len(users))
	for _, u := range users {
		tok := s.tokensByUser[u]
		info := TokenInfo{User: tok.User, Expires: "never", LastUsed: tok.LastUsed, ActivatedAt: tok.ActivatedAt, Expired: tok.IsExpiredAt(s.now())}
		if tok.Expires != "" {
			info.Expires = tok.Expires
		}
		out = append(out, info)
	}
	return out
}

// ── async last_used persistence ─────────────────────────────────────

// lastUsedPersistInterval bounds how often one token's last_used stamp is
// written to the database by the async writer. The user lowered it from 60s
// to 10s: the write is a single-row UPDATE off the hot path, so even the
// tighter interval is negligible I/O, and it keeps the durable copy close to
// the live one across restarts. UI freshness is unaffected either way —
// every read (ListTokens and the console surfaces built on it) serves the
// in-memory value, which updates on the in-memory throttle; the database
// copy exists only so last_used survives a restart.
const lastUsedPersistInterval = 10 * time.Second

// lastUsedQueueDepth sizes the async writer's queue. Stamps arrive at most
// once per token per lastUsedThrottle window, so the queue is effectively
// bounded by the number of concurrently active tokens; if it ever fills,
// Validate drops the stamp instead of blocking.
const lastUsedQueueDepth = 256

// lastUsedEvent is one unit of work for the async last_used writer: a
// stamped last-access to persist (flush nil), or a barrier (flush non-nil,
// closed once every earlier event has been handled — the test seam
// syncLastUsed uses to await durability deterministically).
type lastUsedEvent struct {
	user  string
	token string    // the secret the stamp belongs to: a rotation between enqueue and write must not backdate the new secret's row
	stamp string    // canonical storedb.TimeFormat, the EXACT string already visible in the in-memory map (carried verbatim so memory and row can never differ by a re-render)
	at    time.Time // the stamp's clock reading, carried so the writer never reads a clock
	flush chan struct{}
}

// runLastUsedWriter is the async last_used persister: it drains queue,
// throttling per token by lastUsedPersistInterval and writing each surviving
// stamp as its own single-row UPDATE on a background context — never a
// request context, never under the store lock, so the Validate hot path
// stays free of SQL. A late stamp for a revoked user or a rotated secret
// matches zero rows (the UPDATE is keyed on user AND token) and is silently
// dropped; a write error is logged and the stamp abandoned (last_used is
// best-effort telemetry).
//
// Lifecycle: started by LoadFromDB and deliberately abandoned at process
// exit rather than flushed — there is no shutdown hook, so daemon shutdown
// never blocks on telemetry, and a stamp lost in the final instant merely
// means a restart shows the previous persisted value. An UPDATE racing the
// daemon's deferred database Close at exit at worst logs one error.
func (s *Store) runLastUsedWriter(queue <-chan lastUsedEvent) {
	lastPersisted := make(map[string]time.Time)
	for ev := range queue {
		if ev.flush != nil {
			close(ev.flush)
			continue
		}
		if prev, ok := lastPersisted[ev.user]; ok && ev.at.Sub(prev) < lastUsedPersistInterval {
			continue
		}
		if _, err := s.db.ExecContext(context.Background(),
			`UPDATE tokens SET last_used = ? WHERE user = ? AND token = ?`, ev.stamp, ev.user, ev.token); err != nil {
			fmt.Fprintf(os.Stderr, "tokens: persist last_used for user '%s' failed: %v\n", ev.user, err)
			continue
		}
		lastPersisted[ev.user] = ev.at
	}
}

// syncLastUsed blocks until the async last_used writer has handled every
// event enqueued before the call — the test seam for asserting a stamp's
// durability deterministically. A sink-less store returns immediately.
func (s *Store) syncLastUsed() {
	s.mu.RLock()
	queue := s.lastUsedCh
	s.mu.RUnlock()
	if queue == nil {
		return
	}
	done := make(chan struct{})
	queue <- lastUsedEvent{flush: done}
	<-done
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
		return fmt.Errorf("tokens: persist begin: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("tokens: persist: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tokens: persist commit: %w", err)
	}
	return nil
}

// expectOneRow fails when a per-row UPDATE/DELETE did not touch exactly one
// row — the cache said the row exists, so anything else means cache and
// database have diverged and the mutation must not proceed.
func expectOneRow(res sql.Result, ref string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("row %s: %d rows affected, want 1", ref, n)
	}
	return nil
}

// nullIfEmpty maps the store's "" empty-value convention to SQL NULL for the
// nullable columns (expires, last_used).
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func newTokenString() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(b), nil
}

func expiryDate(today time.Time, days *int) string {
	if days == nil {
		return ""
	}
	return today.AddDate(0, 0, *days).Format(dateFormat)
}
