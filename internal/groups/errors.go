package groups

import "fmt"

type ErrGroupNotFound struct{ Ref string }

func (e ErrGroupNotFound) Error() string {
	return fmt.Sprintf("group '%s' does not exist", e.Ref)
}

type ErrDuplicateName struct{ Name string }

func (e ErrDuplicateName) Error() string {
	return fmt.Sprintf("group name '%s' already exists under the same parent", e.Name)
}

// ErrAmbiguousName is returned when a group is referenced by a display name
// that more than one group now shares. Names are unique only among siblings
// (plan §6 — "형제 팀 내 동일 이름"), so a bare name can be ambiguous across
// different parents; callers should reference by the immutable ID instead.
type ErrAmbiguousName struct {
	Name  string
	Count int
}

func (e ErrAmbiguousName) Error() string {
	return fmt.Sprintf("group name '%s' is ambiguous: %d groups share it — reference by id", e.Name, e.Count)
}

// ErrCycle is returned when the parent chain of a group loops back on
// itself, or exceeds the maximum tree depth. On load this fails startup.
type ErrCycle struct{ GroupID string }

func (e ErrCycle) Error() string {
	return fmt.Sprintf("group tree invalid: cycle or depth > %d at group '%s'", MaxTreeDepth, e.GroupID)
}

// ErrNotAdmin: grant/revoke is reserved for the organization admin
// (Owner) — a single org-wide identity, not a per-group grade
// (plan §5 grant rule, §6-D8).
type ErrNotAdmin struct {
	Actor string
}

func (e ErrNotAdmin) Error() string {
	return fmt.Sprintf("grant denied: '%s' is not the organization admin (Owner)", e.Actor)
}

// Capture-tag errors (plan §5, §6-D6). Capture requires a write role and
// tags only the author's DIRECT groups — inherited descendants are never
// tag candidates (the §0 top-priority invariant: superior memory must not
// leak downward).

// ErrNoWriteGroup: the caller belongs to no group with role >= write, so
// there is nothing they may capture into.
type ErrNoWriteGroup struct{ User string }

func (e ErrNoWriteGroup) Error() string {
	return fmt.Sprintf("capture denied: '%s' has no group with write role (write required to capture)", e.User)
}

// ErrNotDirectMember: the caller asked to tag a group they only reach by
// inheritance (or not at all). Only direct memberships may be tag targets.
type ErrNotDirectMember struct {
	User    string
	GroupID string
}

func (e ErrNotDirectMember) Error() string {
	return fmt.Sprintf("capture denied: '%s' is not a direct member of group '%s' (inherited descendants cannot be tagged)", e.User, e.GroupID)
}

// ErrInsufficientRole: the caller is a direct member of the group but with
// a role below the one the action needs.
type ErrInsufficientRole struct {
	User    string
	GroupID string
	Have    Role
	Need    Role
}

func (e ErrInsufficientRole) Error() string {
	return fmt.Sprintf("permission denied: '%s' has role '%s' on group '%s' (need '%s')", e.User, string(e.Have), e.GroupID, string(e.Need))
}

// Delete guards (plan §6-D7): a group is deletable only when it has no
// child groups, no memberships, and no sole-tag records.

type ErrHasChildren struct {
	GroupID  string
	Children int
}

func (e ErrHasChildren) Error() string {
	return fmt.Sprintf("cannot delete group '%s': %d child group(s) exist", e.GroupID, e.Children)
}

type ErrHasMembers struct {
	GroupID string
	Members int
}

func (e ErrHasMembers) Error() string {
	return fmt.Sprintf("cannot delete group '%s': %d membership(s) exist", e.GroupID, e.Members)
}

type ErrSoleTagRecords struct {
	GroupID string
	Count   int
}

func (e ErrSoleTagRecords) Error() string {
	return fmt.Sprintf("cannot delete group '%s': %d record(s) have it as their only tag", e.GroupID, e.Count)
}

// ErrTagStatsUnavailable is the fail-closed branch of delete guard (c):
// when the tag counts cannot be obtained (no provider wired, or the
// runespace call failed) deletion is refused — "cannot verify" is not
// "verified empty" (plan §6-D7).
type ErrTagStatsUnavailable struct {
	GroupID string
	Cause   error
}

func (e ErrTagStatsUnavailable) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cannot delete group '%s': tag stats unavailable (%v); refusing fail-closed", e.GroupID, e.Cause)
	}
	return fmt.Sprintf("cannot delete group '%s': tag stats unavailable; refusing fail-closed", e.GroupID)
}
