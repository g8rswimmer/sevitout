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
// comment for the resolve-once-and-cache rationale this mirrors.
type githubIssueResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.IssueClient // the static GITHUB_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.IssueClient // resolved datastore client, or fallback; nil if neither is configured
}

// newGitHubIssueResolver resolves current immediately (datastore config
// first, fallback second) before returning — see newOnCallResolver.
func newGitHubIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.IssueClient) *githubIssueResolver {
	r := &githubIssueResolver{integrations: integrations, crypto: crypto, fallback: fallback}
	_ = r.refresh(ctx)
	return r
}

// refresh re-resolves current from the datastore (falling back to the
// static client whenever the datastore has nothing usable, whether that's
// because it's genuinely unconfigured or because resolution failed) and
// swaps it in under mu. It returns the resolution error, if any — see
// onCallResolver.refresh's doc comment for why current is always left
// usable regardless.
func (r *githubIssueResolver) refresh(ctx context.Context) error {
	creds, _, ok, err := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "github")
	next := r.fallback
	if ok {
		if token := creds["token"]; token != "" {
			next = newGitHubIssueClient(token)
		}
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	return err
}

// RefreshIntegrationCredentials re-resolves current when the "github"
// integration's config changes via the Config API; calls for any other
// integration_type are ignored (returning nil), since this resolver owns
// only GitHub issue creation. A non-nil return means the datastore config
// that was just written could not actually be resolved into a usable
// client — see onCallResolver.RefreshIntegrationCredentials's doc comment.
func (r *githubIssueResolver) RefreshIntegrationCredentials(ctx context.Context, integrationType string) error {
	if integrationType != "github" {
		return nil
	}
	return r.refresh(ctx)
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
