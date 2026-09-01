package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func strPtr(v string) *string { return &v }

// TestUserStore_UpdateIntegrationIdentities covers the Phase 10a self-service
// identity write path: full-replace on every field, clearing a field with a
// nil pointer, and ErrNotFound for an unknown user.
func TestUserStore_UpdateIntegrationIdentities(t *testing.T) {
	ctx := context.Background()
	s := memory.NewUserStore()
	now := time.Now()
	if err := s.Create(ctx, &store.User{
		ID: "user-1", Email: "alice@example.com", Name: "Alice",
		OrgRole: store.OrgRoleResponder, Active: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	t.Run("SetsAllThreeFields", func(t *testing.T) {
		got, err := s.UpdateIntegrationIdentities(ctx, "user-1", strPtr("U123"), strPtr("alice-gh"), strPtr("acc-1"))
		if err != nil {
			t.Fatalf("UpdateIntegrationIdentities: %v", err)
		}
		if got.SlackUserID == nil || *got.SlackUserID != "U123" {
			t.Errorf("SlackUserID = %v, want U123", got.SlackUserID)
		}
		if got.GitHubUsername == nil || *got.GitHubUsername != "alice-gh" {
			t.Errorf("GitHubUsername = %v, want alice-gh", got.GitHubUsername)
		}
		if got.JiraAccountID == nil || *got.JiraAccountID != "acc-1" {
			t.Errorf("JiraAccountID = %v, want acc-1", got.JiraAccountID)
		}
	})

	t.Run("FullReplaceClearsOmittedField", func(t *testing.T) {
		// A nil slackUserID clears the field even though it was previously
		// set — full-replace, not sparse-patch (docs/roadmap.md Phase 10a).
		got, err := s.UpdateIntegrationIdentities(ctx, "user-1", nil, strPtr("alice-gh"), strPtr("acc-1"))
		if err != nil {
			t.Fatalf("UpdateIntegrationIdentities: %v", err)
		}
		if got.SlackUserID != nil {
			t.Errorf("SlackUserID = %v, want cleared (nil)", *got.SlackUserID)
		}
		if got.GitHubUsername == nil || *got.GitHubUsername != "alice-gh" {
			t.Errorf("GitHubUsername should be unaffected, got %v", got.GitHubUsername)
		}
	})

	t.Run("PersistsAcrossGet", func(t *testing.T) {
		got, err := s.Get(ctx, "user-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.JiraAccountID == nil || *got.JiraAccountID != "acc-1" {
			t.Errorf("JiraAccountID = %v, want acc-1 to persist", got.JiraAccountID)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		if _, err := s.UpdateIntegrationIdentities(ctx, "missing", strPtr("U1"), nil, nil); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
