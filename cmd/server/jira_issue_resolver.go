package main

import (
	"context"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/jira"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// jiraIssueResolver implements grpchandler.JiraIssueClient the same way
// onCallResolver implements grpchandler.OnCaller: datastore-configured
// credentials (integration_type "jira", credential key "api_token" +
// settings key "cloud_id" — the same two-value convention jiraHealthChecker
// already uses) are preferred over fallback, the process's static
// JIRA_CLOUD_ID/JIRA_API_TOKEN client (which may itself be nil).
type jiraIssueResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.JiraIssueClient
}

// newJiraIssueResolver always returns a non-nil grpchandler.JiraIssueClient,
// even when fallback is nil — see newOnCallResolver's doc comment for why.
func newJiraIssueResolver(integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.JiraIssueClient) grpchandler.JiraIssueClient {
	return &jiraIssueResolver{integrations: integrations, crypto: crypto, fallback: fallback}
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
	if creds, settings, ok := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "jira"); ok {
		apiToken := creds["api_token"]
		cloudID, _ := settings["cloud_id"].(string)
		if apiToken != "" && cloudID != "" {
			return newJiraIssueClientFn(cloudID, apiToken).CreateIssue(ctx, projectKey, issueType, summary, description, labels)
		}
	}
	if r.fallback == nil {
		return nil, grpchandler.ErrIntegrationNotConfigured
	}
	return r.fallback.CreateIssue(ctx, projectKey, issueType, summary, description, labels)
}
