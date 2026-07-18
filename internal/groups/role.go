package groups

import "fmt"

// Role is the per-group permission level a user holds within one group.
// Ordered: read < write < edit — a higher role includes every action of
// the lower ones (plan §5).
//
// admin is deliberately NOT a group role: the organization admin (Owner)
// is a single org-wide identity, not a per-group grade (plan §5, §6-D8).
// It is modeled separately on the Store (SetOrgAdmin / IsOrgAdmin) and,
// in this M1 scope, is only used for judgment (CanGrant).
//
// This is also a separate type from tokens.Role: token roles gate
// identity/rate-limit/top_k on the legacy surface, group roles gate the
// group RBAC judge.
type Role string

const (
	RoleRead  Role = "read"
	RoleWrite Role = "write"
	RoleEdit  Role = "edit"
)

var roleRanks = map[Role]int{
	RoleRead:  1,
	RoleWrite: 2,
	RoleEdit:  3,
}

// Rank returns the ordinal position of r (read=1, write=2, edit=3),
// 0 for unknown.
func (r Role) Rank() int { return roleRanks[r] }

// Valid reports whether r is one of the three known group roles.
func (r Role) Valid() bool { return r.Rank() != 0 }

// AtLeast reports whether r includes everything other grants.
// An unknown r is never AtLeast anything.
func (r Role) AtLeast(other Role) bool {
	return r.Rank() != 0 && r.Rank() >= other.Rank()
}

// ParseRole validates a user-supplied role string.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("invalid group role %q (expected read|write|edit)", s)
	}
	return r, nil
}

// maxRole returns the higher-ranked of a and b.
func maxRole(a, b Role) Role {
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}
