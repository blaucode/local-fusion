package auth

import (
	"context"
	"errors"
)

// ErrUnauthenticated is returned when a request does not contain a valid
// authenticated user in its context.
var ErrUnauthenticated = errors.New("unauthenticated")

// User represents an authenticated principal.  The fields are exported so that
// other packages (e.g., handlers) can safely read them, but the struct itself
// should only be obtained via UserFromContext to avoid data races.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// userKey is an unexported type used as the context key for stored User values.
// Using a distinct type guarantees that values stored by other packages will
// never clash with this key.
type userKey struct{}

// NewContext returns a copy of ctx that carries the supplied user.  It is used
// by the authentication middleware when a JWT is successfully validated.
func NewContext(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFromContext extracts the authenticated User from ctx.
// If the user is missing, the value is of the wrong type, or the stored pointer
// is nil, ErrUnauthenticated is returned.
//
// The returned *User is a shallow copy of the stored value; callers receive a
// distinct instance so that mutation of the result cannot affect the value
// stored in the context (preventing race conditions).
func UserFromContext(ctx context.Context) (*User, error) {
	val := ctx.Value(userKey{})
	if val == nil {
		return nil, ErrUnauthenticated
	}
	u, ok := val.(*User)
	if !ok || u == nil {
		return nil, ErrUnauthenticated
	}
	// Return a copy to avoid race conditions.
	cpy := *u
	return &cpy, nil
}