package main

import (
	"context"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// githubIssueResolver implements grpchandler.IssueClient the same way
// onCallResolver implements grpchandler.OnCaller: datastore-configured
// credentials (integration_type "github", credential key "token" — the same
// convention githubHealthChecker already uses) are preferred over fallback,
// the process's static GITHUB_TOKEN client (which may itself be nil).
type githubIssueResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.IssueClient
}

// newGitHubIssueResolver always returns a non-nil grpchandler.IssueClient,
// even when fallback is nil — see newOnCallResolver's doc comment for why.
func newGitHubIssueResolver(integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.IssueClient) grpchandler.IssueClient {
	return &githubIssueResolver{integrations: integrations, crypto: crypto, fallback: fallback}
}

// newGitHubIssueClient builds the live client used for a datastore-configured
// GitHub credential. A package-level var (rather than constructing
// githubIssueClient directly) so resolver tests can substitute a fake and
// assert the datastore path was taken without making a real GitHub API call.
var newGitHubIssueClient = func(token string) grpchandler.IssueClient {
	return &githubIssueClient{c: github.NewClient(token)}
}

func (r *githubIssueResolver) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*grpchandler.CreatedIssue, error) {
	if creds, _, ok := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "github"); ok {
		if token := creds["token"]; token != "" {
			return newGitHubIssueClient(token).CreateIssue(ctx, owner, repo, title, body, labels)
		}
	}
	if r.fallback == nil {
		return nil, grpchandler.ErrIntegrationNotConfigured
	}
	return r.fallback.CreateIssue(ctx, owner, repo, title, body, labels)
}
