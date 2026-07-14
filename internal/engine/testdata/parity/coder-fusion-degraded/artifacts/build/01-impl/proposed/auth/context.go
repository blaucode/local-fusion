package auth

import (
	"context"
	"errors"
)

// User represents an authenticated user.
// The Name field is exported so other packages can read it.
type User struct {
	ID   string
	Name string
}

// ErrUnauthenticated is returned when no authenticated user is found in the context.
var ErrUnauthenticated = errors.New("unauthenticated")

// contextKey is an unexported type used for storing values in context.
type contextKey struct{}

var userKey = &contextKey{}

// UserFromContext extracts the *User value from the given context.
// It returns a copy of the stored User to avoid race conditions.
//
// If the user value is missing or of the wrong type, ErrUnauthenticated is returned.
func UserFromContext(ctx context.Context) (*User, error) {
	val := ctx.Value(userKey)
	if val == nil {
		return nil, ErrUnauthenticated
	}
	u, ok := val.(*User)
	if !ok || u == nil {
		return nil, ErrUnauthenticated
	}
	// Return a shallow copy (field‑by‑field) to prevent callers from mutating the
	// original struct stored in the context.
	copied := *u
	return &copied, nil
}

// NewContextWithUser returns a new context derived from ctx that carries the provided *User.
// This helper is primarily intended for tests and middleware that need to inject a user.
func NewContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}