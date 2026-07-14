package tokens

import "fmt"

type ErrTokenNotFound struct{}

func (ErrTokenNotFound) Error() string { return "Invalid authentication token" }

type ErrTokenExpired struct {
	User string
}

func (e ErrTokenExpired) Error() string {
	return fmt.Sprintf("Token expired for user '%s'", e.User)
}

type ErrRateLimit struct {
	RetryAfter int
}

func (e ErrRateLimit) Error() string {
	return fmt.Sprintf("Rate limit exceeded. Retry after %ds", e.RetryAfter)
}

type ErrTopKExceeded struct {
	Requested int
	MaxTopK   int
	RoleName  string
}

func (e ErrTopKExceeded) Error() string {
	return fmt.Sprintf("top_k %d exceeds limit %d for role '%s'",
		e.Requested, e.MaxTopK, e.RoleName)
}

type ErrScope struct {
	Method   string
	RoleName string
}

func (e ErrScope) Error() string {
	return fmt.Sprintf("Method '%s' not permitted for role '%s'", e.Method, e.RoleName)
}
