package groups

import (
	"fmt"
	"sort"
)

// This file is the single RBAC judge (plan §6-D3): every effective-role,
// scope, grant, top_k, and delete-guard decision is computed here and
// nowhere else. All functions are pure over the in-memory state — no I/O.
// The decision rules are plan §5, quoted per function.

// EffectiveRole computes the effective role of user on the group
// (plan §5: max over memberships (g, r) where the target group is g
// itself or a recursive descendant of g — i.e. g is an ancestor-or-self
// of the target; inheritance flows downward only). The boolean is false
// when the user has no permission on the group or the group is unknown.
// groupRef may be a group ID or unique name.
func (s *Store) EffectiveRole(user, groupRef string) (Role, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return "", false
	}
	return s.effectiveRoleLocked(user, g.ID)
}

func (s *Store) effectiveRoleLocked(user, groupID string) (Role, bool) {
	byGroup := s.memberships[user]
	if len(byGroup) == 0 {
		return "", false
	}
	// A removed inherited read cancels what the ancestor walk below would
	// otherwise find, so the group reports no permission at all. Only the
	// excluded group itself is cut; a direct membership overrides.
	if _, direct := byGroup[groupID]; !direct && s.isExcludedLocked(user, groupID) {
		return "", false
	}
	var best Role
	visited := make(map[string]bool, MaxTreeDepth)
	cur := groupID
	for cur != "" && !visited[cur] && len(visited) < MaxTreeDepth {
		visited[cur] = true
		if m, ok := byGroup[cur]; ok {
			best = maxRole(best, m.Role)
		}
		g, ok := s.groups[cur]
		if !ok {
			break
		}
		cur = g.ParentID
	}
	if best.Rank() == 0 {
		return "", false
	}
	return best, true
}

// RecallScope returns the sorted set of group IDs the user may recall
// (plan §5: union over all memberships g of {g} ∪ recursive descendants
// of g — read is the lowest role, so every membership contributes).
// Recomputed per call by design: revocation takes effect on the next
// request (plan §5 no-cache constraint).
func (s *Store) RecallScope(user string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subtreeUnionLocked(user, RoleRead)
}

// GroupAccessView is a user's consistent group-access snapshot for the
// console projection: the Direct (explicit) memberships and the group IDs
// the user inherits READ on (Inherited). The two sets are always disjoint —
// a group is direct or inherited, never both — because both are captured
// under a single read lock.
type GroupAccessView struct {
	// Direct is the user's explicit (user, group, role) memberships.
	Direct []Membership
	// Inherited is the sorted group IDs the user reads purely by downward
	// inheritance (descendants of a direct group, minus the direct groups,
	// minus any removed by a ReadExclusion — an excluded group is gone from
	// both this list and the user's recall scope).
	// Role is implicitly read; write or higher requires an explicit grant.
	Inherited []string
}

// GroupAccess returns one user's direct memberships and inherited-read group
// IDs from a single consistent snapshot — see GroupAccessView. This is the
// console-display counterpart to RecallScope: per the console policy a parent
// membership confers read (and only read) on every descendant group; write or
// higher must be granted explicitly, which creates a direct membership that
// moves the group out of the inherited set. The ancestor's actual role does
// not flow down here (unlike EffectiveRole). Recomputed per call (no cache):
// a grant or revoke takes effect on the next request.
func (s *Store) GroupAccess(user string) GroupAccessView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.groupAccessLocked(user, s.memberships[user])
}

// GroupAccessByUser returns every user's GroupAccess, all captured under one
// read lock. The console user list uses this so each row's direct and
// inherited sets come from the same snapshot (no torn read across a
// concurrent grant/revoke) without one lock acquisition per member.
func (s *Store) GroupAccessByUser() map[string]GroupAccessView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]GroupAccessView, len(s.memberships))
	for user, byGroup := range s.memberships {
		out[user] = s.groupAccessLocked(user, byGroup)
	}
	return out
}

// groupAccessLocked builds a GroupAccessView from one user's membership map.
// The user key is needed on top of byGroup to apply their inherited-read
// read exclusions. Caller must hold s.mu (read or write).
func (s *Store) groupAccessLocked(user string, byGroup map[string]*Membership) GroupAccessView {
	direct := make([]Membership, 0, len(byGroup))
	reach := make(map[string]bool)
	for gid, m := range byGroup {
		direct = append(direct, *m)
		if _, ok := s.groups[gid]; ok {
			s.collectSubtreeLocked(gid, reach)
		}
	}
	// The directly-held groups are explicit memberships, not inherited.
	for gid := range byGroup {
		delete(reach, gid)
	}
	// A removed inherited read is no longer inherited, so the console stops
	// listing it — matching subtreeUnionLocked, which drops it from the recall
	// scope. Exclusions on directly-held groups are inert (removed just above).
	for gid := range s.excluded[user] {
		delete(reach, gid)
	}
	inherited := make([]string, 0, len(reach))
	for gid := range reach {
		inherited = append(inherited, gid)
	}
	sort.Strings(inherited)
	return GroupAccessView{Direct: direct, Inherited: inherited}
}

// CaptureTargets returns the sorted group IDs a user MAY tag on capture — the
// candidate universe for explicit selection (the future rune-mcp multi-select):
// the user's DIRECT groups whose role is at least write. Inherited descendant
// groups are excluded on purpose — the §0-critical invariant: capture tags only
// the author's own (possibly higher) group, so a superior's memory does not leak
// downward into a subordinate group's recall scope.
//
// NOTE this is the full candidate set, NOT what automatic capture tags: auto
// capture (CaptureTagSet with no explicit request) narrows further to the
// TOP-MOST of these groups — see topMostDirectWriteLocked.
//
// Contrast RecallScope, which DOES expand downward: reading inherits to
// descendants, writing does not broadcast to them.
func (s *Store) CaptureTargets(user string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.directWriteSetLocked(user)
	out := make([]string, 0, len(set))
	for gid := range set {
		out = append(out, gid)
	}
	sort.Strings(out)
	return out
}

// CaptureTagSet resolves the group tags to apply on capture (plan §5
// "capture 태그 대상", §6-D6). It is the write gate and the tag-selection
// judge in one call:
//
//   - requested non-empty: every ref must be a group the user DIRECTLY
//     belongs to with role >= write. Inherited descendants are rejected
//     (ErrNotDirectMember) — the §0 invariant. Returns the resolved
//     immutable group IDs (the opaque tags), de-duplicated and sorted.
//   - requested empty: returns the user's TOP-MOST direct write groups — the
//     default taken by automatic capture, which runs with no per-item selection
//     and so must not broadcast a memory below the author's own standing. See
//     topMostDirectWriteLocked for the rule (top of each membership chain;
//     unrelated branches each keep their own top, so two tops may differ in
//     depth).
//
// A read-only user (no direct write group) is rejected with ErrNoWriteGroup
// in both modes — this is the Q1 "read role capture" denial.
func (s *Store) CaptureTagSet(user string, requested []string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(requested) > 0 {
		byGroup := s.memberships[user]
		out := make([]string, 0, len(requested))
		seen := make(map[string]bool, len(requested))
		for _, ref := range requested {
			g, err := s.resolveLocked(ref)
			if err != nil {
				return nil, err
			}
			m, ok := byGroup[g.ID]
			if !ok {
				return nil, ErrNotDirectMember{User: user, GroupID: g.ID}
			}
			if !m.Role.AtLeast(RoleWrite) {
				return nil, ErrInsufficientRole{User: user, GroupID: g.ID, Have: m.Role, Need: RoleWrite}
			}
			if !seen[g.ID] {
				seen[g.ID] = true
				out = append(out, g.ID)
			}
		}
		sort.Strings(out)
		return out, nil
	}

	// Default (automatic capture): the user's TOP-MOST direct write groups.
	// Auto capture carries no per-item group selection, so it must not push a
	// memory wider than the author's own standing — tagging every direct write
	// group would broadcast it down into every subordinate group's recall
	// scope. Restricting to the top of each membership chain keeps an
	// auto-captured memory at the highest level the author holds (§0 anti-leak,
	// extended). The explicit branch above still lets a deliberate caller tag
	// any direct write group, including a lower one (the future multi-select).
	out := s.topMostDirectWriteLocked(user)
	if len(out) == 0 {
		return nil, ErrNoWriteGroup{User: user}
	}
	return out, nil
}

// directWriteSetLocked returns the set of group IDs the user DIRECTLY belongs to
// with role >= write and that still exist — the single definition of the
// "capturable" set. CaptureTargets sorts it into the candidate listing, and
// automatic capture (topMostDirectWriteLocked) narrows it to the top of each
// membership chain. Returned as a set because the top-most walk needs O(1)
// ancestor lookups; the slice consumer converts it cheaply. Caller holds s.mu.
func (s *Store) directWriteSetLocked(user string) map[string]bool {
	set := make(map[string]bool)
	for gid, m := range s.memberships[user] {
		if !m.Role.AtLeast(RoleWrite) {
			continue
		}
		if _, ok := s.groups[gid]; !ok {
			continue
		}
		set[gid] = true
	}
	return set
}

// topMostDirectWriteLocked returns the sorted group IDs automatic capture tags:
// the user's direct write+ groups, minus any that sit UNDER another of the
// user's direct write+ groups. A group is kept unless one of its proper
// ancestors is also a direct write+ group, so each membership chain contributes
// exactly its top and unrelated branches each keep their own top — two tops need
// not share a depth. This is the §0 anti-leak invariant applied to the author's
// own groups: an auto-captured memory never lands below the author's highest
// standing in a chain. Caller holds s.mu.
func (s *Store) topMostDirectWriteLocked(user string) []string {
	write := s.directWriteSetLocked(user)
	out := make([]string, 0, len(write))
	for gid := range write {
		if !s.hasWriteAncestorLocked(gid, write) {
			out = append(out, gid)
		}
	}
	sort.Strings(out)
	return out
}

// hasWriteAncestorLocked reports whether any PROPER ancestor of gid is in the
// write set (the user's direct write+ groups). Walks the parent chain with a
// visited set capped at MaxTreeDepth, matching every other ancestor walk here.
// Caller holds s.mu.
func (s *Store) hasWriteAncestorLocked(gid string, write map[string]bool) bool {
	g, ok := s.groups[gid]
	if !ok {
		return false
	}
	visited := make(map[string]bool, MaxTreeDepth)
	cur := g.ParentID
	for cur != "" && !visited[cur] && len(visited) < MaxTreeDepth {
		if write[cur] {
			return true
		}
		visited[cur] = true
		p, ok := s.groups[cur]
		if !ok {
			break
		}
		cur = p.ParentID
	}
	return false
}

// Permissions builds the GetPermissions projection for user (plan §6-D8 D8,
// requirements 4/12): the caller's direct memberships plus the group tree
// they can reach by effective role (recall scope), each node depth-annotated
// (0 = root of the tree) with the caller's effective role there. rootRef ""
// returns the whole reachable set; a non-empty rootRef restricts the tree to
// that group's subtree (still intersected with what the caller may reach).
// A rootRef that does not resolve is NOT an error: it yields the same view
// as a group the caller cannot reach (direct memberships + empty tree), so
// the response never oracles which group ids/names exist org-wide.
// The per-user member-roles listing is NOT built here — the handler adds it
// only for the organization admin.
func (s *Store) Permissions(user, rootRef string) (PermissionsView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var restrict map[string]bool
	if rootRef != "" {
		// Resolve failure is deliberately swallowed: surfacing it would make
		// nonexistent refs distinguishable from existing-but-unreachable ones,
		// letting any valid token enumerate groups (resolveLocked also matches
		// display names). restrict stays an allocated EMPTY set so the tree
		// intersects to empty — a nil restrict would mean "no restriction"
		// and leak the caller's whole reachable set instead.
		restrict = make(map[string]bool)
		if g, err := s.resolveLocked(rootRef); err == nil {
			s.collectSubtreeLocked(g.ID, restrict)
		}
	}

	view := PermissionsView{User: user}

	// Direct memberships, name-resolved, sorted by group name.
	for gid, m := range s.memberships[user] {
		g, ok := s.groups[gid]
		if !ok {
			continue
		}
		view.Memberships = append(view.Memberships, MembershipView{
			GroupID: gid, GroupName: g.Name, Role: m.Role,
		})
	}
	sort.Slice(view.Memberships, func(i, j int) bool {
		return view.Memberships[i].GroupName < view.Memberships[j].GroupName
	})

	// Reachable tree = recall scope (memberships ∪ recursive descendants),
	// optionally intersected with the rootRef subtree.
	for _, gid := range s.subtreeUnionLocked(user, RoleRead) {
		if restrict != nil && !restrict[gid] {
			continue
		}
		g := s.groups[gid]
		if g == nil {
			continue
		}
		eff, _ := s.effectiveRoleLocked(user, gid)
		d, err := s.depthLocked(gid)
		if err != nil {
			return PermissionsView{}, err
		}
		view.Tree = append(view.Tree, TreeNode{
			GroupID: gid, Name: g.Name, ParentID: g.ParentID,
			Depth: d - 1, EffectiveRole: eff, // depthLocked roots at 1; expose 0-based
		})
	}
	sort.Slice(view.Tree, func(i, j int) bool {
		if view.Tree[i].Depth != view.Tree[j].Depth {
			return view.Tree[i].Depth < view.Tree[j].Depth
		}
		return view.Tree[i].Name < view.Tree[j].Name
	})

	return view, nil
}

// MemberRoles returns the org-wide (user, group, role) listing for the
// admin-only member-roles view (requirement 8, Q10 "서브트리 제한 없음").
// Sorted by user, then group name.
func (s *Store) MemberRoles() []MemberRoleView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MemberRoleView, 0)
	for user, byGroup := range s.memberships {
		for gid, m := range byGroup {
			name := gid
			if g, ok := s.groups[gid]; ok {
				name = g.Name
			}
			out = append(out, MemberRoleView{User: user, GroupID: gid, GroupName: name, Role: m.Role})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].User != out[j].User {
			return out[i].User < out[j].User
		}
		return out[i].GroupName < out[j].GroupName
	})
	return out
}

// subtreeUnionLocked unions {g} ∪ descendants(g) over the user's
// memberships whose role is at least min. Used by RecallScope (downward
// inheritance); capture targets deliberately do NOT use this (§6-D6).
func (s *Store) subtreeUnionLocked(user string, min Role) []string {
	set := make(map[string]bool)
	for gid, m := range s.memberships[user] {
		if !m.Role.AtLeast(min) {
			continue
		}
		if _, ok := s.groups[gid]; !ok {
			continue
		}
		s.collectSubtreeLocked(gid, set)
	}
	// Subtract the removed inherited reads. This is what makes a removal real
	// rather than cosmetic: RecallScope is built from this set, so an excluded
	// group leaves the user's recall scope and its memory stops being readable.
	// The group's descendants stay (wireframe C10: removal does not cascade),
	// each being its own memory unit and its own removal decision. A directly
	// held group is never subtracted: an explicit grant outranks an exclusion.
	for gid := range s.excluded[user] {
		if _, direct := s.memberships[user][gid]; direct {
			continue
		}
		delete(set, gid)
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Store) collectSubtreeLocked(id string, set map[string]bool) {
	if set[id] {
		return
	}
	set[id] = true
	for _, c := range s.children[id] {
		s.collectSubtreeLocked(c, set)
	}
}

// CanGrant judges grant(actor → targetUser, group, role) — plan §5:
// allowed only when actor is the organization admin (Owner). Because the
// admin is a single org-wide identity (not a per-group grade), there is
// no self-promotion path to guard — the admin is the only grantor, and
// no group role can raise anyone (plan §6-D8). This is the layer-2 judge:
// the local admin CLI does not run it (operator surface, full power +
// audit); the future authenticated mutation RPC will.
//
// The group and role are still validated so a malformed grant is rejected
// with a precise error even from the admin.
func (s *Store) CanGrant(actor, targetUser, groupRef string, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("invalid group role %q (expected read|write|edit)", string(role))
	}
	if err := s.validatePersonKey(targetUser); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.resolveLocked(groupRef); err != nil {
		return err
	}
	if actor == "" || s.orgAdmin != actor {
		return ErrNotAdmin{Actor: actor}
	}
	return nil
}

// DescendantsWithDepth returns the subtree rooted at groupRef in DFS
// order: the group itself at depth 0, descendants at their relative
// depth (requirement 12: depth-annotated subtree listing).
func (s *Store) DescendantsWithDepth(groupRef string) ([]GroupDepth, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return nil, err
	}
	out := make([]GroupDepth, 0)
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		out = append(out, GroupDepth{ID: id, Name: s.groups[id].Name, Depth: depth})
		kids := append([]string(nil), s.children[id]...)
		s.sortByNameLocked(kids)
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	walk(g.ID, 0)
	return out, nil
}

// DeleteCheck runs the triple delete guard (plan §6-D7) without
// deleting: (a) no child groups, (b) no memberships, (c) no records
// carrying the group's tag as their only tag. Guard (c) is fail-closed:
// a nil provider or a failed call refuses deletion — "cannot verify"
// is not "verified empty".
func (s *Store) DeleteCheck(groupRef string, stats TagStatsProvider) error {
	s.mu.RLock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		s.mu.RUnlock()
		return err
	}
	id := g.ID
	err = s.deleteCheckMembershipTreeLocked(id)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	// GetTagStats is a remote call — run it outside the store lock.
	return soleTagGuard(id, stats)
}

func (s *Store) deleteCheckMembershipTreeLocked(id string) error {
	if n := len(s.children[id]); n > 0 {
		return ErrHasChildren{GroupID: id, Children: n}
	}
	members := 0
	for _, byGroup := range s.memberships {
		if _, ok := byGroup[id]; ok {
			members++
		}
	}
	if members > 0 {
		return ErrHasMembers{GroupID: id, Members: members}
	}
	return nil
}

func soleTagGuard(id string, stats TagStatsProvider) error {
	if stats == nil {
		return ErrTagStatsUnavailable{GroupID: id}
	}
	m, err := stats.GetTagStats([]string{id})
	if err != nil {
		return ErrTagStatsUnavailable{GroupID: id, Cause: err}
	}
	if st, ok := m[id]; ok && st.Sole > 0 {
		return ErrSoleTagRecords{GroupID: id, Count: st.Sole}
	}
	return nil
}
