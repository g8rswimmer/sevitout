package main

import (
	"context"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func TestOnCallResolver_DatastoreConfiguredAtStartup_PrefersDatastore(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "pagerduty", map[string]string{"api_key": "pd_live_key"}, nil)

	origNewPagerdutyOnCaller := newPagerdutyOnCaller
	t.Cleanup(func() { newPagerdutyOnCaller = origNewPagerdutyOnCaller })
	var gotAPIKey string
	newPagerdutyOnCaller = func(apiKey string) grpchandler.OnCaller {
		gotAPIKey = apiKey
		return &fakeOnCaller{name: "alice"}
	}

	fallback := &fakeOnCaller{name: "static-fallback"}
	resolver := newOnCallResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "alice" {
		t.Errorf("OnCallLookup = %q, want %q (datastore-configured client resolved at construction)", got, "alice")
	}
	if gotAPIKey != "pd_live_key" {
		t.Errorf("datastore client built with api_key = %q, want %q", gotAPIKey, "pd_live_key")
	}
	if fallback.called {
		t.Error("fallback should not be called when datastore config is usable")
	}
}

func TestOnCallResolver_NoDatastoreRowAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // no "pagerduty" row upserted
	enc := crypto.NewKeyEncryptor(mustKey(t))

	fallback := &fakeOnCaller{name: "static-fallback"}
	resolver := newOnCallResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "static-fallback" || !fallback.called {
		t.Errorf("OnCallLookup = (%q, called=%v), want fallback used", got, fallback.called)
	}
}

func TestOnCallResolver_DatastoreRowMissingCredentialAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	// Row exists but has no credentials at all (e.g. a settings-only row).
	putIntegrationConfig(t, integrations, enc, "pagerduty", nil, map[string]any{"note": "placeholder"})

	fallback := &fakeOnCaller{name: "static-fallback"}
	resolver := newOnCallResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "static-fallback" || !fallback.called {
		t.Errorf("OnCallLookup = (%q, called=%v), want fallback used", got, fallback.called)
	}
}

func TestOnCallResolver_DecryptionFailsAtStartup_FallsBackWithoutError(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	writeKeyEnc := crypto.NewKeyEncryptor(mustKey(t))
	readKeyEnc := crypto.NewKeyEncryptor(mustKey(t)) // different key: decryption will fail
	putIntegrationConfig(t, integrations, writeKeyEnc, "pagerduty", map[string]string{"api_key": "pd_live_key"}, nil)

	fallback := &fakeOnCaller{name: "static-fallback"}
	resolver := newOnCallResolver(context.Background(), integrations, readKeyEnc, fallback)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup should swallow decrypt failures, got err: %v", err)
	}
	if got != "static-fallback" || !fallback.called {
		t.Errorf("OnCallLookup = (%q, called=%v), want fallback used", got, fallback.called)
	}
}

func TestOnCallResolver_NeitherConfigured_ReturnsEmpty(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))

	resolver := newOnCallResolver(context.Background(), integrations, enc, nil)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "" {
		t.Errorf("OnCallLookup = %q, want \"\" (nobody on-call / not configured)", got)
	}
}

// TestOnCallResolver_RefreshPicksUpDatastoreChangeWithoutRestart is the core
// behavior this caching design exists for: a resolver constructed before any
// datastore config existed (so it started on the static fallback) picks up
// a credential added later purely because RefreshIntegrationCredentials is
// called — the same notification ConfigServer.UpsertIntegrationConfig sends
// in production — with no new construction and no per-call datastore read.
func TestOnCallResolver_RefreshPicksUpDatastoreChangeWithoutRestart(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))

	fallback := &fakeOnCaller{name: "static-fallback"}
	resolver := newOnCallResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "static-fallback" {
		t.Fatalf("OnCallLookup before config exists = %q, want fallback", got)
	}

	origNewPagerdutyOnCaller := newPagerdutyOnCaller
	t.Cleanup(func() { newPagerdutyOnCaller = origNewPagerdutyOnCaller })
	newPagerdutyOnCaller = func(apiKey string) grpchandler.OnCaller {
		return &fakeOnCaller{name: "alice"}
	}
	putIntegrationConfig(t, integrations, enc, "pagerduty", map[string]string{"api_key": "pd_live_key"}, nil)

	// Refreshing for an unrelated integration type must be a no-op.
	resolver.RefreshIntegrationCredentials(context.Background(), "github")
	got, err = resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "static-fallback" {
		t.Errorf("OnCallLookup after an unrelated refresh = %q, want still fallback", got)
	}

	resolver.RefreshIntegrationCredentials(context.Background(), "pagerduty")
	got, err = resolver.OnCallLookup(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "alice" {
		t.Errorf("OnCallLookup after RefreshIntegrationCredentials(\"pagerduty\") = %q, want %q", got, "alice")
	}
}
