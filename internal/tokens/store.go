package tokens

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// Store is the token + role registry: an in-memory read cache (four maps
// behind an RWMutex) over an optional SQLite write-through persistence sink.
// Validate — the per-RPC hot path — is pure map work with zero synchronous
// SQL; every mutator commits its rows to the database before the maps
// change, so the cache can never get ahead of durable state. The one
// exception is the last_used stamp, which is persisted asynchronously
// (see runLastUsedWriter). Rate limiters are deliberately non-persistent.
// A store with no sink attached (NewStore alone) is a pure in-memory
// registry — how unit tests and the one-time YAML importer use it.
type Store struct {
	mu           sync.RWMutex
	tokens       map[string]*Token // keyed by token string
	tokensByUser map[string]*Token // keyed by username
	roles        map[string]*Role
	rateLimiters map[string]*RateLimiter // keyed by username; in-memory only

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
		roles:        make(map[string]*Role),
		rateLimiters: make(map[string]*RateLimiter),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// LoadFromFiles reads roles and tokens from the legacy YAML pair. It is the
// one-time importer's input path (internal/storedb/yamlimport) — the daemon
// loads from the store database via LoadFromDB — and it preserves the legacy
// load semantics as the import contract: a missing file loads defaults
// (built-in roles, no tokens), the default roles are merged in when absent,
// read-time defaults fill top_k 0 and empty rate_limit, the legacy `created`
// key coalesces into issued_at, and a parse error fails the load (and with
// it the import). It never writes anything.
func (s *Store) LoadFromFiles(rolesPath, tokensPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Roles
	if data, err := os.ReadFile(rolesPath); err == nil {
		var doc struct {
			Roles map[string]struct {
				Scope     []string `yaml:"scope"`
				TopK      int      `yaml:"top_k"`
				RateLimit string   `yaml:"rate_limit"`
			} `yaml:"roles"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse roles file %s: %w", rolesPath, err)
		}
		for name, cfg := range doc.Roles {
			topK, rl := materializeRoleDefaults(cfg.TopK, cfg.RateLimit)
			s.roles[name] = &Role{Name: name, Scope: cfg.Scope, TopK: topK, RateLimit: rl}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read roles file %s: %w", rolesPath, err)
	}
	for name, role := range DefaultRoles() {
		if _, ok := s.roles[name]; !ok {
			s.roles[name] = role
		}
	}

	// Tokens
	if data, err := os.ReadFile(tokensPath); err == nil {
		var doc struct {
			Tokens []struct {
				User     string `yaml:"user"`
				Token    string `yaml:"token"`
				Role     string `yaml:"role"`
				IssuedAt string `yaml:"issued_at"`
				Created  string `yaml:"created"`
				Expires  string `yaml:"expires"`
			} `yaml:"tokens"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse tokens file %s: %w", tokensPath, err)
		}
		for _, e := range doc.Tokens {
			issued := e.IssuedAt
			if issued == "" {
				issued = e.Created
			}
			tok := &Token{
				User:     e.User,
				Token:    e.Token,
				Role:     e.Role,
				IssuedAt: issued,
				Expires:  e.Expires,
			}
			s.tokens[tok.Token] = tok
			s.tokensByUser[tok.User] = tok
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read tokens file %s: %w", tokensPath, err)
	}
	return nil
}

// LoadFromDB attaches database (the unified store database, opened with
// db.OpenStrict and carrying the storedb schema) as the write-through
// persistence sink, seeds the built-in admin/member roles as rows when
// absent (replacing the legacy unconditional load-time merge and
// install.sh's roles.yml seeding — an operator's stored override of a
// default role is respected), loads the in-memory indexes from the roles
// and tokens tables, and starts the async last_used writer. NULL
// expires/last_used columns map to the store's "" empty-value convention.
// Rows are trusted as-is: they were either written by this store's own
// mutators or funnelled through LoadFromFiles' semantics by the importer.
func (s *Store) LoadFromDB(database *sql.DB) error {
	ctx := context.Background()

	// Seed rows inserted when absent, in one transaction: default-role
	// presence becomes a boot invariant of the database itself.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tokens: load from db: begin seed: %w", err)
	}
	defaults := DefaultRoles()
	names := make([]string, 0, len(defaults))
	for name := range defaults {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := defaults[name]
		scopeJSON, jerr := encodeScope(r.Scope)
		if jerr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("tokens: load from db: seed role %q: %w", name, jerr)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO roles (name, scope, top_k, rate_limit) VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO NOTHING`,
			r.Name, scopeJSON, r.TopK, r.RateLimit); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("tokens: load from db: seed role %q: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tokens: load from db: commit seed: %w", err)
	}

	roles := make(map[string]*Role)
	rows, err := database.QueryContext(ctx, `SELECT name, scope, top_k, rate_limit FROM roles`)
	if err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r Role
		var scopeJSON string
		if err := rows.Scan(&r.Name, &scopeJSON, &r.TopK, &r.RateLimit); err != nil {
			return fmt.Errorf("tokens: load from db: %w", err)
		}
		if err := json.Unmarshal([]byte(scopeJSON), &r.Scope); err != nil {
			return fmt.Errorf("tokens: load from db: decode scope of role %q: %w", r.Name, err)
		}
		cp := r
		roles[cp.Name] = &cp
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}

	byToken := make(map[string]*Token)
	byUser := make(map[string]*Token)
	tokRows, err := database.QueryContext(ctx,
		`SELECT user, token, role, issued_at, expires, last_used FROM tokens`)
	if err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}
	defer func() { _ = tokRows.Close() }()
	for tokRows.Next() {
		var t Token
		var expires, lastUsed sql.NullString
		if err := tokRows.Scan(&t.User, &t.Token, &t.Role, &t.IssuedAt, &expires, &lastUsed); err != nil {
			return fmt.Errorf("tokens: load from db: %w", err)
		}
		t.Expires = expires.String
		t.LastUsed = lastUsed.String
		// ONE shared *Token per row, same aliasing as LoadFromFiles and
		// AddToken: mutators update the shared record and both indexes see it.
		cp := t
		byToken[cp.Token] = &cp
		byUser[cp.User] = &cp
	}
	if err := tokRows.Err(); err != nil {
		return fmt.Errorf("tokens: load from db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles = roles
	s.tokens = byToken
	s.tokensByUser = byUser
	s.db = database
	if s.lastUsedCh == nil {
		s.lastUsedCh = make(chan lastUsedEvent, lastUsedQueueDepth)
		go s.runLastUsedWriter(s.lastUsedCh)
	}
	return nil
}

// LoadDefaultsWithDemoToken seeds the store with default roles and the demo
// token. Useful for dev/CI bootstraps that don't ship persisted state;
// memory-only (it never touches an attached sink).
func (s *Store) LoadDefaultsWithDemoToken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, role := range DefaultRoles() {
		s.roles[name] = role
	}
	tok := &Token{
		User:     "demo",
		Token:    DemoToken,
		Role:     "admin",
		IssuedAt: s.now().Format(dateFormat),
	}
	s.tokens[tok.Token] = tok
	s.tokensByUser[tok.User] = tok
}

// Validate authenticates a token string and returns its user and role. It is
// the per-RPC hot path: lookup → expiry → role (fail-closed: a token whose
// role is missing is refused as ErrTokenNotFound, indistinguishable from "no
// such token") → last-used stamp → rate limit, all against the in-memory
// maps with ZERO synchronous SQL. The returned *Role is a value copy — the
// caller reads it after the lock is released, so it must never alias store
// state that UpdateRole mutates in place.
func (s *Store) Validate(tokenStr string) (string, *Role, error) {
	s.mu.Lock()
	tok, ok := s.tokens[tokenStr]
	if !ok {
		s.mu.Unlock()
		return "", nil, ErrTokenNotFound{}
	}
	if tok.IsExpiredAt(s.now()) {
		user := tok.User
		s.mu.Unlock()
		return "", nil, ErrTokenExpired{User: user}
	}
	role, ok := s.roles[tok.Role]
	if !ok {
		s.mu.Unlock()
		return "", nil, ErrTokenNotFound{}
	}
	limiter, err := s.getOrCreateLimiterLocked(tok.User, role)
	// Stamp last-access (throttled) while we still hold the lock and the token
	// pointer. A burst of data-plane RPCs from one member only rewrites the
	// timestamp once per throttle window. The durable copy is handed to the
	// async writer after unlocking — the hot path never waits on SQL.
	stampAt, stamped := s.stampLastUsedLocked(tok)
	user := tok.User
	secret := tok.Token
	stampVal := tok.LastUsed
	queue := s.lastUsedCh
	roleCopy := *role
	roleCopy.Scope = append([]string(nil), role.Scope...)
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
	if err != nil {
		return "", nil, err
	}
	if !limiter.IsAllowed(user) {
		return "", nil, ErrRateLimit{RetryAfter: limiter.RetryAfter(user)}
	}
	return user, &roleCopy, nil
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

func (s *Store) getOrCreateLimiterLocked(user string, role *Role) (*RateLimiter, error) {
	if l, ok := s.rateLimiters[user]; ok {
		return l, nil
	}
	maxReq, window, err := role.RateLimitParsed()
	if err != nil {
		return nil, err
	}
	l := NewRateLimiter(maxReq, window)
	s.rateLimiters[user] = l
	return l, nil
}

// AddToken mints a token for user with the given role and optional expiry in
// days (nil = never expires). The role must exist and the user must not
// already hold a token (one token per user). The row is committed to the
// store database before the maps change; the returned *Token is a copy.
func (s *Store) AddToken(user, roleName string, expiresDays *int) (*Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roles[roleName]; !ok {
		return nil, fmt.Errorf("role '%s' does not exist", roleName)
	}
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
		Role:     roleName,
		IssuedAt: today.Format(dateFormat),
		Expires:  expiryDate(today, expiresDays),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO tokens (user, token, role, issued_at, expires, last_used)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			tok.User, tok.Token, tok.Role, tok.IssuedAt, nullIfEmpty(tok.Expires), nullIfEmpty(tok.LastUsed))
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
	if l, ok := s.rateLimiters[user]; ok {
		delete(s.rateLimiters, user)
		l.Remove(user)
	}
	return true, nil
}

// RotateToken replaces user's token secret with a fresh one, preserving the
// role name as-is (deliberately without checking the role still exists —
// matching the legacy behavior; the rotated token of a dangling role keeps
// failing Validate closed) and re-anchoring the expiry span from today
// (unparseable old dates = the rotated token never expires). The row is
// updated in the store database before the maps change; the returned *Token
// is a copy.
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
		Role:     old.Role,
		IssuedAt: today.Format(dateFormat),
		Expires:  expiryDate(today, expiresDays),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE tokens SET token = ?, role = ?, issued_at = ?, expires = ?, last_used = NULL
			 WHERE user = ?`,
			newTok.Token, newTok.Role, newTok.IssuedAt, nullIfEmpty(newTok.Expires), user)
		if err != nil {
			return err
		}
		return expectOneRow(res, user)
	}); err != nil {
		return nil, err
	}
	delete(s.tokens, old.Token)
	if l, ok := s.rateLimiters[user]; ok {
		delete(s.rateLimiters, user)
		l.Remove(user)
	}
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

// TokenInfo is a secret-free token listing row. TopK/RateLimit are typed any
// because a token whose role was deleted renders both as the string "?" — the
// row stays visible (the role name is on the token itself) while Validate
// refuses it fail-closed.
type TokenInfo struct {
	User      string `json:"user" yaml:"user"`
	Role      string `json:"role" yaml:"role"`
	TopK      any    `json:"top_k" yaml:"top_k"`
	RateLimit any    `json:"rate_limit" yaml:"rate_limit"`
	Expires   string `json:"expires" yaml:"expires"`
	// LastUsed is the token's last-access timestamp (canonical
	// storedb.TimeFormat: RFC3339 UTC with milliseconds), empty when the
	// token has never authenticated a request. Expired reports whether the
	// token is past its expiry as of the read. Both back the console's user
	// liveness fields (lastAccessAt / session-expired status).
	LastUsed string `json:"last_used" yaml:"last_used"`
	Expired  bool   `json:"expired" yaml:"expired"`
}

// ListTokens returns a secret-free, user-sorted listing of every token.
// Expires "" renders "never"; a dangling role renders TopK/RateLimit as "?".
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
		info := TokenInfo{User: tok.User, Role: tok.Role, Expires: "never", LastUsed: tok.LastUsed, Expired: tok.IsExpiredAt(s.now())}
		if tok.Expires != "" {
			info.Expires = tok.Expires
		}
		if role, ok := s.roles[tok.Role]; ok {
			info.TopK = role.TopK
			info.RateLimit = role.RateLimit
		} else {
			info.TopK = "?"
			info.RateLimit = "?"
		}
		out = append(out, info)
	}
	return out
}

// RoleInfo is a role listing row.
type RoleInfo struct {
	Name      string   `json:"name" yaml:"name"`
	Scope     []string `json:"scope" yaml:"scope"`
	TopK      int      `json:"top_k" yaml:"top_k"`
	RateLimit string   `json:"rate_limit" yaml:"rate_limit"`
}

// materializeRoleDefaults fills the role column defaults at write time:
// top_k 0 becomes 5 and an empty rate_limit becomes "30/60s". The legacy
// store applied these only when (re)loading the YAML file, so a role written
// with top_k 0 silently became 5 after a restart; materializing at write
// removes that restart flip — what is stored is what every future boot reads.
func materializeRoleDefaults(topK int, rateLimit string) (int, string) {
	if topK == 0 {
		topK = 5
	}
	if rateLimit == "" {
		rateLimit = "30/60s"
	}
	return topK, rateLimit
}

// AddRole creates a role. Write-time defaults materialize first (top_k 0→5,
// rate_limit ""→"30/60s" — see materializeRoleDefaults), then the rate limit
// format is validated and the name checked for uniqueness. The row is
// committed to the store database before the maps change; the returned *Role
// is a copy.
func (s *Store) AddRole(name string, scope []string, topK int, rateLimit string) (*Role, error) {
	topK, rateLimit = materializeRoleDefaults(topK, rateLimit)
	if err := validateRateLimit(rateLimit); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roles[name]; ok {
		return nil, fmt.Errorf("role '%s' already exists", name)
	}
	role := &Role{Name: name, Scope: scope, TopK: topK, RateLimit: rateLimit}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		scopeJSON, jerr := encodeScope(role.Scope)
		if jerr != nil {
			return jerr
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO roles (name, scope, top_k, rate_limit) VALUES (?, ?, ?, ?)`,
			role.Name, scopeJSON, role.TopK, role.RateLimit)
		return err
	}); err != nil {
		return nil, err
	}
	s.roles[name] = role
	out := *role
	out.Scope = append([]string(nil), role.Scope...)
	return &out, nil
}

// UpdateRoleOpts is a tri-state role patch: a nil field leaves that
// attribute unchanged.
type UpdateRoleOpts struct {
	Scope     *[]string
	TopK      *int
	RateLimit *string
}

// UpdateRole patches a role in place. TopK 0 materializes to the default 5
// exactly as in AddRole (the legacy store stored the 0 and flipped it to 5
// on the next restart; an empty RateLimit is still rejected by the format
// validation, as it always was on this path). A RateLimit change evicts the
// rate limiter of every user whose token holds the role. The row is updated
// in the store database before the maps change; the returned *Role is a copy.
func (s *Store) UpdateRole(name string, opts UpdateRoleOpts) (*Role, error) {
	if opts.RateLimit != nil {
		if err := validateRateLimit(*opts.RateLimit); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[name]
	if !ok {
		return nil, fmt.Errorf("role '%s' does not exist", name)
	}
	next := *role
	if opts.Scope != nil {
		next.Scope = *opts.Scope
	}
	if opts.TopK != nil {
		topK, _ := materializeRoleDefaults(*opts.TopK, next.RateLimit)
		next.TopK = topK
	}
	if opts.RateLimit != nil {
		next.RateLimit = *opts.RateLimit
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		scopeJSON, jerr := encodeScope(next.Scope)
		if jerr != nil {
			return jerr
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE roles SET scope = ?, top_k = ?, rate_limit = ? WHERE name = ?`,
			scopeJSON, next.TopK, next.RateLimit, name)
		if err != nil {
			return err
		}
		return expectOneRow(res, name)
	}); err != nil {
		return nil, err
	}
	*role = next
	if opts.RateLimit != nil {
		for _, tok := range s.tokensByUser {
			if tok.Role == name {
				delete(s.rateLimiters, tok.User)
			}
		}
	}
	out := *role
	out.Scope = append([]string(nil), role.Scope...)
	return &out, nil
}

// DeleteRole removes a role. Guards run in the legacy order: default roles
// (admin/member) are undeletable — checked before existence, so deleting
// "admin" on an empty store still reports "Cannot delete default" — then the
// role must exist, then no token may reference it (RESTRICT kept in Go so a
// dangling role stays representable for the "?" listing). The row is deleted
// from the store database before the maps change.
func (s *Store) DeleteRole(name string) error {
	if isDefaultRoleName(name) {
		return fmt.Errorf("Cannot delete default role '%s'", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roles[name]; !ok {
		return fmt.Errorf("role '%s' does not exist", name)
	}
	for _, tok := range s.tokensByUser {
		if tok.Role == name {
			return fmt.Errorf("Cannot delete role '%s': token for user '%s' is assigned to it", name, tok.User)
		}
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE name = ?`, name)
		if err != nil {
			return err
		}
		return expectOneRow(res, name)
	}); err != nil {
		return err
	}
	delete(s.roles, name)
	return nil
}

// ListRoles returns a name-sorted listing of every role (scope copied
// defensively).
func (s *Store) ListRoles() []RoleInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.roles))
	for n := range s.roles {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]RoleInfo, 0, len(names))
	for _, n := range names {
		r := s.roles[n]
		scope := append([]string(nil), r.Scope...)
		out = append(out, RoleInfo{Name: r.Name, Scope: scope, TopK: r.TopK, RateLimit: r.RateLimit})
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
// exit rather than flushed — Shutdown stays a no-op so daemon shutdown never
// blocks on telemetry, and a stamp lost in the final instant merely means a
// restart shows the previous persisted value. An UPDATE racing the daemon's
// deferred database Close at exit at worst logs one error.
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

// encodeScope renders a role scope as the JSON TEXT array the roles.scope
// column stores (nil encodes as []).
func encodeScope(scope []string) (string, error) {
	if scope == nil {
		scope = []string{}
	}
	data, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("encode scope: %w", err)
	}
	return string(data), nil
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

// Shutdown does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns) and the async last_used writer is abandoned
// at process exit by design; kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Shutdown() {}

// Flush does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns); kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Flush() {}

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
