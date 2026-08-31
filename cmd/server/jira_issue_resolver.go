package main

import (
	"context"
	"fmt"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/jira"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// jiraIssueResolver implements both grpchandler.JiraIssueClient and
// grpchandler.IntegrationCredentialsRefresher — see pagerdutyResolver's doc
// comment for the resolve-once-at-startup, refresh-by-direct-handoff
// rationale this mirrors.
type jiraIssueResolver struct {
	fallback grpchandler.JiraIssueClient // the static JIRA_CLOUD_ID/JIRA_API_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.JiraIssueClient // datastore-configured client, or fallback; nil if neither is configured
}

// newJiraIssueResolver resolves current from the datastore once,
// immediately — see newPagerdutyResolver. A startup fallback (nothing usable
// in the datastore yet) is expected, ordinary operation, not reported as an
// error — there's no request yet whose caller could be told about it the
// way RefreshIntegrationCredentials's caller can.
func newJiraIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.JiraIssueClient) *jiraIssueResolver {
	r := &jiraIssueResolver{fallback: fallback}
	creds, settings, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "jira")
	r.apply(creds, settings)
	return r
}

// apply picks what current should point to given credentials/settings
// already known to be plaintext and current, swaps it in under mu, and
// reports whether it had to fall back to the static client
// (usedFallback=true, including when fallback is itself nil) rather than a
// datastore-configured one. site_url is optional — an empty value is
// treated the same as it not being set at all (see newJiraIssueClientFn).
func (r *jiraIssueResolver) apply(credentials map[string]string, settings map[string]any) (usedFallback bool) {
	next := r.fallback
	usedFallback = true
	apiToken := credentials["api_token"]
	cloudID, _ := settings["cloud_id"].(string)
	siteURL, _ := settings["site_url"].(string)
	if apiToken != "" && cloudID != "" {
		next = newJiraIssueClientFn(cloudID, apiToken, siteURL)
		usedFallback = false
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	return usedFallback
}

// RefreshIntegrationCredentials applies a new "jira" credential/setting pair
// the moment ConfigServer.UpsertIntegrationConfig saves one; calls for any
// other integration_type are ignored, since this resolver owns only Jira
// issue creation. Falling back here is reported as an error, the same as
// pagerdutyResolver/githubIssueResolver — see githubIssueResolver's
// RefreshIntegrationCredentials doc comment for the full rationale.
func (r *jiraIssueResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, settings map[string]any) error {
	if integrationType != "jira" {
		return nil
	}
	if usedFallback := r.apply(credentials, settings); usedFallback {
		return fmt.Errorf("jira: need both \"api_token\" (credentials) and \"cloud_id\" (settings); falling back to the static configuration")
	}
	return nil
}

// newJiraIssueClientFn builds the live client used for a datastore-configured
// Jira credential. A package-level var (rather than constructing
// jiraIssueClient directly) so resolver tests can substitute a fake and
// assert the datastore path was taken without making a real Jira API call.
// siteURL is cosmetic only (browse-link generation, see
// jiraIssueClient.CreateIssue) — mirroring config.Config.JiraSiteURL's
// independently-optional treatment of JIRA_SITE_URL, an empty siteURL is
// safe here too, since jira.NewClient treats "" as "no browse links".
var newJiraIssueClientFn = func(cloudID, apiToken, siteURL string) grpchandler.JiraIssueClient {
	return &jiraIssueClient{c: jira.NewClient(cloudID, apiToken, siteURL)}
}

func (r *jiraIssueResolver) CreateIssue(ctx context.Context, projectKey, issueType, summary, description string, labels []string, assigneeAccountID string) (*grpchandler.CreatedIssue, error) {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return nil, grpchandler.ErrIntegrationNotConfigured
	}
	return current.CreateIssue(ctx, projectKey, issueType, summary, description, labels, assigneeAccountID)
}

var (
	_ grpchandler.JiraIssueClient                 = (*jiraIssueResolver)(nil)
	_ grpchandler.IntegrationCredentialsRefresher = (*jiraIssueResolver)(nil)
)
