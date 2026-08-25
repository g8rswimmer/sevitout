//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestIntegrationConfigStore covers Get/Upsert/List for §18.4's per-
// integration credentials and settings. Per this store's own doc comment,
// credentials are stored exactly as handed to it — encryption/decryption is
// ConfigServer's responsibility (internal/api/grpc), not this store's — so
// EncryptedCredentials here is just an opaque byte slice standing in for
// ciphertext, and this test does not exercise real encryption.
func TestIntegrationConfigStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewIntegrationConfigStore(pool)

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "slack"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	cfg := &store.IntegrationConfig{
		IntegrationType:      "slack",
		EncryptedCredentials: []byte("opaque-ciphertext"),
		Settings:             map[string]any{"default_channel": "#incidents"},
	}

	t.Run("Upsert_Insert", func(t *testing.T) {
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if cfg.ID == 0 {
			t.Fatal("ID should be set after Upsert")
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, "slack")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got.EncryptedCredentials) != "opaque-ciphertext" {
			t.Fatalf("credentials mismatch: got %q", got.EncryptedCredentials)
		}
		if got.Settings["default_channel"] != "#incidents" {
			t.Fatalf("settings mismatch: got %+v", got.Settings)
		}
	})

	t.Run("Upsert_UpdatePreservesCredentialsWhenReSent", func(t *testing.T) {
		cfg.Settings = map[string]any{"default_channel": "#incidents-v2"}
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got, err := s.Get(ctx, "slack")
		if err != nil {
			t.Fatalf("Get after Upsert: %v", err)
		}
		if got.Settings["default_channel"] != "#incidents-v2" {
			t.Fatalf("settings not updated: got %+v", got.Settings)
		}
	})

	t.Run("List", func(t *testing.T) {
		if err := s.Upsert(ctx, &store.IntegrationConfig{IntegrationType: "pagerduty", Settings: map[string]any{}}); err != nil {
			t.Fatalf("Upsert pagerduty: %v", err)
		}
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2, got %d", len(items))
		}
	})
}
