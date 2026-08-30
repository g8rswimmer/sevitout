package main

import (
	"context"
	"fmt"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// pagerdutyResolver implements both grpchandler.OnCaller and
// grpchandler.IntegrationCredentialsRefresher — see githubIssueResolver's
// doc comment for the resolve-once-at-startup, refresh-by-direct-handoff
// rationale this mirrors.
type pagerdutyResolver struct {
	fallback grpchandler.OnCaller // the static PAGERDUTY_API_KEY client, or nil

	mu      sync.RWMutex
	current grpchandler.OnCaller // datastore-configured client, or fallback; nil if neither is configured
}

// newPagerdutyResolver resolves current from the datastore once,
// immediately (falling back to fallback when nothing usable is stored) — a
// startup fallback is expected, ordinary operation, not reported as an
// error, since there's no request yet whose caller could be told about it
// the way RefreshIntegrationCredentials's caller can.
func newPagerdutyResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.OnCaller) *pagerdutyResolver {
	r := &pagerdutyResolver{fallback: fallback}
	creds, _, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "pagerduty")
	r.apply(creds)
	return r
}

// apply picks what current should point to given credentials already known
// to be plaintext and current, swaps it in under mu, and reports whether it
// had to fall back to the static client (usedFallback=true, including when
// fallback is itself nil) rather than a datastore-configured one.
func (r *pagerdutyResolver) apply(credentials map[string]string) (usedFallback bool) {
	next := r.fallback
	usedFallback = true
	if apiKey := credentials["api_key"]; apiKey != "" {
		next = newPagerdutyOnCaller(apiKey)
		usedFallback = false
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	return usedFallback
}

// RefreshIntegrationCredentials applies a new "pagerduty" credential the
// moment ConfigServer.UpsertIntegrationConfig saves one; calls for any
// other integration_type are ignored, since this resolver owns only
// PagerDuty on-call lookups.
//
// Falling back here is reported as an error, the same as
// githubIssueResolver/jiraIssueResolver: it means the credentials
// ConfigServer just persisted don't actually enable the datastore path,
// which ConfigServer treats as the write having failed (see
// IntegrationCredentialsRefresher's doc comment) and rolls back. This is
// orthogonal to OnCallLookup's own contract below, which separately treats
// "nobody on-call" as a normal runtime result — that's about what a lookup
// call returns once things are configured, not about whether a config
// write actually took effect.
func (r *pagerdutyResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, _ map[string]any) error {
	if integrationType != "pagerduty" {
		return nil
	}
	if usedFallback := r.apply(credentials); usedFallback {
		return fmt.Errorf("pagerduty: no usable \"api_key\" credential; falling back to the static configuration")
	}
	return nil
}

// newPagerdutyOnCaller builds the live client used for a datastore-configured
// PagerDuty credential. A package-level var (rather than calling
// pagerduty.NewClient directly) so resolver tests can substitute a fake and
// assert the datastore path was taken without making a real PagerDuty API
// call.
var newPagerdutyOnCaller = func(apiKey string) grpchandler.OnCaller {
	return pagerduty.NewClient(apiKey)
}

// OnCallLookup returns ("", nil) — OnCaller's documented "nobody on-call"
// contract — whenever current is nil, whether that's because neither a
// datastore-configured nor a static credential exists, or because the most
// recent RefreshIntegrationCredentials call rejected an incomplete update
// (see that method's doc comment).
func (r *pagerdutyResolver) OnCallLookup(ctx context.Context, serviceID string) (string, error) {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return "", nil
	}
	return current.OnCallLookup(ctx, serviceID)
}

var (
	_ grpchandler.OnCaller                        = (*pagerdutyResolver)(nil)
	_ grpchandler.IntegrationCredentialsRefresher = (*pagerdutyResolver)(nil)
)
