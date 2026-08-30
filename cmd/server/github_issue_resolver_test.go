package main

import (
	"context"
	"errors"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func TestGitHubIssueResolver_DatastoreConfigured_PrefersDatastore(t *testing.T) {
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
	resolver := newGitHubIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil)
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

func TestGitHubIssueResolver_NoDatastoreRow_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // no "github" row upserted
	enc := crypto.NewKeyEncryptor(mustKey(t))

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestGitHubIssueResolver_DatastoreRowMissingCredential_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "github", nil, map[string]any{"note": "placeholder"})

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestGitHubIssueResolver_DecryptionFails_FallsBackWithoutError(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	writeKeyEnc := crypto.NewKeyEncryptor(mustKey(t))
	readKeyEnc := crypto.NewKeyEncryptor(mustKey(t)) // different key: decryption will fail
	putIntegrationConfig(t, integrations, writeKeyEnc, "github", map[string]string{"token": "ghp_live_token"}, nil)

	wantIssue := &grpchandler.CreatedIssue{Number: 1}
	fallback := &fakeIssueClient{issue: wantIssue}
	resolver := newGitHubIssueResolver(integrations, readKeyEnc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil)
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

	resolver := newGitHubIssueResolver(integrations, enc, nil)

	_, err := resolver.CreateIssue(context.Background(), "o", "r", "title", "body", nil)
	if !errors.Is(err, grpchandler.ErrIntegrationNotConfigured) {
		t.Errorf("CreateIssue err = %v, want ErrIntegrationNotConfigured", err)
	}
}
