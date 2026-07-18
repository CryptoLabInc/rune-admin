package groups

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
)

// testTree builds the plan §5 three-level tree:
// hq (본부) > dev-team (개발팀) > search-part (검색파트).
func testTree(t *testing.T) (s *Store, hq, dev, search Group) {
	t.Helper()
	s = NewStore()
	var err error
	if hq, err = s.CreateGroup("hq", ""); err != nil {
		t.Fatalf("create hq: %v", err)
	}
	if dev, err = s.CreateGroup("dev-team", hq.ID); err != nil {
		t.Fatalf("create dev-team: %v", err)
	}
	if search, err = s.CreateGroup("search-part", dev.ID); err != nil {
		t.Fatalf("create search-part: %v", err)
	}
	return s, hq, dev, search
}

// mustGrant grants directly. User keys are emails (plan §0 / D2).
func mustGrant(t *testing.T, s *Store, user, group string, role Role) {
	t.Helper()
	if _, err := s.Grant(user, group, role, "local-admin:test"); err != nil {
		t.Fatalf("grant %s %s %s: %v", user, group, role, err)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

// TestS0_RecallDiffersByAuthorization is the #0 top-priority guarantee
// (plan §0): a superior's captured memory is invisible to a subordinate,
// while a superior still reads a subordinate's memory (downward inheritance).
// Tree 본부(hq) > 개발팀(dev); 지수=(hq, write), 민호=(dev, read).
func TestS0_RecallDiffersByAuthorization(t *testing.T) {
	s := NewStore()
	hq, _ := s.CreateGroup("본부", "")
	dev, _ := s.CreateGroup("개발팀", hq.ID)
	mustGrant(t, s, "jisoo@corp.com", hq.ID, RoleWrite)
	mustGrant(t, s, "minho@corp.com", dev.ID, RoleRead)

	// 지수 captures into 본부 only: default = direct write groups. 개발팀 is
	// inherited (not direct), so it is NOT a tag candidate — the anti-leak core.
	tags, err := s.CaptureTagSet("jisoo@corp.com", nil)
	if err != nil {
		t.Fatalf("CaptureTagSet(jisoo, nil) = %v", err)
	}
	if !reflect.DeepEqual(tags, []string{hq.ID}) {
		t.Fatalf("capture tags = %v, want [본부 only] (%s)", tags, hq.ID)
	}

	// Core §0 assertion at the judge layer (runespace enforces tags∩scope;
	// here we prove the scopes that drive it):
	if !contains(s.RecallScope("jisoo@corp.com"), hq.ID) {
		t.Error("지수 should see 본부 memory (hq must be in jisoo's recall scope)")
	}
	if contains(s.RecallScope("minho@corp.com"), hq.ID) {
		t.Error("§0 VIOLATION: 민호 must NOT see 본부 memory (hq must not be in minho's scope)")
	}

	// Downward inheritance still holds: 지수 reads 민호's 개발팀 memory.
	if !contains(s.RecallScope("jisoo@corp.com"), dev.ID) {
		t.Error("지수 should see 개발팀 memory (downward inheritance)")
	}
	// 민호 has no upward permission on 본부.
	if _, ok := s.EffectiveRole("minho@corp.com", hq.ID); ok {
		t.Error("민호 must have no effective role on 본부 (inheritance is downward only)")
	}
	// 지수 cannot tag the inherited 개발팀 on capture (direct groups only).
	if _, err := s.CaptureTagSet("jisoo@corp.com", []string{dev.ID}); !errors.As(err, new(ErrNotDirectMember)) {
		t.Errorf("CaptureTagSet(jisoo, [개발팀]) = %v, want ErrNotDirectMember", err)
	}
}

// TestEffectiveRoleCombinationTable — plan §6-D3 combination table:
// 3-level tree × 3 group roles × membership level, downward-only inheritance.
func TestEffectiveRoleCombinationTable(t *testing.T) {
	roles := []Role{RoleRead, RoleWrite, RoleEdit}
	levels := []string{"hq", "dev-team", "search-part"}
	want := map[string]map[string]bool{
		"hq":          {"hq": true, "dev-team": true, "search-part": true},
		"dev-team":    {"hq": false, "dev-team": true, "search-part": true},
		"search-part": {"hq": false, "dev-team": false, "search-part": true},
	}
	for _, r := range roles {
		for _, memberAt := range levels {
			t.Run(fmt.Sprintf("%s@%s", r, memberAt), func(t *testing.T) {
				s, _, _, _ := testTree(t)
				mustGrant(t, s, "u@corp.com", memberAt, r)
				for _, target := range levels {
					got, ok := s.EffectiveRole("u@corp.com", target)
					if want[memberAt][target] {
						if !ok || got != r {
							t.Errorf("EffectiveRole(u, %s) = (%q, %v), want (%q, true)", target, got, ok, r)
						}
					} else if ok {
						t.Errorf("EffectiveRole(u, %s) = (%q, true), want none", target, got)
					}
				}
			})
		}
	}
}

// TestPlanMiniExample — updated plan §5 worked example:
// 지수=(hq, write), 민호=(search-part, edit).
func TestPlanMiniExample(t *testing.T) {
	s, hq, _, search := testTree(t)
	mustGrant(t, s, "jisoo@corp.com", hq.ID, RoleWrite)
	mustGrant(t, s, "minho@corp.com", search.ID, RoleEdit)

	// 유효 role(지수, 검색파트) = write (본부 멤버십이 하위로 상속).
	if r, ok := s.EffectiveRole("jisoo@corp.com", search.ID); !ok || r != RoleWrite {
		t.Errorf("EffectiveRole(jisoo, search) = (%q, %v), want (write, true)", r, ok)
	}
	// 유효 role(민호, 본부) = 없음 (상속은 아래로만).
	if r, ok := s.EffectiveRole("minho@corp.com", hq.ID); ok {
		t.Errorf("EffectiveRole(minho, hq) = (%q, true), want none", r)
	}
	// 지수가 capture 때 태그할 수 있는 대상 = {본부} 뿐 (직속만).
	if got := s.CaptureTargets("jisoo@corp.com"); !reflect.DeepEqual(got, []string{hq.ID}) {
		t.Errorf("CaptureTargets(jisoo) = %v, want [hq only]", got)
	}
	// grant/revoke는 조직 admin 1명만 — 지수도 민호도 불가.
	if err := s.CanGrant("jisoo@corp.com", "x@corp.com", search.ID, RoleRead); !errors.As(err, new(ErrNotAdmin)) {
		t.Errorf("CanGrant(jisoo) = %v, want ErrNotAdmin", err)
	}
	if err := s.CanGrant("minho@corp.com", "x@corp.com", search.ID, RoleRead); !errors.As(err, new(ErrNotAdmin)) {
		t.Errorf("CanGrant(minho) = %v, want ErrNotAdmin", err)
	}
}

func TestMultiMembershipTakesMax(t *testing.T) {
	s, hq, dev, search := testTree(t)
	mustGrant(t, s, "u@corp.com", hq.ID, RoleRead)
	mustGrant(t, s, "u@corp.com", search.ID, RoleEdit)
	cases := []struct {
		group string
		want  Role
	}{
		{hq.ID, RoleRead},
		{dev.ID, RoleRead},    // inherited read from hq
		{search.ID, RoleEdit}, // max(read via hq, edit direct)
	}
	for _, c := range cases {
		if got, ok := s.EffectiveRole("u@corp.com", c.group); !ok || got != c.want {
			t.Errorf("EffectiveRole(u, %s) = (%q, %v), want (%q, true)", c.group, got, ok, c.want)
		}
	}
}

// directGroupIDs is the sorted set of group IDs in a GroupAccessView's direct
// memberships — a test helper to assert the direct/inherited split.
func directGroupIDs(ga GroupAccessView) []string {
	out := make([]string, 0, len(ga.Direct))
	for _, m := range ga.Direct {
		out = append(out, m.GroupID)
	}
	sort.Strings(out)
	return out
}

// TestGroupAccess — the console-display split: Direct (explicit memberships,
// real role) vs Inherited (descendants of a direct group, minus direct groups,
// always read). Complement to RecallScope (which includes direct groups and
// keeps the effective role). Tree hq > dev-team > search-part.
func TestGroupAccess(t *testing.T) {
	t.Run("edit at root inherits read on every descendant", func(t *testing.T) {
		s, hq, dev, search := testTree(t)
		mustGrant(t, s, "ceo@corp.com", hq.ID, RoleEdit)
		ga := s.GroupAccess("ceo@corp.com")
		// hq is direct (edit); dev + search are inherited read (disjoint).
		if got, want := directGroupIDs(ga), []string{hq.ID}; !reflect.DeepEqual(got, want) {
			t.Errorf("direct = %v, want [hq]", got)
		}
		if got, want := ga.Inherited, sortedCopy([]string{dev.ID, search.ID}); !reflect.DeepEqual(got, want) {
			t.Errorf("inherited = %v, want %v", got, want)
		}
	})

	t.Run("read at mid-level inherits read on the leaf only", func(t *testing.T) {
		s, _, dev, search := testTree(t)
		mustGrant(t, s, "u@corp.com", dev.ID, RoleRead)
		ga := s.GroupAccess("u@corp.com")
		if got, want := ga.Inherited, []string{search.ID}; !reflect.DeepEqual(got, want) {
			t.Errorf("inherited = %v, want %v", got, want)
		}
	})

	t.Run("leaf membership inherits nothing", func(t *testing.T) {
		s, _, _, search := testTree(t)
		mustGrant(t, s, "u@corp.com", search.ID, RoleEdit)
		if got := s.GroupAccess("u@corp.com").Inherited; len(got) != 0 {
			t.Errorf("inherited = %v, want empty", got)
		}
	})

	t.Run("promotion moves a group from inherited to direct", func(t *testing.T) {
		s, hq, dev, search := testTree(t)
		mustGrant(t, s, "ceo@corp.com", hq.ID, RoleEdit)
		// Admin grants an explicit write on dev-team (the promotion).
		mustGrant(t, s, "ceo@corp.com", dev.ID, RoleWrite)
		ga := s.GroupAccess("ceo@corp.com")
		// dev is now DIRECT → out of the inherited set; only the leaf remains
		// inherited read (write does not broadcast down).
		if got, want := directGroupIDs(ga), sortedCopy([]string{hq.ID, dev.ID}); !reflect.DeepEqual(got, want) {
			t.Errorf("after promotion direct = %v, want [hq, dev]", got)
		}
		if got, want := ga.Inherited, []string{search.ID}; !reflect.DeepEqual(got, want) {
			t.Errorf("after promotion inherited = %v, want %v", got, want)
		}
	})

	t.Run("no memberships → empty", func(t *testing.T) {
		s, _, _, _ := testTree(t)
		ga := s.GroupAccess("nobody@corp.com")
		if len(ga.Direct) != 0 || len(ga.Inherited) != 0 {
			t.Errorf("GroupAccess(nobody) = %+v, want empty", ga)
		}
	})

	t.Run("GroupAccessByUser matches per-user GroupAccess", func(t *testing.T) {
		s, hq, dev, search := testTree(t)
		mustGrant(t, s, "ceo@corp.com", hq.ID, RoleEdit)
		mustGrant(t, s, "lead@corp.com", dev.ID, RoleWrite)
		bulk := s.GroupAccessByUser()
		for _, u := range []string{"ceo@corp.com", "lead@corp.com"} {
			if got, want := bulk[u].Inherited, s.GroupAccess(u).Inherited; !reflect.DeepEqual(got, want) {
				t.Errorf("bulk[%s].Inherited = %v, want %v", u, got, want)
			}
			if got, want := directGroupIDs(bulk[u]), directGroupIDs(s.GroupAccess(u)); !reflect.DeepEqual(got, want) {
				t.Errorf("bulk[%s] direct = %v, want %v", u, got, want)
			}
		}
		_ = search
	})
}

// TestGroupAccessNoTornRead pins the GroupAccessView disjointness invariant
// under concurrency: Direct and Inherited are captured under ONE read lock, so
// a reader must never observe a group in both sets, nor see a reachable
// subtree vanish from both while an ancestor grant is held. Computing the two
// sets from separate lock acquisitions (an earlier design) fails this.
// Run with -race; the iteration count is kept modest because the race detector
// — not raw repetition — is what surfaces the interleaving.
func TestGroupAccessNoTornRead(t *testing.T) {
	s, hq, dev, search := testTree(t)
	const user = "ceo@corp.com"
	// hq stays granted for the whole test: dev/search are therefore always
	// reachable — directly or by inheritance — never neither.
	mustGrant(t, s, user, hq.ID, RoleEdit)

	// assertConsistent reports (not fails) so it is safe from goroutines and a
	// failing run prints one line per issue instead of thousands.
	assertConsistent := func(ga GroupAccessView) error {
		direct := make(map[string]bool, len(ga.Direct))
		for _, m := range ga.Direct {
			direct[m.GroupID] = true
		}
		for _, gid := range ga.Inherited {
			if direct[gid] {
				return fmt.Errorf("torn read: group %s is BOTH direct and inherited", gid)
			}
		}
		if !direct[hq.ID] {
			return fmt.Errorf("hq grant disappeared: direct=%v", ga.Direct)
		}
		for _, gid := range []string{dev.ID, search.ID} {
			if !direct[gid] && !contains(ga.Inherited, gid) {
				return fmt.Errorf("torn read: %s vanished from both sets while hq was direct", gid)
			}
		}
		return nil
	}

	var writer, readers sync.WaitGroup
	stop := make(chan struct{})
	errs := make(chan error, 64)

	// Writer: churn a direct grant/revoke on dev — the mutation that moves dev
	// between the direct and inherited sets. Runs until the readers are done.
	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = s.Grant(user, dev.ID, RoleWrite, "local-admin:test")
			_, _ = s.RevokeDirectGrant(user, dev.ID)
		}
	}()

	// Readers: assert the invariant on both the single-user and org-wide paths.
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 200; j++ {
				if err := assertConsistent(s.GroupAccess(user)); err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
				for _, ga := range s.GroupAccessByUser() {
					if err := assertConsistent(ga); err != nil {
						select {
						case errs <- err:
						default:
						}
						return
					}
				}
			}
		}()
	}
	readers.Wait() // readers race the live writer for their whole run …
	close(stop)    // … only then does the writer stop
	writer.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRecallScope(t *testing.T) {
	s, hq, dev, search := testTree(t)
	mustGrant(t, s, "jisoo@corp.com", hq.ID, RoleWrite)
	mustGrant(t, s, "minho@corp.com", search.ID, RoleEdit)
	mustGrant(t, s, "reader@corp.com", dev.ID, RoleRead)

	if got, want := s.RecallScope("jisoo@corp.com"), sortedCopy([]string{hq.ID, dev.ID, search.ID}); !reflect.DeepEqual(got, want) {
		t.Errorf("RecallScope(jisoo) = %v, want %v", got, want)
	}
	if got, want := s.RecallScope("minho@corp.com"), []string{search.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("RecallScope(minho) = %v, want %v", got, want)
	}
	// read is the lowest role — it still contributes the full subtree.
	if got, want := s.RecallScope("reader@corp.com"), sortedCopy([]string{dev.ID, search.ID}); !reflect.DeepEqual(got, want) {
		t.Errorf("RecallScope(reader) = %v, want %v", got, want)
	}
	if got := s.RecallScope("nobody@corp.com"); len(got) != 0 {
		t.Errorf("RecallScope(nobody) = %v, want empty", got)
	}
}

// TestPermissionsNoExistenceOracle — a nonexistent rootRef must be
// indistinguishable from an existing group outside the caller's reach:
// both yield the caller's direct memberships plus an EMPTY tree, never
// an error. A distinguishable not-found would let any valid token
// enumerate which group ids/names exist org-wide (resolveLocked also
// matches display names).
func TestPermissionsNoExistenceOracle(t *testing.T) {
	s, _, _, search := testTree(t)
	// An existing root the caller cannot reach at all (sibling of hq, so
	// its subtree shares nothing with the caller's recall scope).
	secret, err := s.CreateGroup("z-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	mustGrant(t, s, "minho@corp.com", search.ID, RoleRead)

	unreachable, err := s.Permissions("minho@corp.com", secret.ID)
	if err != nil {
		t.Fatalf("Permissions(unreachable existing group) = %v, want nil error", err)
	}
	if len(unreachable.Tree) != 0 {
		t.Fatalf("unreachable tree = %+v, want empty", unreachable.Tree)
	}
	if len(unreachable.Memberships) != 1 || unreachable.Memberships[0].GroupID != search.ID {
		t.Fatalf("memberships = %+v, want minho's direct (search-part) row", unreachable.Memberships)
	}

	for _, ref := range []string{"no-such-id", "no-such-name"} {
		nonexistent, err := s.Permissions("minho@corp.com", ref)
		if err != nil {
			t.Fatalf("Permissions(%q) = %v, want nil error (no existence oracle)", ref, err)
		}
		if !reflect.DeepEqual(nonexistent, unreachable) {
			t.Errorf("Permissions(%q) = %+v, want deep-equal to unreachable-group view %+v", ref, nonexistent, unreachable)
		}
	}
	// Same guarantee when the unreachable ref is a display name.
	byName, err := s.Permissions("minho@corp.com", "z-secret")
	if err != nil || !reflect.DeepEqual(byName, unreachable) {
		t.Errorf("Permissions(by unreachable name) = (%+v, %v), want the same empty-tree view", byName, err)
	}
}

func TestRevokeIsImmediate(t *testing.T) {
	s, hq, _, _ := testTree(t)
	mustGrant(t, s, "u@corp.com", hq.ID, RoleWrite)
	if len(s.RecallScope("u@corp.com")) != 3 {
		t.Fatalf("scope before revoke = %v", s.RecallScope("u@corp.com"))
	}
	ok, err := s.RevokeDirectGrant("u@corp.com", hq.ID)
	if err != nil || !ok {
		t.Fatalf("Revoke = (%v, %v)", ok, err)
	}
	if got := s.RecallScope("u@corp.com"); len(got) != 0 {
		t.Errorf("scope after revoke = %v, want empty", got)
	}
	if _, ok := s.EffectiveRole("u@corp.com", hq.ID); ok {
		t.Error("EffectiveRole after revoke should be none")
	}
}

// TestCaptureTagSet — plan §5/§6-D6 capture rules (the §0 anti-leak gate).
func TestCaptureTagSet(t *testing.T) {
	s, hq, dev, search := testTree(t)
	mustGrant(t, s, "jisoo@corp.com", hq.ID, RoleWrite)     // direct write at hq
	mustGrant(t, s, "reader@corp.com", search.ID, RoleRead) // read only

	t.Run("default = direct write groups only", func(t *testing.T) {
		got, err := s.CaptureTagSet("jisoo@corp.com", nil)
		if err != nil || !reflect.DeepEqual(got, []string{hq.ID}) {
			t.Errorf("CaptureTagSet(jisoo, nil) = (%v, %v), want ([hq], nil)", got, err)
		}
	})
	t.Run("explicit direct group ok", func(t *testing.T) {
		got, err := s.CaptureTagSet("jisoo@corp.com", []string{hq.ID})
		if err != nil || !reflect.DeepEqual(got, []string{hq.ID}) {
			t.Errorf("= (%v, %v)", got, err)
		}
	})
	t.Run("inherited descendant rejected", func(t *testing.T) {
		if _, err := s.CaptureTagSet("jisoo@corp.com", []string{dev.ID}); !errors.As(err, new(ErrNotDirectMember)) {
			t.Errorf("= %v, want ErrNotDirectMember", err)
		}
	})
	t.Run("read-only user rejected", func(t *testing.T) {
		if _, err := s.CaptureTagSet("reader@corp.com", nil); !errors.As(err, new(ErrNoWriteGroup)) {
			t.Errorf("= %v, want ErrNoWriteGroup", err)
		}
	})
}

// TestCanGrantOrgAdminOnly — grant is reserved for the single org admin (Owner).
func TestCanGrantOrgAdminOnly(t *testing.T) {
	s, hq, _, search := testTree(t)
	mustGrant(t, s, "jisoo@corp.com", hq.ID, RoleEdit) // high group role, still not admin
	s.SetOrgAdmin("owner@corp.com")

	if err := s.CanGrant("owner@corp.com", "x@corp.com", search.ID, RoleWrite); err != nil {
		t.Errorf("CanGrant(owner) = %v, want allowed", err)
	}
	if err := s.CanGrant("jisoo@corp.com", "x@corp.com", search.ID, RoleWrite); !errors.As(err, new(ErrNotAdmin)) {
		t.Errorf("CanGrant(jisoo edit) = %v, want ErrNotAdmin", err)
	}
	if !s.IsOrgAdmin("owner@corp.com") || s.IsOrgAdmin("jisoo@corp.com") {
		t.Error("IsOrgAdmin wrong")
	}
	if err := s.CanGrant("owner@corp.com", "x@corp.com", search.ID, Role("boss")); err == nil {
		t.Error("invalid role should be rejected")
	}
	if err := s.CanGrant("owner@corp.com", "x@corp.com", "no-such", RoleRead); !errors.As(err, new(ErrGroupNotFound)) {
		t.Errorf("unknown group = %v, want ErrGroupNotFound", err)
	}
}

func TestTopKLimit(t *testing.T) {
	s, hq, _, search := testTree(t)
	mustGrant(t, s, "reader@corp.com", search.ID, RoleRead)
	mustGrant(t, s, "writer@corp.com", hq.ID, RoleWrite)
	mustGrant(t, s, "editor@corp.com", search.ID, RoleEdit)
	mustGrant(t, s, "mixed@corp.com", hq.ID, RoleRead)
	mustGrant(t, s, "mixed@corp.com", search.ID, RoleWrite)
	cases := []struct {
		user string
		want int
	}{
		{"reader@corp.com", 10},
		{"writer@corp.com", 50},
		{"editor@corp.com", 50},
		{"mixed@corp.com", 50}, // best membership role wins
		{"nobody@corp.com", 10},
	}
	for _, c := range cases {
		if got := s.TopKLimit(c.user); got != c.want {
			t.Errorf("TopKLimit(%s) = %d, want %d", c.user, got, c.want)
		}
	}
	s.SetLimits(Limits{TopKRead: 7, TopKWrite: 99})
	if s.TopKLimit("reader@corp.com") != 7 || s.TopKLimit("writer@corp.com") != 99 {
		t.Error("SetLimits not applied")
	}
}

func TestDescendantsWithDepth(t *testing.T) {
	s, hq, dev, search := testTree(t)
	ops, err := s.CreateGroup("a-ops", hq.ID) // second child to check sibling ordering
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DescendantsWithDepth(hq.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupDepth{
		{ID: hq.ID, Name: "hq", Depth: 0},
		{ID: ops.ID, Name: "a-ops", Depth: 1},
		{ID: dev.ID, Name: "dev-team", Depth: 1},
		{ID: search.ID, Name: "search-part", Depth: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DescendantsWithDepth(hq) = %+v, want %+v", got, want)
	}
}

// stubStats is a canned TagStatsProvider for delete-guard tests.
type stubStats struct {
	m     map[string]TagStat
	err   error
	calls [][]string
}

func (s *stubStats) GetTagStats(tags []string) (map[string]TagStat, error) {
	s.calls = append(s.calls, append([]string(nil), tags...))
	if s.err != nil {
		return nil, s.err
	}
	return s.m, nil
}

func TestDeleteCheckTripleGuard(t *testing.T) {
	emptyStats := &stubStats{m: map[string]TagStat{}}

	t.Run("children block deletion", func(t *testing.T) {
		s, hq, _, _ := testTree(t)
		if err := s.DeleteCheck(hq.ID, emptyStats); !errors.As(err, new(ErrHasChildren)) {
			t.Errorf("= %v, want ErrHasChildren", err)
		}
	})
	t.Run("members block deletion", func(t *testing.T) {
		s, _, _, search := testTree(t)
		mustGrant(t, s, "u@corp.com", search.ID, RoleRead)
		if err := s.DeleteCheck(search.ID, emptyStats); !errors.As(err, new(ErrHasMembers)) {
			t.Errorf("= %v, want ErrHasMembers", err)
		}
	})
	t.Run("sole-tag records block deletion", func(t *testing.T) {
		s, _, _, search := testTree(t)
		stats := &stubStats{m: map[string]TagStat{search.ID: {Total: 5, Sole: 2}}}
		if err := s.DeleteCheck(search.ID, stats); !errors.As(err, new(ErrSoleTagRecords)) {
			t.Errorf("= %v, want ErrSoleTagRecords", err)
		}
	})
	t.Run("multi-tag-only records do not block", func(t *testing.T) {
		s, _, _, search := testTree(t)
		stats := &stubStats{m: map[string]TagStat{search.ID: {Total: 5, Sole: 0}}}
		if err := s.DeleteCheck(search.ID, stats); err != nil {
			t.Errorf("= %v, want nil", err)
		}
	})
	t.Run("nil provider fails closed", func(t *testing.T) {
		s, _, _, search := testTree(t)
		if err := s.DeleteCheck(search.ID, nil); !errors.As(err, new(ErrTagStatsUnavailable)) {
			t.Errorf("= %v, want ErrTagStatsUnavailable", err)
		}
	})
	t.Run("provider error fails closed", func(t *testing.T) {
		s, _, _, search := testTree(t)
		stats := &stubStats{err: errors.New("runespace unreachable")}
		if err := s.DeleteCheck(search.ID, stats); !errors.As(err, new(ErrTagStatsUnavailable)) {
			t.Errorf("= %v, want ErrTagStatsUnavailable", err)
		}
	})
	t.Run("clean leaf passes and delete succeeds", func(t *testing.T) {
		s, _, _, search := testTree(t)
		if err := s.DeleteCheck(search.ID, emptyStats); err != nil {
			t.Fatalf("DeleteCheck(clean leaf) = %v", err)
		}
		if _, err := s.DeleteGroup(search.ID, emptyStats); err != nil {
			t.Fatalf("DeleteGroup(clean leaf) = %v", err)
		}
		if _, err := s.ResolveGroup(search.ID); err == nil {
			t.Error("group still resolvable after delete")
		}
	})
	t.Run("local guards before remote call", func(t *testing.T) {
		s, hq, _, _ := testTree(t)
		stats := &stubStats{m: map[string]TagStat{}}
		_ = s.DeleteCheck(hq.ID, stats)
		if len(stats.calls) != 0 {
			t.Errorf("GetTagStats called %d times for a group with children, want 0", len(stats.calls))
		}
	})
}

// TestExcludeReadBlocksRecallScopeNodeOnly pins the carve-out semantics: a
// denial must remove the group from the RECALL SCOPE (the real memory-read
// gate), not merely from a console listing — and it must cut that group ONLY,
// leaving its descendants inherited, since each team owns its memory 1:1 with
// its tag.
func TestExcludeReadBlocksRecallScopeNodeOnly(t *testing.T) {
	s, hq, dev, search := testTree(t) // hq > dev-team > search-part
	const ceo = "ceo@corp.com"
	mustGrant(t, s, ceo, hq.ID, RoleWrite)

	// Baseline: the whole subtree is readable, dev/search purely by inheritance.
	if got, want := s.RecallScope(ceo), sortedCopy([]string{hq.ID, dev.ID, search.ID}); !reflect.DeepEqual(got, want) {
		t.Fatalf("baseline RecallScope = %v, want %v", got, want)
	}

	excluded, err := s.ExcludeRead(ceo, dev.ID, "local-admin:test")
	if err != nil || !excluded {
		t.Fatalf("ExcludeRead(dev) = (%v, %v), want (true, nil)", excluded, err)
	}

	// dev leaves the recall scope; search-part — a descendant of dev — stays,
	// because it still descends from the direct hq membership and was not removed.
	if got, want := s.RecallScope(ceo), sortedCopy([]string{hq.ID, search.ID}); !reflect.DeepEqual(got, want) {
		t.Fatalf("after removal RecallScope = %v, want %v (only that team is cut)", got, want)
	}
	if role, ok := s.EffectiveRole(ceo, dev.ID); ok {
		t.Errorf("EffectiveRole(dev) = (%q, true), want no permission", role)
	}
	if role, ok := s.EffectiveRole(ceo, search.ID); !ok || role != RoleWrite {
		t.Errorf("EffectiveRole(search) = (%q, %v), want (write, true) — descendants unaffected", role, ok)
	}

	// Console view agrees with the gate: dev is gone from inherited, hq stays direct.
	ga := s.GroupAccess(ceo)
	if len(ga.Direct) != 1 || ga.Direct[0].GroupID != hq.ID {
		t.Errorf("Direct = %+v, want [hq]", ga.Direct)
	}
	if !reflect.DeepEqual(ga.Inherited, []string{search.ID}) {
		t.Errorf("Inherited = %v, want [search-part] (dev removed)", ga.Inherited)
	}
}

// TestExcludeReadRefusesWhenNothingToDeny — a denial is recorded only when it
// actually takes something away, so the store never fills with inert rows.
func TestExcludeReadRefusesWhenNothingToDeny(t *testing.T) {
	s, hq, dev, _ := testTree(t)
	const ceo = "ceo@corp.com"
	mustGrant(t, s, ceo, hq.ID, RoleWrite)
	other, err := s.CreateGroup("unrelated", "")
	if err != nil {
		t.Fatal(err)
	}

	// Directly held → revoke it instead; an explicit grant outranks a carve-out.
	if ok, err := s.ExcludeRead(ceo, hq.ID, "t"); ok || err != nil {
		t.Errorf("ExcludeRead(direct hq) = (%v, %v), want (false, nil)", ok, err)
	}
	// Unreachable → nothing to take away.
	if ok, err := s.ExcludeRead(ceo, other.ID, "t"); ok || err != nil {
		t.Errorf("ExcludeRead(unreachable) = (%v, %v), want (false, nil)", ok, err)
	}
	// Second denial of the same group is a no-op.
	if ok, _ := s.ExcludeRead(ceo, dev.ID, "t"); !ok {
		t.Fatal("first ExcludeRead(dev) should succeed")
	}
	if ok, err := s.ExcludeRead(ceo, dev.ID, "t"); ok || err != nil {
		t.Errorf("re-ExcludeRead(dev) = (%v, %v), want (false, nil)", ok, err)
	}
	// Unknown group ref is an error, not a silent false.
	if _, err := s.ExcludeRead(ceo, "no-such-group", "t"); err == nil {
		t.Error("ExcludeRead(unknown group) = nil error, want ErrGroupNotFound")
	}
}

// TestGrantClearsReadExclusion — an explicit grant is a deliberate yes and must
// wipe the carve-out; otherwise the group would vanish again the moment the
// direct row is revoked.
func TestGrantClearsReadExclusion(t *testing.T) {
	s, hq, dev, _ := testTree(t)
	const ceo = "ceo@corp.com"
	mustGrant(t, s, ceo, hq.ID, RoleWrite)
	if ok, _ := s.ExcludeRead(ceo, dev.ID, "t"); !ok {
		t.Fatal("ExcludeRead(dev) should succeed")
	}
	mustGrant(t, s, ceo, dev.ID, RoleRead) // add the removed team back explicitly

	scope := s.RecallScope(ceo)
	if !slices.Contains(scope, dev.ID) {
		t.Errorf("RecallScope = %v, want dev back after grant", scope)
	}
	// Revoking the grant must fall back to plain inherited read, not to removed.
	if _, err := s.RevokeDirectGrant(ceo, dev.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.RecallScope(ceo), dev.ID) {
		t.Errorf("after revoke RecallScope = %v, want dev inherited again (denial was cleared)", s.RecallScope(ceo))
	}
}
