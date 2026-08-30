package main

import (
	"context"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/jira"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// jiraIssueResolver implements both grpchandler.JiraIssueClient and
// grpchandler.IntegrationCredentialsRefresher — see onCallResolver's doc
// comment for the resolve-once-and-cache rationale this mirrors.
type jiraIssueResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.JiraIssueClient // the static JIRA_CLOUD_ID/JIRA_API_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.JiraIssueClient // resolved datastore client, or fallback; nil if neither is configured
}

// newJiraIssueResolver resolves current immediately (datastore config
// first, fallback second) before returning — see newOnCallResolver.
func newJiraIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.JiraIssueClient) *jiraIssueResolver {
	r := &jiraIssueResolver{integrations: integrations, crypto: crypto, fallback: fallback}
	r.refresh(ctx)
	return r
}

// refresh re-resolves current from the datastore (falling back to the
// static client when the datastore has nothing usable) and swaps it in
// under mu.
func (r *jiraIssueResolver) refresh(ctx context.Context) {
	next := r.fallback
	if creds, settings, ok := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "jira"); ok {
		apiToken := creds["api_token"]
		cloudID, _ := settings["cloud_id"].(string)
		if apiToken != "" && cloudID != "" {
			next = newJiraIssueClientFn(cloudID, apiToken)
		}
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
}

// RefreshIntegrationCredentials re-resolves current when the "jira"
// integration's config changes via the Config API; calls for any other
// integration_type are ignored, since this resolver owns only Jira issue
// creation.
func (r *jiraIssueResolver) RefreshIntegrationCredentials(ctx context.Context, integrationType string) {
	if integrationType != "jira" {
		return
	}
	r.refresh(ctx)
}

// newJiraIssueClientFn builds the live client used for a datastore-configured
// Jira credential. A package-level var (rather than constructing
// jiraIssueClient directly) so resolver tests can substitute a fake and
// assert the datastore path was taken without making a real Jira API call.
// Site URL is cosmetic only (browse-link generation, see
// jiraIssueClient.CreateIssue) and has no settings-key convention yet — ""
// is safe, jira.NewClient treats it as "no browse links".
var newJiraIssueClientFn = func(cloudID, apiToken string) grpchandler.JiraIssueClient {
	return &jiraIssueClient{c: jira.NewClient(cloudID, apiToken, "")}
}

func (r *jiraIssueResolver) CreateIssue(ctx context.Context, projectKey, issueType, summary, description string, labels []string) (*grpchandler.CreatedIssue, error) {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return nil, grpchandler.ErrIntegrationNotConfigured
	}
	return current.CreateIssue(ctx, projectKey, issueType, summary, description, labels)
}

var (
	_ grpchandler.JiraIssueClient                 = (*jiraIssueResolver)(nil)
	_ grpchandler.IntegrationCredentialsRefresher = (*jiraIssueResolver)(nil)
)
