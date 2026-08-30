package main

import (
	"context"
	"errors"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func TestJiraIssueResolver_DatastoreConfigured_PrefersDatastore(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "jira",
		map[string]string{"api_token": "jira_live_token"},
		map[string]any{"cloud_id": "cloud-123"})

	origNewJiraIssueClientFn := newJiraIssueClientFn
	t.Cleanup(func() { newJiraIssueClientFn = origNewJiraIssueClientFn })
	var gotCloudID, gotAPIToken string
	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1", URL: "https://example.atlassian.net/browse/PROJ-1"}
	newJiraIssueClientFn = func(cloudID, apiToken string) grpchandler.JiraIssueClient {
		gotCloudID, gotAPIToken = cloudID, apiToken
		return &fakeJiraIssueClient{issue: wantIssue}
	}

	fallback := &fakeJiraIssueClient{issue: &grpchandler.CreatedIssue{Key: "OTHER-1"}}
	resolver := newJiraIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue {
		t.Errorf("CreateIssue returned %+v, want the datastore-configured client's issue", got)
	}
	if gotCloudID != "cloud-123" || gotAPIToken != "jira_live_token" {
		t.Errorf("datastore client built with (cloudID=%q, apiToken=%q), want (%q, %q)",
			gotCloudID, gotAPIToken, "cloud-123", "jira_live_token")
	}
	if fallback.called {
		t.Error("fallback should not be called when datastore config is usable")
	}
}

func TestJiraIssueResolver_NoDatastoreRow_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // no "jira" row upserted
	enc := crypto.NewKeyEncryptor(mustKey(t))

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestJiraIssueResolver_DatastoreRowMissingCloudID_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	// api_token present but cloud_id missing from settings — Jira needs both.
	putIntegrationConfig(t, integrations, enc, "jira", map[string]string{"api_token": "jira_live_token"}, nil)

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestJiraIssueResolver_DecryptionFails_FallsBackWithoutError(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	writeKeyEnc := crypto.NewKeyEncryptor(mustKey(t))
	readKeyEnc := crypto.NewKeyEncryptor(mustKey(t)) // different key: decryption will fail
	putIntegrationConfig(t, integrations, writeKeyEnc, "jira",
		map[string]string{"api_token": "jira_live_token"},
		map[string]any{"cloud_id": "cloud-123"})

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(integrations, readKeyEnc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue should swallow decrypt failures, got err: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestJiraIssueResolver_NeitherConfigured_ReturnsErrIntegrationNotConfigured(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))

	resolver := newJiraIssueResolver(integrations, enc, nil)

	_, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if !errors.Is(err, grpchandler.ErrIntegrationNotConfigured) {
		t.Errorf("CreateIssue err = %v, want ErrIntegrationNotConfigured", err)
	}
}
