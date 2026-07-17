package groups

// Group is one node of the single group tree. ID is an immutable UUID:
// it is the opaque filter tag value that leaves the console (plan §6-D5),
// so renames and tree moves never touch stored records.
type Group struct {
	ID        string `yaml:"id" json:"id"`
	Name      string `yaml:"name" json:"name"`
	ParentID  string `yaml:"parent_id,omitempty" json:"parent_id,omitempty"`
	CreatedAt string `yaml:"created_at" json:"created_at"` // canonical storedb.TimeFormat (RFC3339 UTC ms)
}

// Membership grants user a role on one group; downward inheritance makes
// the same role effective on the group's recursive descendants (plan §5).
// A user may hold any number of memberships (user:group = N:M).
type Membership struct {
	User      string `yaml:"user" json:"user"`
	GroupID   string `yaml:"group_id" json:"group_id"`
	Role      Role   `yaml:"role" json:"role"`
	GrantedBy string `yaml:"granted_by" json:"granted_by"`
	GrantedAt string `yaml:"granted_at" json:"granted_at"` // canonical storedb.TimeFormat (RFC3339 UTC ms)
}

// ReadExclusion records that a team was REMOVED from a user's team list when
// their read came from downward inheritance (plan §5) rather than from a stored
// grant. The member drawer (wireframe SC-13) lists direct and child teams in one
// team table and offers removal on every row; a direct row is removed by deleting
// its Membership, but an inherited row has no row to delete, so the removal is
// recorded here instead.
//
// It is not a membership and never appears in the membership map: it carries no
// role and grants nothing. It only subtracts ONE group from the user's inherited
// read, so the group leaves their recall scope and the console stops listing it.
//
// The subtree is never touched: wireframe C10 removed the cascade rule outright
// (child-team memberships are no longer auto-removed; SC-14 lists the remaining
// child memberships as information only). Child teams are separate memory units
// (a team maps 1:1 to its tag) and stay inherited until removed on their own row.
//
// An exclusion applies only where the user has NO direct membership: the drawer's
// add-team action grants the team explicitly, which is a deliberate yes, so Grant
// clears the exclusion — that is also the documented way back after a removal.
type ReadExclusion struct {
	User      string `yaml:"user" json:"user"`
	GroupID   string `yaml:"group_id" json:"group_id"`
	RemovedBy string `yaml:"removed_by" json:"removed_by"`
	RemovedAt string `yaml:"removed_at" json:"removed_at"` // canonical storedb.TimeFormat (RFC3339 UTC ms)
}

// GroupInfo is the list/RPC projection of a Group plus its absolute
// depth in the tree (root = 1).
type GroupInfo struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	ParentID  string `json:"parent_id,omitempty" yaml:"parent_id,omitempty"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
	Depth     int    `json:"depth" yaml:"depth"`
}

// GroupDepth is one row of a subtree walk: Depth is relative to the
// walk root (root itself = 0).
type GroupDepth struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Depth int    `json:"depth"`
}

// MembershipView is one direct (group, role) binding, name-resolved, for
// the GetPermissions projection (plan §6-D8 D8, requirement 4).
type MembershipView struct {
	GroupID   string
	GroupName string
	Role      Role
}

// TreeNode is one group the caller can reach by effective role, annotated
// with tree depth (0 = root) and the caller's effective role there
// (plan §5 recall scope, requirement 12).
type TreeNode struct {
	GroupID       string
	Name          string
	ParentID      string
	Depth         int
	EffectiveRole Role
}

// MemberRoleView is one org-wide (user, group, role) row for the admin-only
// member-roles listing (requirement 8, Q10).
type MemberRoleView struct {
	User      string
	GroupID   string
	GroupName string
	Role      Role
}

// PermissionsView is the whole GetPermissions projection (minus member_roles,
// which the handler adds only for the org admin).
type PermissionsView struct {
	User        string
	Memberships []MembershipView
	Tree        []TreeNode
}

// TagStat is the per-tag summary the delete guard consumes (plan §6-D7):
// Total counts records carrying the tag, Sole counts records where the
// tag is the record's only tag.
type TagStat struct {
	Total int
	Sole  int
}

// TagStatsProvider abstracts the runespace GetTagStats call. M1 injects
// nil (no engine wired yet); DeleteCheck treats nil or a failed call as
// fail-closed and refuses deletion. M2b wires the real engine.
type TagStatsProvider interface {
	GetTagStats(tags []string) (map[string]TagStat, error)
}
