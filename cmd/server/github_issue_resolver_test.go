package main

import (
	"context"
	"errors"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func TestGitHubIssueResolver_DatastoreConfiguredAtStartup_PrefersDatastore(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "github", map[string]string{"token": "ghp_live_token"}, nil)

	origNewGitHubIssueClient := newGitHubIssueClient
	t.Cleanup(func() { newGitHubIssueClient = origNewGitHubIssueClient })
	var gotToken string
	wantIssue := &grpchandler.CreatedIssue{Number: 42, URL: "https://github.com/o/r/issues/42"}
	newGitHubIssueClient = func(token string) grpchandler.IssueClient {
		gotToken = token
		return &fakeIssueClient{issue: wantIssue}
	}

	fallback := &fakeIssueClient{issue: &grpchandler.CreatedIssue{Number: 1}}
	resolver := newGitHubIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue {
		t.Errorf("CreateIssue returned %+v, want the datastore-configured client's issue", got)
	}
	if gotToken != "ghp_live_token" {
		t.Errorf("datastore client built with token = %q, want %q", gotToken, "ghp_live_token")
	}
	if fallback.called {
		t.Error("fallback should not be called when datastore config is usable")
	}
}

func TestGitHubIssueResolver_NoDatastoreRowAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // no "github" row upserted
	enc := crypto.NewKeyEncryptor(mustKey(t))

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestGitHubIssueResolver_DatastoreRowMissingCredentialAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "github", nil, map[string]any{"note": "placeholder"})

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestGitHubIssueResolver_DecryptionFailsAtStartup_FallsBackWithoutError(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	writeKeyEnc := crypto.NewKeyEncryptor(mustKey(t))
	readKeyEnc := crypto.NewKeyEncryptor(mustKey(t)) // different key: decryption will fail
	putIntegrationConfig(t, integrations, writeKeyEnc, "github", map[string]string{"token": "ghp_live_token"}, nil)

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(context.Background(), integrations, readKeyEnc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue should swallow decrypt failures, got err: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestGitHubIssueResolver_NeitherConfigured_ReturnsErrIntegrationNotConfigured(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))

	resolver := newGitHubIssueResolver(context.Background(), integrations, enc, nil)

	_, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if !errors.Is(err, grpchandler.ErrIntegrationNotConfigured) {
		t.Errorf("CreateIssue err = %v, want ErrIntegrationNotConfigured", err)
	}
}

// TestGitHubIssueResolver_RefreshWithIncompleteCredentials_ReturnsErrorAndUsesFallback
// covers the failure-signaling every *Resolver type needs: a
// RefreshIntegrationCredentials call that can't actually enable the
// datastore path must say so — ConfigServer treats a non-nil return as the
// write having failed and rolls it back (see
// IntegrationCredentialsRefresher's doc comment).
func TestGitHubIssueResolver_RefreshWithIncompleteCredentials_ReturnsErrorAndUsesFallback(t *testing.T) {
	fallbackIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: fallbackIssue}
	resolver := newGitHubIssueResolver(context.Background(), memory.NewIntegrationConfigStore(), crypto.NewKeyEncryptor(mustKey(t)), fallback)

	err := resolver.RefreshIntegrationCredentials(context.Background(), "github", map[string]string{"token": ""}, nil)
	if err == nil {
		t.Fatal("RefreshIntegrationCredentials with no usable token should return an error")
	}

	got, createErr := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if createErr != nil {
		t.Fatalf("CreateIssue: %v", createErr)
	}
	if got != fallbackIssue {
		t.Errorf("CreateIssue after a rejected refresh = %+v, want the fallback still in use", got)
	}
}

// TestGitHubIssueResolver_RefreshWithValidCredentials_ReturnsNilError is the
// success-path counterpart to the test above.
func TestGitHubIssueResolver_RefreshWithValidCredentials_ReturnsNilError(t *testing.T) {
	origNewGitHubIssueClient := newGitHubIssueClient
	t.Cleanup(func() { newGitHubIssueClient = origNewGitHubIssueClient })
	newGitHubIssueClient = func(string) grpchandler.IssueClient { return &fakeIssueClient{} }

	resolver := newGitHubIssueResolver(context.Background(), memory.NewIntegrationConfigStore(), crypto.NewKeyEncryptor(mustKey(t)), nil)

	if err := resolver.RefreshIntegrationCredentials(context.Background(), "github", map[string]string{"token": "ghp_live_token"}, nil); err != nil {
		t.Errorf("RefreshIntegrationCredentials with a usable token = %v, want nil", err)
	}
}

// TestGitHubIssueResolver_RefreshAppliesCredentialsDirectlyWithoutRestart is
// the core behavior this design exists for — see the OnCaller resolver's
// equivalent test for the full rationale.
func TestGitHubIssueResolver_RefreshAppliesCredentialsDirectlyWithoutRestart(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // never written to in this test
	enc := crypto.NewKeyEncryptor(mustKey(t))

	fallbackIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: fallbackIssue}
	resolver := newGitHubIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != fallbackIssue {
		t.Fatalf("CreateIssue before any refresh = %+v, want fallback", got)
	}

	origNewGitHubIssueClient := newGitHubIssueClient
	t.Cleanup(func() { newGitHubIssueClient = origNewGitHubIssueClient })
	datastoreIssue := &grpchandler.CreatedIssue{Number: 42}
	var gotToken string
	newGitHubIssueClient = func(token string) grpchandler.IssueClient {
		gotToken = token
		return &fakeIssueClient{issue: datastoreIssue}
	}

	// Refreshing for an unrelated integration type must be a no-op.
	if err := resolver.RefreshIntegrationCredentials(context.Background(), "jira", map[string]string{"token": "ghp_live_token"}, nil); err != nil {
		t.Fatalf("RefreshIntegrationCredentials: %v", err)
	}
	got, err = resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != fallbackIssue {
		t.Errorf("CreateIssue after an unrelated refresh = %+v, want still fallback", got)
	}

	if err := resolver.RefreshIntegrationCredentials(context.Background(), "github", map[string]string{"token": "ghp_live_token"}, nil); err != nil {
		t.Fatalf("RefreshIntegrationCredentials: %v", err)
	}
	got, err = resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil, "")
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != datastoreIssue {
		t.Errorf("CreateIssue after RefreshIntegrationCredentials(\"github\") = %+v, want %+v", got, datastoreIssue)
	}
	if gotToken != "ghp_live_token" {
		t.Errorf("datastore client built with token = %q, want %q", gotToken, "ghp_live_token")
	}
}
