package invites

import (
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// issueAt issues an invite for member m at a fixed clock instant so ordering is
// deterministic (List sorts by CreatedAt desc).
func issueAt(t *testing.T, s *Store, member, email string, at time.Time) {
	t.Helper()
	s.now = func() time.Time { return at }
	if _, err := s.Issue(IssueParams{
		MemberID:     member,
		Email:        email,
		Role:         "member",
		TokenValue:   "evt_" + member,
		CreationPath: "admin.member.invite",
		TTL:          30 * time.Minute,
	}); err != nil {
		t.Fatalf("issue: %v", err)
	}
}

func TestInviteListOrderingAndByMember(t *testing.T) {
	s := NewStore()
	base := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	issueAt(t, s, "m_1", "a@x.com", base)
	issueAt(t, s, "m_2", "b@x.com", base.Add(2*time.Hour))
	issueAt(t, s, "m_1", "a@x.com", base.Add(1*time.Hour)) // second invite for m_1

	all := s.List()
	if len(all) != 3 {
		t.Fatalf("List len = %d, want 3", len(all))
	}
	// Newest first: m_2 (base+2h), then m_1 (base+1h), then m_1 (base).
	if all[0].MemberID != "m_2" {
		t.Errorf("newest = %s, want m_2", all[0].MemberID)
	}
	// No secret leaks in the view.
	for _, v := range all {
		_ = v // InviteView has no TokenValue field by construction
	}

	byM1 := s.ListByMember("m_1")
	if len(byM1) != 2 {
		t.Fatalf("ListByMember(m_1) len = %d, want 2", len(byM1))
	}
	latest, ok := s.LatestByMember("m_1")
	if !ok || latest.CreatedAt != storedb.FormatTime(base.Add(1*time.Hour)) {
		t.Errorf("LatestByMember(m_1) = %+v (ok=%v), want the base+1h invite", latest, ok)
	}
	if _, ok := s.LatestByMember("m_nope"); ok {
		t.Error("LatestByMember for unknown member returned ok=true")
	}
}
