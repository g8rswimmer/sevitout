//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestUserStore covers Create/Get/GetByEmail/Update/List/Count. users.id is
// the primary key and users.email carries a real UNIQUE constraint
// (migrations/000002_schema.up.sql, unchanged by the later password_auth
// migration), so CreateEmailConflict exercises that specifically.
func TestUserStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewUserStore(pool)

	user := &store.User{
		ID: "usr-abc", Email: "alice@example.com", Name: "Alice",
		OrgRole: store.OrgRoleResponder, Active: true, PasswordHash: "bcrypt-hash-placeholder",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicateID", func(t *testing.T) {
		dup := &store.User{ID: "usr-abc", Email: "different@example.com", Name: "Different", OrgRole: store.OrgRoleViewer, PasswordHash: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("CreateEmailConflict", func(t *testing.T) {
		dup := &store.User{ID: "usr-xyz", Email: "alice@example.com", Name: "Also Alice", OrgRole: store.OrgRoleViewer, PasswordHash: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict on email dup, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, user.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Email != user.Email {
			t.Fatal("email mismatch")
		}
		if got.PasswordHash != user.PasswordHash {
			t.Fatal("password_hash mismatch")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("GetByEmail", func(t *testing.T) {
		got, err := s.GetByEmail(ctx, user.Email)
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		if got.ID != user.ID {
			t.Fatal("id mismatch")
		}
	})

	t.Run("GetByEmailNotFound", func(t *testing.T) {
		if _, err := s.GetByEmail(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		user.OrgRole = store.OrgRoleAdmin
		user.UpdatedAt = time.Now()
		if err := s.Update(ctx, user); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.Get(ctx, user.ID)
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if got.OrgRole != store.OrgRoleAdmin {
			t.Fatal("role not updated")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := &store.User{ID: "missing", Name: "ghost", UpdatedAt: time.Now()}
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		users, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("want 1, got %d", len(users))
		}
	})

	t.Run("Count", func(t *testing.T) {
		n, err := s.Count(ctx)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if n != 1 {
			t.Fatalf("want 1, got %d", n)
		}
	})
}
