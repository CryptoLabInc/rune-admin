package members

import "fmt"

// Typed error values, matched by callers with errors.As (same convention as
// internal/groups and internal/tokens). Value receivers so both the value
// and a pointer satisfy error.

// ErrMemberNotFound is returned when a member id/email does not resolve.
type ErrMemberNotFound struct{ Ref string }

func (e ErrMemberNotFound) Error() string {
	return fmt.Sprintf("member '%s' does not exist", e.Ref)
}

// ErrDuplicateEmail is returned when an add/update would collide with an
// email already registered to another member.
type ErrDuplicateEmail struct{ Email string }

func (e ErrDuplicateEmail) Error() string {
	return fmt.Sprintf("member email '%s' already exists", e.Email)
}

// ErrInvalidEmail is returned when an email fails the person-key check
// (non-empty, a single interior '@', no whitespace).
type ErrInvalidEmail struct{ Email string }

func (e ErrInvalidEmail) Error() string {
	return fmt.Sprintf("invalid member email %q (the member key is an email)", e.Email)
}

// ErrInvalidStatus is returned when a status value is not one of
// registered|invited|active|disabled, or when a requested transition is not
// allowed.
type ErrInvalidStatus struct{ Status string }

func (e ErrInvalidStatus) Error() string {
	return fmt.Sprintf("invalid member status %q (expected registered|invited|active|disabled, and a permitted transition)", e.Status)
}
