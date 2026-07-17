package tokens

import "sort"

// ExportRoles returns a full-fidelity copy of every role, sorted by name.
// It exists for the one-time YAML→SQLite importer (internal/storedb/yamlimport),
// which needs the raw role rows rather than the RoleInfo listing projection.
func (s *Store) ExportRoles() []Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Role, 0, len(s.roles))
	for _, r := range s.roles {
		cp := *r
		cp.Scope = append([]string(nil), r.Scope...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ExportTokens returns a full-fidelity copy (including the plaintext secret)
// of every effective token row, sorted by user, for the one-time YAML→SQLite
// importer. Never expose the result beyond the import path — every other
// listing surface uses the secret-free TokenInfo projection.
//
// Legacy tokens.yml files may contain duplicate users or duplicate token
// strings; the loader tolerates them silently, keeping the last occurrence in
// each map. A row is "effective" when it wins both indexes (per-user and
// per-token-string keep-last), which is exactly the set that authenticates
// and would survive the next YAML persist today. hadDuplicates reports
// whether any loaded row was shadowed so the importer can warn.
func (s *Store) ExportTokens() (rows []Token, hadDuplicates bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Duplicate users inflate s.tokens; duplicate token strings inflate
	// s.tokensByUser — either kind makes the index sizes diverge.
	hadDuplicates = len(s.tokens) != len(s.tokensByUser)
	rows = make([]Token, 0, len(s.tokensByUser))
	for _, t := range s.tokensByUser {
		if s.tokens[t.Token] != t {
			// This user's row lost its token string to a later duplicate:
			// only the winner authenticates, so only the winner is exported.
			hadDuplicates = true
			continue
		}
		rows = append(rows, *t)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].User < rows[j].User })
	return rows, hadDuplicates
}
