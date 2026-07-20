package groups

import (
	"context"
	"database/sql"
	"sort"
)

// ConsoleTreeNode is one flat node of the console team tree (GET /teams/tree).
// It enriches GroupInfo with the child-id list and the derived counts the
// console UI needs — childCount for the tree affordance, memberCount for the
// node badge. Counts are computed in a single locked pass so the HTTP handler
// never re-scans memberships per node.
type ConsoleTreeNode struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ParentID    *string  `json:"parentId"` // null for a root team (doc contract)
	ChildrenIDs []string `json:"childrenIds"`
	ChildCount  int      `json:"childCount"`
	MemberCount int      `json:"memberCount"`
}

// ConsoleTeamDetail is the single-team projection (GET /teams/{id}): the node
// plus its direct children ids and derived member count.
type ConsoleTeamDetail struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ParentID    *string  `json:"parentId"` // null for a root team (doc contract)
	Children    []string `json:"children"`
	MemberCount int      `json:"memberCount"`
	CreatedAt   string   `json:"createdAt"`
}

// ConsoleTeamMember is one row source for GET /teams/{id}/members. Direct
// members retain their explicit role and source team. Inherited members are
// projected as read, with SourceGroupID and GrantedAt taken from the nearest
// direct ancestor membership. Keeping this projection in the store lets the
// whole member list come from one consistent read snapshot.
type ConsoleTeamMember struct {
	User          string
	Role          Role
	SourceGroupID string
	GrantedAt     string
	Inherited     bool
}

// parentPtr maps an empty parent id (a root team) to nil so it serializes as
// JSON null, per the design doc; a non-empty parent serializes as the string.
func parentPtr(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// effectiveMemberCountLocked counts the same people exposed by the team-member
// list: direct members plus users who inherit read from a proper ancestor.
// Direct+inherited overlap is counted once, and read exclusions are honored.
// Caller holds s.mu.
func (s *Store) effectiveMemberCountLocked(id string) int {
	n := 0
	for user, byGroup := range s.memberships {
		if _, ok := byGroup[id]; ok {
			n++
			continue
		}
		if _, inherited := s.inheritedSourceLocked(user, id); inherited {
			n++
		}
	}
	return n
}

// effectiveMemberCountsLocked computes effective counts for every team in one
// membership snapshot. groupAccessLocked already produces disjoint direct and
// inherited sets with de-duplication and per-target exclusions applied.
func (s *Store) effectiveMemberCountsLocked() map[string]int {
	counts := make(map[string]int, len(s.groups))
	for user, byGroup := range s.memberships {
		access := s.groupAccessLocked(user, byGroup)
		for _, m := range access.Direct {
			counts[m.GroupID]++
		}
		for _, id := range access.Inherited {
			counts[id]++
		}
	}
	return counts
}

// ConsoleTeamMembers returns all people displayed for groupID — direct plus
// recursively inherited — under one read lock. Unknown groups return nil.
func (s *Store) ConsoleTeamMembers(groupID string) []ConsoleTeamMember {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.groups[groupID]; !ok {
		return nil
	}
	out := make([]ConsoleTeamMember, 0)
	for user, byGroup := range s.memberships {
		if direct, ok := byGroup[groupID]; ok {
			out = append(out, ConsoleTeamMember{
				User:          user,
				Role:          direct.Role,
				SourceGroupID: groupID,
				GrantedAt:     direct.GrantedAt,
			})
			continue
		}
		source, inherited := s.inheritedSourceLocked(user, groupID)
		if !inherited {
			continue
		}
		out = append(out, ConsoleTeamMember{
			User:          user,
			Role:          RoleRead,
			SourceGroupID: source.GroupID,
			GrantedAt:     source.GrantedAt,
			Inherited:     true,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	return out
}

// sortedChildrenLocked returns id's child ids sorted by name — always non-nil
// so it serializes as [] not null. Caller holds s.mu.
func (s *Store) sortedChildrenLocked(id string) []string {
	kids := append([]string{}, s.children[id]...)
	s.sortByNameLocked(kids)
	return kids
}

// ConsoleTree returns every group as a flat node with children ids and derived
// counts, in DFS order (parents before children, siblings by name) — the same
// order as ListGroups. The client assembles the tree from parentId/childrenIds.
func (s *Store) ConsoleTree() []ConsoleTreeNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roots := make([]string, 0)
	for id, g := range s.groups {
		if g.ParentID == "" {
			roots = append(roots, id)
		}
	}
	s.sortByNameLocked(roots)
	memberCounts := s.effectiveMemberCountsLocked()

	out := make([]ConsoleTreeNode, 0, len(s.groups))
	var walk func(id string)
	walk = func(id string) {
		g := s.groups[id]
		kids := s.sortedChildrenLocked(id)
		out = append(out, ConsoleTreeNode{
			ID:          g.ID,
			Name:        g.Name,
			ParentID:    parentPtr(g.ParentID),
			ChildrenIDs: kids,
			ChildCount:  len(kids),
			MemberCount: memberCounts[id],
		})
		for _, c := range kids {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

// TeamDetail returns the single-team projection for ref (id or name).
func (s *Store) TeamDetail(ref string) (ConsoleTeamDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(ref)
	if err != nil {
		return ConsoleTeamDetail{}, err
	}
	return ConsoleTeamDetail{
		ID:          g.ID,
		Name:        g.Name,
		ParentID:    parentPtr(g.ParentID),
		Children:    s.sortedChildrenLocked(g.ID),
		MemberCount: s.effectiveMemberCountLocked(g.ID),
		CreatedAt:   g.CreatedAt,
	}, nil
}

// DirectRole reports the role of a user's DIRECT membership on group ref (no
// inheritance) and whether such a membership exists. Used by the console to
// distinguish "not a member of this team" (NOT_TEAM_MEMBER) from a role change
// on an existing membership, and to detect ALREADY_TEAM_MEMBER on add.
func (s *Store) DirectRole(user, groupRef string) (Role, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return "", false, err
	}
	byGroup, ok := s.memberships[user]
	if !ok {
		return "", false, nil
	}
	m, ok := byGroup[g.ID]
	if !ok {
		return "", false, nil
	}
	return m.Role, true, nil
}

// GroupName resolves ref (id or name) to the group's current name.
func (s *Store) GroupName(ref string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(ref)
	if err != nil {
		return "", err
	}
	return g.Name, nil
}

// DeleteGroupWithMembers deletes a team for the console team-delete flow. Unlike
// DeleteGroup it does NOT run the sole-tag remote guard (the console handles the
// team's memory explicitly via transfer/purge before calling this, and — per
// product decision — team deletion is not gated on that guard), and it removes
// the team's memberships as part of the deletion instead of refusing when
// members exist (the design doc allows deleting a team with members; only child
// teams block). Child teams still block (ErrHasChildren) — the wireframe routes
// that to an alert and never enters the delete flow. Returns the deleted group
// and the person keys whose membership on it was removed. Atomic under the
// write lock AND in the database: memberships and the group are deleted in
// ONE transaction (memberships first, satisfying the ON DELETE RESTRICT
// foreign key), so no crash can strand a membership of a deleted group.
func (s *Store) DeleteGroupWithMembers(ref string) (Group, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	g, err := s.resolveLocked(ref)
	if err != nil {
		return Group{}, nil, err
	}
	id := g.ID
	if n := len(s.children[id]); n > 0 {
		return Group{}, nil, ErrHasChildren{GroupID: id, Children: n}
	}

	// Collect the affected person keys first; the maps are only touched
	// after the transaction commits.
	removed := make([]string, 0)
	for user, byGroup := range s.memberships {
		if _, ok := byGroup[id]; ok {
			removed = append(removed, user)
		}
	}
	sort.Strings(removed)

	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, derr := tx.ExecContext(ctx, `DELETE FROM memberships WHERE group_id = ?`, id)
		if derr != nil {
			return derr
		}
		if err := expectRows(res, int64(len(removed)), "memberships of group "+id); err != nil {
			return err
		}
		res, derr = tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
		if derr != nil {
			return derr
		}
		return expectOneRow(res, "group "+id)
	}); err != nil {
		return Group{}, nil, err
	}

	for _, user := range removed {
		byGroup := s.memberships[user]
		delete(byGroup, id)
		if len(byGroup) == 0 {
			delete(s.memberships, user)
		}
	}
	s.removeFromByNameLocked(g.Name, g.ID)
	delete(s.groups, id)
	delete(s.children, id)
	s.purgeExclusionsForGroupLocked(id)
	if g.ParentID != "" {
		sibs := s.children[g.ParentID]
		for i, c := range sibs {
			if c == id {
				s.children[g.ParentID] = append(sibs[:i], sibs[i+1:]...)
				break
			}
		}
	}
	return *g, removed, nil
}
