package groups

import (
	"errors"
	"testing"
	"time"
)

// findNode returns the console tree node with id, failing the test if absent.
func findNode(t *testing.T, tree []ConsoleTreeNode, id string) ConsoleTreeNode {
	t.Helper()
	for _, n := range tree {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %s not found in tree", id)
	return ConsoleTreeNode{}
}

func TestConsoleTreeCountsAndChildren(t *testing.T) {
	s := NewStore()
	root, err := s.CreateGroup("Platform", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateGroup("Payments", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("a@x.com", root.ID, RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("b@x.com", root.ID, RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("c@x.com", child.ID, RoleWrite, "actor"); err != nil {
		t.Fatal(err)
	}

	tree := s.ConsoleTree()
	if len(tree) != 2 {
		t.Fatalf("tree len = %d, want 2", len(tree))
	}
	rn := findNode(t, tree, root.ID)
	if rn.ChildCount != 1 || len(rn.ChildrenIDs) != 1 || rn.ChildrenIDs[0] != child.ID {
		t.Errorf("root children = %+v (count %d), want [%s]", rn.ChildrenIDs, rn.ChildCount, child.ID)
	}
	if rn.MemberCount != 2 {
		t.Errorf("root memberCount = %d, want 2", rn.MemberCount)
	}
	cn := findNode(t, tree, child.ID)
	if cn.ParentID == nil || *cn.ParentID != root.ID {
		t.Errorf("child parentId = %v, want %s", cn.ParentID, root.ID)
	}
	if rn.ParentID != nil {
		t.Errorf("root parentId = %v, want nil (null)", *rn.ParentID)
	}
	if cn.ChildCount != 0 || cn.MemberCount != 3 {
		t.Errorf("child childCount=%d memberCount=%d, want 0 and 3 effective members", cn.ChildCount, cn.MemberCount)
	}
}

func TestConsoleTeamMembersUseNearestInheritanceSource(t *testing.T) {
	s := NewStore()
	root, _ := s.CreateGroup("Root", "")
	mid, _ := s.CreateGroup("Mid", root.ID)
	leaf, _ := s.CreateGroup("Leaf", mid.ID)

	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return t0 }
	rootOnly, err := s.Grant("root@x.com", root.ID, RoleEdit, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("nearest@x.com", root.ID, RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("excluded@x.com", root.ID, RoleWrite, "actor"); err != nil {
		t.Fatal(err)
	}

	s.now = func() time.Time { return t0.Add(24 * time.Hour) }
	midGrant, err := s.Grant("nearest@x.com", mid.ID, RoleWrite, "actor")
	if err != nil {
		t.Fatal(err)
	}

	s.now = func() time.Time { return t0.Add(48 * time.Hour) }
	direct, err := s.Grant("direct@x.com", leaf.ID, RoleEdit, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if excluded, err := s.ExcludeRead("excluded@x.com", leaf.ID, "actor"); err != nil || !excluded {
		t.Fatalf("ExcludeRead = (%v, %v), want (true, nil)", excluded, err)
	}

	members := s.ConsoleTeamMembers(leaf.ID)
	if len(members) != 3 {
		t.Fatalf("effective members = %+v, want 3", members)
	}
	byUser := make(map[string]ConsoleTeamMember, len(members))
	for _, m := range members {
		byUser[m.User] = m
	}
	if got := byUser["root@x.com"]; !got.Inherited || got.Role != RoleRead || got.SourceGroupID != root.ID || got.GrantedAt != rootOnly.GrantedAt {
		t.Errorf("root-only inherited member = %+v, want root source at %s", got, rootOnly.GrantedAt)
	}
	if got := byUser["nearest@x.com"]; !got.Inherited || got.Role != RoleRead || got.SourceGroupID != mid.ID || got.GrantedAt != midGrant.GrantedAt {
		t.Errorf("multi-ancestor inherited member = %+v, want nearest mid source at %s", got, midGrant.GrantedAt)
	}
	if got := byUser["direct@x.com"]; got.Inherited || got.Role != RoleEdit || got.SourceGroupID != leaf.ID || got.GrantedAt != direct.GrantedAt {
		t.Errorf("direct member = %+v, want leaf direct source at %s", got, direct.GrantedAt)
	}
	if _, found := byUser["excluded@x.com"]; found {
		t.Error("read-excluded inherited member was listed")
	}

	detail, err := s.TeamDetail(leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.MemberCount != len(members) {
		t.Errorf("detail memberCount=%d, list len=%d", detail.MemberCount, len(members))
	}
	if node := findNode(t, s.ConsoleTree(), leaf.ID); node.MemberCount != len(members) {
		t.Errorf("tree memberCount=%d, list len=%d", node.MemberCount, len(members))
	}
}

func TestTeamDetailAndDirectRole(t *testing.T) {
	s := NewStore()
	root, _ := s.CreateGroup("R", "")
	_, _ = s.CreateGroup("C", root.ID)
	if _, err := s.Grant("a@x.com", root.ID, RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}

	d, err := s.TeamDetail(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.MemberCount != 1 || len(d.Children) != 1 {
		t.Errorf("detail memberCount=%d children=%v", d.MemberCount, d.Children)
	}

	role, ok, err := s.DirectRole("a@x.com", root.ID)
	if err != nil || !ok || role != RoleEdit {
		t.Errorf("DirectRole = %q,%v,%v; want edit,true,nil", role, ok, err)
	}
	if _, ok, _ := s.DirectRole("nobody@x.com", root.ID); ok {
		t.Error("DirectRole for non-member returned ok=true")
	}
	if _, _, err := s.DirectRole("a@x.com", "no-such-team"); err == nil {
		t.Error("DirectRole on unknown team should error")
	}
}

func TestDeleteGroupWithMembers(t *testing.T) {
	s := NewStore()
	root, _ := s.CreateGroup("R", "")
	if _, err := s.Grant("a@x.com", root.ID, RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("b@x.com", root.ID, RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}

	g, removed, err := s.DeleteGroupWithMembers(root.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g.ID != root.ID || len(removed) != 2 {
		t.Errorf("removed = %v (group %s), want 2 membership keys", removed, g.ID)
	}
	if _, err := s.ResolveGroup(root.ID); err == nil {
		t.Error("group still resolvable after delete")
	}
}

// TestDeleteGroupWithMembersRemovesRowsAtomically — the SQL twin of
// TestDeleteGroupWithMembers: memberships and the group are deleted in ONE
// transaction (with the ON DELETE RESTRICT foreign key forbidding any other
// order), so after the call the database holds zero rows for the deleted
// team, unrelated memberships survive, and no membership row can ever
// reference a missing group.
func TestDeleteGroupWithMembersRemovesRowsAtomically(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	root, err := s.CreateGroup("R", "")
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.CreateGroup("K", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ user, group string }{
		{"a@x.com", root.ID}, {"b@x.com", root.ID}, {"a@x.com", keep.ID},
	} {
		if _, err := s.Grant(grant.user, grant.group, RoleRead, "actor"); err != nil {
			t.Fatal(err)
		}
	}

	if _, removed, err := s.DeleteGroupWithMembers(root.ID); err != nil || len(removed) != 2 {
		t.Fatalf("DeleteGroupWithMembers = (removed %v, %v), want 2 keys", removed, err)
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := database.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM groups WHERE id = ?`, root.ID); n != 0 {
		t.Errorf("deleted group still has %d row(s)", n)
	}
	if n := count(`SELECT COUNT(*) FROM memberships WHERE group_id = ?`, root.ID); n != 0 {
		t.Errorf("deleted group still has %d membership row(s)", n)
	}
	if n := count(`SELECT COUNT(*) FROM memberships WHERE group_id = ?`, keep.ID); n != 1 {
		t.Errorf("unrelated team's memberships = %d row(s), want 1", n)
	}
	// The orphan class is gone table-wide, not just for this delete.
	if n := count(
		`SELECT COUNT(*) FROM memberships m LEFT JOIN groups g ON m.group_id = g.id WHERE g.id IS NULL`); n != 0 {
		t.Errorf("found %d orphaned membership row(s), want 0", n)
	}
}

func TestDeleteGroupWithMembersBlockedByChildren(t *testing.T) {
	s := NewStore()
	root, _ := s.CreateGroup("R", "")
	_, _ = s.CreateGroup("C", root.ID)

	_, _, err := s.DeleteGroupWithMembers(root.ID)
	if !errors.As(err, new(ErrHasChildren)) {
		t.Fatalf("delete err = %v, want ErrHasChildren", err)
	}
}
