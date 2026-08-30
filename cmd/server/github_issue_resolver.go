package main

import (
	"context"
	"fmt"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// githubIssueResolver implements both grpchandler.IssueClient and
// grpchandler.IntegrationCredentialsRefresher — see pagerdutyResolver's doc
// comment for the resolve-once-at-startup, refresh-by-direct-handoff
// rationale this mirrors.
type githubIssueResolver struct {
	fallback grpchandler.IssueClient // the static GITHUB_TOKEN client, or nil

	mu      sync.RWMutex
	current grpchandler.IssueClient // datastore-configured client, or fallback; nil if neither is configured
}

// newGitHubIssueResolver resolves current from the datastore once,
// immediately — see newPagerdutyResolver. A startup fallback (nothing usable
// in the datastore yet) is expected, ordinary operation, not reported as an
// error — there's no request yet whose caller could be told about it the
// way RefreshIntegrationCredentials's caller can.
func newGitHubIssueResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.IssueClient) *githubIssueResolver {
	r := &githubIssueResolver{fallback: fallback}
	creds, _, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "github")
	r.apply(creds)
	return r
}

// apply picks what current should point to given credentials already known
// to be plaintext and current, swaps it in under mu, and reports whether it
// had to fall back to the static client (usedFallback=true, including when
// fallback is itself nil) rather than a datastore-configured one.
func (r *githubIssueResolver) apply(credentials map[string]string) (usedFallback bool) {
	next := r.fallback
	usedFallback = true
	if token := credentials["token"]; token != "" {
		next = newGitHubIssueClient(token)
		usedFallback = false
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	return usedFallback
}

// RefreshIntegrationCredentials applies a new "github" credential the moment
// ConfigServer.UpsertIntegrationConfig saves one; calls for any other
// integration_type are ignored, since this resolver owns only GitHub issue
// creation. Falling back here is reported as an error, the same as
// pagerdutyResolver/jiraIssueResolver: it means the credentials ConfigServer
// just persisted don't actually enable the datastore path, which
// ConfigServer treats as the write having failed (see
// IntegrationCredentialsRefresher's doc comment) and rolls back. IssueClient
// has no "not configured" contract the way OnCaller.OnCallLookup does —
// every CreateIssue call is expected to either succeed or report a real
// failure — but that's a statement about what CreateIssue itself returns at
// runtime, not about whether this config write took effect, which is what's
// being reported here.
func (r *githubIssueResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, _ map[string]any) error {
	if integrationType != "github" {
		return nil
	}
	if usedFallback := r.apply(credentials); usedFallback {
		return fmt.Errorf("github: no usable \"token\" credential; falling back to the static configuration")
	}
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
