package invites

import "sort"

// Export returns a full-fidelity copy of every invite row — including the
// sealed token plaintext of pending invites — sorted by handle. It exists for
// the one-time YAML→SQLite importer (internal/storedb/yamlimport), which must
// move rows byte-faithfully; every other read surface uses the secret-free
// InviteView / ClearBundle projections. Never expose the result beyond the
// import path.
func (s *Store) Export() []Invite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Invite, 0, len(s.byHandle))
	for _, inv := range s.byHandle {
		out = append(out, *inv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}
