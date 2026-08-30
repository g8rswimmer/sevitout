package main

import (
	"context"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// githubIssueResolver implements both grpchandler.IssueClient and
// grpchandler.IntegrationCredentialsRefresher — see onCallResolver's doc
// comment for the resolve-once-at-startup, refresh-by-direct-handoff
// rationale this mirrors.
type githubIssueResolver struct {
	fallback grpchandler.IssueClient // the static GITHUB_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.IssueClient // datastore-configured client, or fallback; nil if neither is configured
}

// newGitHubIssueResolver resolves current from the datastore once,
// immediately — see newOnCallResolver.
func newGitHubIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.IssueClient) *githubIssueResolver {
	r := &githubIssueResolver{fallback: fallback}
	creds, _, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "github")
	r.apply(creds)
	return r
}

// apply picks what current should point to given credentials already known
// to be plaintext and current, and swaps it in under mu.
func (r *githubIssueResolver) apply(credentials map[string]string) {
	next := r.fallback
	if token := credentials["token"]; token != "" {
		next = newGitHubIssueClient(token)
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
}

// RefreshIntegrationCredentials applies a new "github" credential the moment
// ConfigServer.UpsertIntegrationConfig saves one; calls for any other
// integration_type are ignored, since this resolver owns only GitHub issue
// creation — see onCallResolver.RefreshIntegrationCredentials's doc comment
// for the error-return rationale.
func (r *githubIssueResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, _ map[string]any) error {
	if integrationType != "github" {
		return nil
	}
	r.apply(credentials)
	return nil
}

// newGitHubIssueClient builds the live client used for a datastore-configured
// GitHub credential. A package-level var (rather than constructing
// githubIssueClient directly) so resolver tests can substitute a fake and
// assert the datastore path was taken without making a real GitHub API call.
var newGitHubIssueClient = func(token string) grpchandler.IssueClient {
	return &githubIssueClient{c: github.NewClient(token)}
}

func (r *githubIssueResolver) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*grpchandler.CreatedIssue, error) {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return nil, grpchandler.ErrIntegrationNotConfigured
	}
	return current.CreateIssue(ctx, owner, repo, title, body, labels)
}

var (
	_ grpchandler.IssueClient                     = (*githubIssueResolver)(nil)
	_ grpchandler.IntegrationCredentialsRefresher = (*githubIssueResolver)(nil)
)
