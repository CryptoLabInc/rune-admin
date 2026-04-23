package tokens

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const persistDebounce = 100 * time.Millisecond

type Store struct {
	mu           sync.RWMutex
	tokens       map[string]*Token // keyed by token string
	tokensByUser map[string]*Token // keyed by username
	roles        map[string]*Role
	rateLimiters map[string]*RateLimiter // keyed by username
	rolesPath    string
	tokensPath   string

	now func() time.Time

	persistMu     sync.Mutex
	persistTimer  *time.Timer
	persistWG     sync.WaitGroup
	persistClosed bool
}

func NewStore() *Store {
	return &Store{
		tokens:       make(map[string]*Token),
		tokensByUser: make(map[string]*Token),
		roles:        make(map[string]*Role),
		rateLimiters: make(map[string]*RateLimiter),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// LoadFromFiles reads roles and tokens from YAML at startup.
// Missing files cause defaults to be loaded and an immediate persist scheduled.
func (s *Store) LoadFromFiles(rolesPath, tokensPath string) error {
	s.mu.Lock()
	s.rolesPath = rolesPath
	s.tokensPath = tokensPath

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
			s.mu.Unlock()
			return fmt.Errorf("parse roles file %s: %w", rolesPath, err)
		}
		for name, cfg := range doc.Roles {
			topK := cfg.TopK
			if topK == 0 {
				topK = 5
			}
			rl := cfg.RateLimit
			if rl == "" {
				rl = "30/60s"
			}
			s.roles[name] = &Role{Name: name, Scope: cfg.Scope, TopK: topK, RateLimit: rl}
		}
	} else if !os.IsNotExist(err) {
		s.mu.Unlock()
		return fmt.Errorf("read roles file %s: %w", rolesPath, err)
	} else {
		for name, role := range DefaultRoles() {
			s.roles[name] = role
		}
	}
	for name, role := range DefaultRoles() {
		if _, ok := s.roles[name]; !ok {
			s.roles[name] = role
		}
	}

	// Tokens
	rolesMissing := false
	if _, err := os.Stat(rolesPath); os.IsNotExist(err) {
		rolesMissing = true
	}
	tokensMissing := false
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
			s.mu.Unlock()
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
		s.mu.Unlock()
		return fmt.Errorf("read tokens file %s: %w", tokensPath, err)
	} else {
		tokensMissing = true
	}
	s.mu.Unlock()

	if rolesMissing || tokensMissing {
		s.schedulePersist()
	}
	return nil
}

// LoadDefaultsWithDemoToken seeds the store with default roles and the demo token.
// Useful for dev/CI bootstraps that don't ship persisted state.
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
	s.mu.Unlock()
	if err != nil {
		return "", nil, err
	}
	if !limiter.IsAllowed(tok.User) {
		return "", nil, ErrRateLimit{RetryAfter: limiter.RetryAfter(tok.User)}
	}
	return tok.User, role, nil
}

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

func (s *Store) AddToken(user, roleName string, expiresDays *int) (*Token, error) {
	s.mu.Lock()
	if _, ok := s.roles[roleName]; !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("role '%s' does not exist", roleName)
	}
	if _, ok := s.tokensByUser[user]; ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("token already exists for user '%s'", user)
	}
	tokStr, err := newTokenString()
	if err != nil {
		s.mu.Unlock()
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
	s.tokens[tok.Token] = tok
	s.tokensByUser[tok.User] = tok
	s.mu.Unlock()
	s.schedulePersist()
	return tok, nil
}

func (s *Store) RevokeToken(user string) bool {
	s.mu.Lock()
	tok, ok := s.tokensByUser[user]
	if !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.tokensByUser, user)
	delete(s.tokens, tok.Token)
	if l, ok := s.rateLimiters[user]; ok {
		delete(s.rateLimiters, user)
		l.Remove(user)
	}
	s.mu.Unlock()
	s.schedulePersist()
	return true
}

func (s *Store) RotateToken(user string) (*Token, error) {
	s.mu.Lock()
	old, ok := s.tokensByUser[user]
	if !ok {
		s.mu.Unlock()
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
	delete(s.tokens, old.Token)
	delete(s.tokensByUser, user)
	if l, ok := s.rateLimiters[user]; ok {
		delete(s.rateLimiters, user)
		l.Remove(user)
	}
	tokStr, err := newTokenString()
	if err != nil {
		s.mu.Unlock()
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
	s.tokens[newTok.Token] = newTok
	s.tokensByUser[user] = newTok
	s.mu.Unlock()
	s.schedulePersist()
	return newTok, nil
}

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

type TokenInfo struct {
	User      string `json:"user" yaml:"user"`
	Role      string `json:"role" yaml:"role"`
	TopK      any    `json:"top_k" yaml:"top_k"`
	RateLimit any    `json:"rate_limit" yaml:"rate_limit"`
	Expires   string `json:"expires" yaml:"expires"`
}

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
		info := TokenInfo{User: tok.User, Role: tok.Role, Expires: "never"}
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

type RoleInfo struct {
	Name      string   `json:"name" yaml:"name"`
	Scope     []string `json:"scope" yaml:"scope"`
	TopK      int      `json:"top_k" yaml:"top_k"`
	RateLimit string   `json:"rate_limit" yaml:"rate_limit"`
}

func (s *Store) AddRole(name string, scope []string, topK int, rateLimit string) (*Role, error) {
	if err := validateRateLimit(rateLimit); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if _, ok := s.roles[name]; ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("role '%s' already exists", name)
	}
	role := &Role{Name: name, Scope: scope, TopK: topK, RateLimit: rateLimit}
	s.roles[name] = role
	s.mu.Unlock()
	s.schedulePersist()
	return role, nil
}

type UpdateRoleOpts struct {
	Scope     *[]string
	TopK      *int
	RateLimit *string
}

func (s *Store) UpdateRole(name string, opts UpdateRoleOpts) (*Role, error) {
	if opts.RateLimit != nil {
		if err := validateRateLimit(*opts.RateLimit); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	role, ok := s.roles[name]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("role '%s' does not exist", name)
	}
	if opts.Scope != nil {
		role.Scope = *opts.Scope
	}
	if opts.TopK != nil {
		role.TopK = *opts.TopK
	}
	if opts.RateLimit != nil {
		role.RateLimit = *opts.RateLimit
		for _, tok := range s.tokensByUser {
			if tok.Role == name {
				delete(s.rateLimiters, tok.User)
			}
		}
	}
	s.mu.Unlock()
	s.schedulePersist()
	return role, nil
}

func (s *Store) DeleteRole(name string) error {
	if isDefaultRoleName(name) {
		return fmt.Errorf("Cannot delete default role '%s'", name)
	}
	s.mu.Lock()
	if _, ok := s.roles[name]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("role '%s' does not exist", name)
	}
	for _, tok := range s.tokensByUser {
		if tok.Role == name {
			s.mu.Unlock()
			return fmt.Errorf("Cannot delete role '%s': token for user '%s' is assigned to it", name, tok.User)
		}
	}
	delete(s.roles, name)
	s.mu.Unlock()
	s.schedulePersist()
	return nil
}

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

// Shutdown cancels any pending persist and waits for in-flight writes to finish.
// Use Flush instead when you want pending changes to be written before exit.
func (s *Store) Shutdown() {
	s.persistMu.Lock()
	s.persistClosed = true
	if s.persistTimer != nil {
		s.persistTimer.Stop()
		s.persistTimer = nil
	}
	s.persistMu.Unlock()
	s.persistWG.Wait()
}

// Flush forces any pending debounced persist to run synchronously,
// then blocks until in-flight writes complete.
func (s *Store) Flush() {
	s.persistMu.Lock()
	pending := false
	if s.persistTimer != nil {
		if s.persistTimer.Stop() {
			pending = true
		}
		s.persistTimer = nil
	}
	s.persistMu.Unlock()
	if pending {
		s.doPersist()
	}
	s.persistWG.Wait()
}

func (s *Store) schedulePersist() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistClosed {
		return
	}
	if s.rolesPath == "" || s.tokensPath == "" {
		return
	}
	if s.persistTimer != nil {
		s.persistTimer.Stop()
	}
	s.persistTimer = time.AfterFunc(persistDebounce, func() {
		s.persistMu.Lock()
		s.persistTimer = nil
		closed := s.persistClosed
		s.persistMu.Unlock()
		if closed {
			return
		}
		s.doPersist()
	})
}

func (s *Store) doPersist() {
	s.persistWG.Add(1)
	defer s.persistWG.Done()

	s.mu.RLock()
	rolesPath := s.rolesPath
	tokensPath := s.tokensPath

	rolesDoc := struct {
		Roles map[string]struct {
			Scope     []string `yaml:"scope"`
			TopK      int      `yaml:"top_k"`
			RateLimit string   `yaml:"rate_limit"`
		} `yaml:"roles"`
	}{Roles: make(map[string]struct {
		Scope     []string `yaml:"scope"`
		TopK      int      `yaml:"top_k"`
		RateLimit string   `yaml:"rate_limit"`
	})}
	for n, r := range s.roles {
		rolesDoc.Roles[n] = struct {
			Scope     []string `yaml:"scope"`
			TopK      int      `yaml:"top_k"`
			RateLimit string   `yaml:"rate_limit"`
		}{Scope: append([]string(nil), r.Scope...), TopK: r.TopK, RateLimit: r.RateLimit}
	}

	tokensDoc := struct {
		Tokens []map[string]string `yaml:"tokens"`
	}{Tokens: make([]map[string]string, 0, len(s.tokensByUser))}
	users := make([]string, 0, len(s.tokensByUser))
	for u := range s.tokensByUser {
		users = append(users, u)
	}
	sort.Strings(users)
	for _, u := range users {
		t := s.tokensByUser[u]
		entry := map[string]string{
			"user":      t.User,
			"token":     t.Token,
			"role":      t.Role,
			"issued_at": t.IssuedAt,
		}
		if t.Expires != "" {
			entry["expires"] = t.Expires
		}
		tokensDoc.Tokens = append(tokensDoc.Tokens, entry)
	}
	s.mu.RUnlock()

	if err := atomicWriteYAML(rolesPath, rolesDoc); err != nil {
		fmt.Fprintf(os.Stderr, "tokens: persist roles failed: %v\n", err)
	}
	if err := atomicWriteYAML(tokensPath, tokensDoc); err != nil {
		fmt.Fprintf(os.Stderr, "tokens: persist tokens failed: %v\n", err)
	}
}

func atomicWriteYAML(path string, data any) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".persist-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	enc := yaml.NewEncoder(tmp)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		_ = enc.Close()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := enc.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
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
