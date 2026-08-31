package main

import (
	"context"
	"errors"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func TestJiraIssueResolver_DatastoreConfiguredAtStartup_PrefersDatastore(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	putIntegrationConfig(t, integrations, enc, "jira",
		map[string]string{"api_token": "jira_live_token"},
		map[string]any{"cloud_id": "cloud-123", "site_url": "https://example.atlassian.net"})

	origNewJiraIssueClientFn := newJiraIssueClientFn
	t.Cleanup(func() { newJiraIssueClientFn = origNewJiraIssueClientFn })
	var gotCloudID, gotAPIToken, gotSiteURL string
	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1", URL: "https://example.atlassian.net/browse/PROJ-1"}
	newJiraIssueClientFn = func(cloudID, apiToken, siteURL string) grpchandler.JiraIssueClient {
		gotCloudID, gotAPIToken, gotSiteURL = cloudID, apiToken, siteURL
		return &fakeJiraIssueClient{issue: wantIssue}
	}

	fallback := &fakeJiraIssueClient{issue: &grpchandler.CreatedIssue{Key: "OTHER-1"}}
	resolver := newJiraIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue {
		t.Errorf("CreateIssue returned %+v, want the datastore-configured client's issue", got)
	}
	if gotCloudID != "cloud-123" || gotAPIToken != "jira_live_token" || gotSiteURL != "https://example.atlassian.net" {
		t.Errorf("datastore client built with (cloudID=%q, apiToken=%q, siteURL=%q), want (%q, %q, %q)",
			gotCloudID, gotAPIToken, gotSiteURL, "cloud-123", "jira_live_token", "https://example.atlassian.net")
	}
	if fallback.called {
		t.Error("fallback should not be called when datastore config is usable")
	}
}

func TestJiraIssueResolver_NoDatastoreRowAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // no "jira" row upserted
	enc := crypto.NewKeyEncryptor(mustKey(t))

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestJiraIssueResolver_DatastoreRowMissingCloudIDAtStartup_FallsBack(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	enc := crypto.NewKeyEncryptor(mustKey(t))
	// api_token present but cloud_id missing from settings — Jira needs both.
	putIntegrationConfig(t, integrations, enc, "jira", map[string]string{"api_token": "jira_live_token"}, nil)

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != wantIssue || !fallback.called {
		t.Errorf("CreateIssue = (%+v, called=%v), want fallback used", got, fallback.called)
	}
}

func TestJiraIssueResolver_DecryptionFailsAtStartup_FallsBackWithoutError(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore()
	writeKeyEnc := crypto.NewKeyEncryptor(mustKey(t))
	readKeyEnc := crypto.NewKeyEncryptor(mustKey(t)) // different key: decryption will fail
	putIntegrationConfig(t, integrations, writeKeyEnc, "jira",
		map[string]string{"api_token": "jira_live_token"},
		map[string]any{"cloud_id": "cloud-123"})

	wantIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	fallback := &fakeJiraIssueClient{issue: wantIssue}
	resolver := newJiraIssueResolver(context.Background(), integrations, readKeyEnc, fallback)

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

	resolver := newJiraIssueResolver(context.Background(), integrations, enc, nil)

	_, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if !errors.Is(err, grpchandler.ErrIntegrationNotConfigured) {
		t.Errorf("CreateIssue err = %v, want ErrIntegrationNotConfigured", err)
	}
}

// TestJiraIssueResolver_RefreshWithIncompleteCredentials_ReturnsErrorAndUsesFallback
// covers the failure-signaling this resolver needs — see the equivalent
// GitHub resolver test for the full rationale. Jira needs two values
// (api_token, cloud_id), so both "only one present" cases are covered.
func TestJiraIssueResolver_RefreshWithIncompleteCredentials_ReturnsErrorAndUsesFallback(t *testing.T) {
	tests := []struct {
		name        string
		credentials map[string]string
		settings    map[string]any
	}{
		{"missing api_token", map[string]string{}, map[string]any{"cloud_id": "cloud-123"}},
		{"missing cloud_id", map[string]string{"api_token": "jira_live_token"}, map[string]any{}},
		{"neither", map[string]string{}, map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fallbackIssue := &grpchandler.CreatedIssue{Key: "FALLBACK-1"}
			fallback := &fakeJiraIssueClient{issue: fallbackIssue}
			resolver := newJiraIssueResolver(context.Background(), memory.NewIntegrationConfigStore(), crypto.NewKeyEncryptor(mustKey(t)), fallback)

			if err := resolver.RefreshIntegrationCredentials(context.Background(), "jira", tt.credentials, tt.settings); err == nil {
				t.Fatal("RefreshIntegrationCredentials with incomplete credentials/settings should return an error")
			}

			got, createErr := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
			if createErr != nil {
				t.Fatalf("CreateIssue: %v", createErr)
			}
			if got != fallbackIssue {
				t.Errorf("CreateIssue after a rejected refresh = %+v, want the fallback still in use", got)
			}
		})
	}
}

// TestJiraIssueResolver_RefreshWithValidCredentials_ReturnsNilError is the
// success-path counterpart to the test above.
func TestJiraIssueResolver_RefreshWithValidCredentials_ReturnsNilError(t *testing.T) {
	origNewJiraIssueClientFn := newJiraIssueClientFn
	t.Cleanup(func() { newJiraIssueClientFn = origNewJiraIssueClientFn })
	newJiraIssueClientFn = func(string, string, string) grpchandler.JiraIssueClient { return &fakeJiraIssueClient{} }

	resolver := newJiraIssueResolver(context.Background(), memory.NewIntegrationConfigStore(), crypto.NewKeyEncryptor(mustKey(t)), nil)

	err := resolver.RefreshIntegrationCredentials(context.Background(), "jira",
		map[string]string{"api_token": "jira_live_token"}, map[string]any{"cloud_id": "cloud-123"})
	if err != nil {
		t.Errorf("RefreshIntegrationCredentials with complete credentials/settings = %v, want nil", err)
	}
}

// TestJiraIssueResolver_RefreshAppliesCredentialsDirectlyWithoutRestart is
// the core behavior this design exists for — see the OnCaller resolver's
// equivalent test for the full rationale.
func TestJiraIssueResolver_RefreshAppliesCredentialsDirectlyWithoutRestart(t *testing.T) {
	integrations := memory.NewIntegrationConfigStore() // never written to in this test
	enc := crypto.NewKeyEncryptor(mustKey(t))

	fallbackIssue := &grpchandler.CreatedIssue{Key: "FALLBACK-1"}
	fallback := &fakeJiraIssueClient{issue: fallbackIssue}
	resolver := newJiraIssueResolver(context.Background(), integrations, enc, fallback)

	got, err := resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != fallbackIssue {
		t.Fatalf("CreateIssue before any refresh = %+v, want fallback", got)
	}

	origNewJiraIssueClientFn := newJiraIssueClientFn
	t.Cleanup(func() { newJiraIssueClientFn = origNewJiraIssueClientFn })
	datastoreIssue := &grpchandler.CreatedIssue{Key: "PROJ-1"}
	var gotCloudID, gotAPIToken string
	newJiraIssueClientFn = func(cloudID, apiToken, _ string) grpchandler.JiraIssueClient {
		gotCloudID, gotAPIToken = cloudID, apiToken
		return &fakeJiraIssueClient{issue: datastoreIssue}
	}
	creds := map[string]string{"api_token": "jira_live_token"}
	settings := map[string]any{"cloud_id": "cloud-123"}

	// Refreshing for an unrelated integration type must be a no-op.
	if err := resolver.RefreshIntegrationCredentials(context.Background(), "github", creds, settings); err != nil {
		t.Fatalf("RefreshIntegrationCredentials: %v", err)
	}
	got, err = resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != fallbackIssue {
		t.Errorf("CreateIssue after an unrelated refresh = %+v, want still fallback", got)
	}

	if err := resolver.RefreshIntegrationCredentials(context.Background(), "jira", creds, settings); err != nil {
		t.Fatalf("RefreshIntegrationCredentials: %v", err)
	}
	got, err = resolver.CreateIssue(context.Background(), "PROJ", "Bug", "summary", "description", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got != datastoreIssue {
		t.Errorf("CreateIssue after RefreshIntegrationCredentials(\"jira\") = %+v, want %+v", got, datastoreIssue)
	}
	if gotCloudID != "cloud-123" || gotAPIToken != "jira_live_token" {
		t.Errorf("datastore client built with (cloudID=%q, apiToken=%q), want (%q, %q)",
			gotCloudID, gotAPIToken, "cloud-123", "jira_live_token")
	}
}
