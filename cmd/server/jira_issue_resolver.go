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
// comment for the resolve-once-at-startup, refresh-by-direct-handoff
// rationale this mirrors.
type jiraIssueResolver struct {
	fallback grpchandler.JiraIssueClient // the static JIRA_CLOUD_ID/JIRA_API_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.JiraIssueClient // datastore-configured client, or fallback; nil if neither is configured
}

// newJiraIssueResolver resolves current from the datastore once,
// immediately — see newOnCallResolver.
func newJiraIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.JiraIssueClient) *jiraIssueResolver {
	r := &jiraIssueResolver{fallback: fallback}
	creds, settings, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "jira")
	r.apply(creds, settings)
	return r
}

// apply picks what current should point to given credentials/settings
// already known to be plaintext and current, and swaps it in under mu.
func (r *jiraIssueResolver) apply(credentials map[string]string, settings map[string]any) {
	next := r.fallback
	apiToken := credentials["api_token"]
	cloudID, _ := settings["cloud_id"].(string)
	if apiToken != "" && cloudID != "" {
		next = newJiraIssueClientFn(cloudID, apiToken)
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
}

// RefreshIntegrationCredentials applies a new "jira" credential/setting pair
// the moment ConfigServer.UpsertIntegrationConfig saves one; calls for any
// other integration_type are ignored, since this resolver owns only Jira
// issue creation — see onCallResolver.RefreshIntegrationCredentials's doc
// comment for the error-return rationale.
func (r *jiraIssueResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, settings map[string]any) error {
	if integrationType != "jira" {
		return nil
	}
	r.apply(credentials, settings)
	return nil
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
