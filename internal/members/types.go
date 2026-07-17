// Package members owns the member registry: the human-identity records the
// invite flow (internal/invites) and the group RBAC store (internal/groups)
// both hang off. Design-decisions §6.6 defines the member entity. Rows live
// in the unified store database (internal/storedb schema) behind an
// in-memory read cache: reads are RLock map lookups, and every mutation
// commits its SQLite transaction before the cache changes (write-through).
// The legacy YAML file is consumed once by internal/storedb/yamlimport,
// whose members format contract is LoadFromFile.
package members

// Member is one human identity. ID is an immutable UUIDv4 assigned at
// registration — group memberships are keyed by it; Email is the immutable,
// unique join key, fixed at Add() time (tokens are keyed by the address, and
// the dataplane resolves it to the ID per request, so it cannot be changed on
// the member row alone); Status tracks the invite lifecycle.
type Member struct {
	ID          string `yaml:"id" json:"id"`                     // immutable UUIDv4
	Email       string `yaml:"email" json:"email"`               // unique, immutable (set at Add)
	DisplayName string `yaml:"display_name" json:"display_name"` // free text
	Status      string `yaml:"status" json:"status"`             // registered|invited|active|disabled
	// DisabledFrom is the status the member held when it was disabled; empty
	// unless Status is "disabled" (and empty on legacy rows disabled before
	// this field existed). Restore may only return the member to exactly this
	// status, so a disable/restore round-trip can never advance the lifecycle.
	DisabledFrom string `yaml:"disabled_from,omitempty" json:"disabled_from,omitempty"`
	CreatedAt    string `yaml:"created_at" json:"created_at"` // canonical storedb.TimeFormat (RFC3339 UTC ms)
	// SessionExpiredAt is the moment the member's console session token was
	// explicitly destroyed (console [세션 비활성화] / DELETE /users/{id}/session).
	// Empty unless a deactivation happened. Natural token expiry is NOT recorded
	// here — the console derives that from the token's own Expires.
	SessionExpiredAt string `yaml:"session_expired_at,omitempty" json:"session_expired_at,omitempty"` // canonical storedb.TimeFormat (RFC3339 UTC ms)
}

// Member status enum and lifecycle: registered → invited → active.
// A freshly registered member is "registered" — created and (in the atomic
// register+grant flow) granted a group role, but with no invite envelope
// issued yet. Issuing the invite envelope advances it to "invited"
// (MarkInvited); accepting that invite advances it to "active" (Activate).
// "disabled" is a soft delete that keeps the row (and its email reservation).
// Disabling cuts access by revoking the member's token immediately, and the
// dataplane additionally denies any token that resolves to a disabled member;
// group grants are KEPT so a restore preserves memberships. Restore returns
// the member to the exact status it was disabled from (DisabledFrom), never
// further along the lifecycle, and does NOT restore tokens — the disable
// revoked them, so after restoring a previously-active member the admin
// issues a fresh token via the CLI (rotate/issue). Seat counting starts at
// "invited" (design-decisions §8.3).
const (
	StatusRegistered = "registered"
	StatusInvited    = "invited"
	StatusActive     = "active"
	StatusDisabled   = "disabled"
)

// validStatus reports whether s is one of the known member statuses.
func validStatus(s string) bool {
	switch s {
	case StatusRegistered, StatusInvited, StatusActive, StatusDisabled:
		return true
	default:
		return false
	}
}

// allowedStatusTransition encodes the Update status rules: any state may be
// soft-deleted (→disabled); a disabled member may only be RESTORED to the
// status it was disabled from (m.DisabledFrom — restore-to-prior-status); a
// no-op (from==to) is allowed. The forward lifecycle hops are deliberately NOT
// allowed here: registered→invited is owned by MarkInvited (the invite-issue
// hook) and invited→active by Activate (the invite-accept hook), so a PATCH
// can neither silently advance a member who was never invited or never
// accepted, nor launder that advancement through a disable/restore round-trip
// (registered→disabled→active is rejected; invited→active is Activate
// business only).
func (m *Member) allowedStatusTransition(to string) bool {
	switch {
	case m.Status == to:
		return true
	case to == StatusDisabled:
		return true
	case m.Status == StatusDisabled:
		return to == m.restoreStatus()
	default:
		return false
	}
}

// restoreStatus is the only status a disabled member may be restored to: the
// one it held when disabled. Legacy rows disabled before DisabledFrom existed
// carry an empty marker and restore to "registered" — the lifecycle entry
// state — so a restore can never grant more than a fresh registration.
func (m *Member) restoreStatus() string {
	if m.DisabledFrom == "" {
		return StatusRegistered
	}
	return m.DisabledFrom
}
