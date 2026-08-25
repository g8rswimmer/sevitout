package auth_test

import (
	"context"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

func TestWithUser_UserFromContext_RoundTrip(t *testing.T) {
	uc := &auth.UserContext{UserID: "user-1", Email: "alice@example.com", OrgRole: store.OrgRoleResponder}
	ctx := auth.WithUser(context.Background(), uc)

	got, ok := auth.UserFromContext(ctx)
	if !ok {
		t.Fatal("UserFromContext: ok = false, want true")
	}
	if got != uc {
		t.Fatalf("UserFromContext: got %+v, want the same *UserContext instance %+v", got, uc)
	}
}

func TestUserFromContext_NotSet(t *testing.T) {
	got, ok := auth.UserFromContext(context.Background())
	if ok {
		t.Fatal("ok = true, want false for a context with no user attached")
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestUserFromContext_NilValueAttached(t *testing.T) {
	// WithUser's signature takes a *UserContext, so a caller could in
	// principle attach a nil pointer explicitly. UserFromContext must treat
	// that the same as "no user attached" rather than reporting ok=true with
	// a nil *UserContext a caller could dereference.
	ctx := auth.WithUser(context.Background(), nil)

	got, ok := auth.UserFromContext(ctx)
	if ok {
		t.Fatal("ok = true, want false for an explicitly nil *UserContext")
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}
