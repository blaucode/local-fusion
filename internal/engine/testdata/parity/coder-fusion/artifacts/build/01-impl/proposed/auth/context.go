package auth

import (
	"context"
	"errors"
)

// User represents an authenticated principal.
// Exported fields allow other packages (e.g., handlers) to read the data.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ErrUnauthenticated is returned when a request does not carry a valid
// authenticated user in the context.
var ErrUnauthenticated = errors.New("unauthenticated")

// ctxKey is an unexported type used as the key for storing a *User in a context.
type ctxKey struct{}

// userKey is the singleton key used throughout the service.
var userKey = &ctxKey{}

// NewContextWithUser returns a new context containing the supplied user.
// It is intended for use by authentication middleware and tests.
func NewContextWithUser(parent context.Context, u *User) context.Context {
	return context.WithValue(parent, userKey, u)
}

// UserFromContext extracts the authenticated user from ctx.
// It returns a *copy* of the stored User value so that callers cannot
// mutate the original data residing in the context (avoiding race conditions).
// If no user is present, or the stored value has an unexpected type,
// ErrUnauthenticated is returned.
func UserFromContext(ctx context.Context) (*User, error) {
	val := ctx.Value(userKey)
	if val == nil {
		return nil, ErrUnauthenticated
	}
	u, ok := val.(*User)
	if !ok || u == nil {
		return nil, ErrUnauthenticated
	}
	// Return a shallow copy of the struct (fields are all value types or strings).
	copied := *u
	return &copied, nil
}