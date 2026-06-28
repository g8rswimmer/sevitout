package auth

import (
	"context"

	"github.com/g8rswimmer/sevitout/internal/store"
)

type contextKey int

const userContextKey contextKey = iota

// UserContext holds the authenticated user attached to a request context.
type UserContext struct {
	UserID  string
	Email   string
	OrgRole store.OrgRole
}

// WithUser returns a copy of ctx with uc attached.
func WithUser(ctx context.Context, uc *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey, uc)
}

// UserFromContext retrieves the authenticated user from ctx. The second
// return value is false when no user has been attached.
func UserFromContext(ctx context.Context) (*UserContext, bool) {
	uc, ok := ctx.Value(userContextKey).(*UserContext)
	return uc, ok && uc != nil
}
