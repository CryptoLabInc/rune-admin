package invites

import "sort"

// InviteView is a secret-free projection of an Invite for the console listing
// surfaces (user status derivation, issuance history, pending counts). It
// deliberately omits TokenValue and LeaseID — the sealed token never leaves the
// store on a read path, and the design doc states the code value itself is
// never included in any response.
type InviteView struct {
	Handle       string
	MemberID     string
	Email        string
	Role         string
	CreationPath string
	CreatedAt    string // canonical storedb.TimeFormat (RFC3339 UTC ms)
	ExpiresAt    string // canonical storedb.TimeFormat (RFC3339 UTC ms)
	Status       string // pending|consumed|compromised|expired|revoked
}

func (inv *Invite) view() InviteView {
	return InviteView{
		Handle:       inv.Handle,
		MemberID:     inv.MemberID,
		Email:        inv.Email,
		Role:         inv.Role,
		CreationPath: inv.CreationPath,
		CreatedAt:    inv.CreatedAt,
		ExpiresAt:    inv.ExpiresAt,
		Status:       inv.Status,
	}
}

// List returns every invite as a secret-free view, newest issuance first
// (CreatedAt desc, Handle as a stable tiebreaker). It backs the console
// issuance-history page and the per-member status/last-invited derivation.
func (s *Store) List() []InviteView {
	s.mu.RLock()
	out := make([]InviteView, 0, len(s.byHandle))
	for _, inv := range s.byHandle {
		out = append(out, inv.view())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Handle < out[j].Handle
	})
	return out
}

// ListByMember returns a member's invites, newest first. Empty when the member
// has never been invited.
func (s *Store) ListByMember(memberID string) []InviteView {
	all := s.List()
	out := make([]InviteView, 0, 4)
	for _, v := range all {
		if v.MemberID == memberID {
			out = append(out, v)
		}
	}
	return out
}

// LatestByMember returns the most recently issued invite for a member and
// whether one exists. Used to derive lastInvitedAt and the invite_pending /
// invite_expired status.
func (s *Store) LatestByMember(memberID string) (InviteView, bool) {
	byMember := s.ListByMember(memberID)
	if len(byMember) == 0 {
		return InviteView{}, false
	}
	return byMember[0], true
}
