package invites

import "fmt"

// Typed error values, matched by callers with errors.As (same convention as
// internal/groups, internal/tokens, internal/members).

// ErrInviteNotFound is returned when a handle or lease id does not resolve.
type ErrInviteNotFound struct{ Ref string }

func (e ErrInviteNotFound) Error() string {
	return fmt.Sprintf("invite '%s' does not exist", e.Ref)
}

// ErrInviteConsumed is returned when an already-consumed invite is looked up
// or unwrapped again (the one-time guarantee).
type ErrInviteConsumed struct{ Handle string }

func (e ErrInviteConsumed) Error() string {
	return fmt.Sprintf("invite '%s' has already been used", e.Handle)
}

// ErrInviteExpired is returned when an invite is past its ExpiresAt, or has
// been administratively revoked.
type ErrInviteExpired struct{ Handle string }

func (e ErrInviteExpired) Error() string {
	return fmt.Sprintf("invite '%s' has expired", e.Handle)
}

// ErrInviteCompromised is returned when an invite whose lease was reported
// compromised is looked up or unwrapped.
type ErrInviteCompromised struct{ Handle string }

func (e ErrInviteCompromised) Error() string {
	return fmt.Sprintf("invite '%s' has been flagged compromised", e.Handle)
}

// ErrCreationPathMismatch is returned when a Lookup presents a creation path
// that does not match the one the invite was wrapped under (§8.3 binding).
type ErrCreationPathMismatch struct{}

func (ErrCreationPathMismatch) Error() string {
	return "invite creation path does not match"
}

// ErrNotConsumed is returned by ReportCompromise when the target lease is not
// in the consumed state. A pending invite cannot be reported compromised —
// that would let an attacker force endless re-issues (design-decisions DoS-A).
type ErrNotConsumed struct{ LeaseID string }

func (e ErrNotConsumed) Error() string {
	return fmt.Sprintf("lease '%s' is not consumed; only a consumed invite can be reported compromised", e.LeaseID)
}
