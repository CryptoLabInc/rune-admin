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
