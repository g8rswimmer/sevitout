package main

import (
	"context"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// onCallResolver implements both grpchandler.OnCaller and
// grpchandler.IntegrationCredentialsRefresher. It resolves the datastore
// path exactly once, at construction (server startup); after that,
// RefreshIntegrationCredentials is handed the current plaintext credentials
// directly by grpchandler.ConfigServer.UpsertIntegrationConfig whenever the
// "pagerduty" config changes, so this resolver never touches the datastore
// or a decryptor again after startup. The hot path (OnCallLookup) just
// reads whatever was last resolved or handed to it.
type onCallResolver struct {
	fallback grpchandler.OnCaller // the static PAGERDUTY_API_KEY client, or nil

	mu      sync.RWMutex
	current grpchandler.OnCaller // datastore-configured client, or fallback; nil if neither is configured
}

// newOnCallResolver resolves current from the datastore once, immediately
// (falling back to fallback when nothing usable is stored), so the very
// first request after startup already reflects whatever was configured at
// that point.
func newOnCallResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.OnCaller) *onCallResolver {
	r := &onCallResolver{fallback: fallback}
	creds, _, _ := resolveIntegrationCredentials(ctx, integrations, crypto, "pagerduty")
	r.apply(creds)
	return r
}

// apply picks what current should point to given credentials already known
// to be plaintext and current — either freshly decrypted at startup, or
// handed directly by RefreshIntegrationCredentials — and swaps it in under
// mu.
func (r *onCallResolver) apply(credentials map[string]string) {
	next := r.fallback
	if apiKey := credentials["api_key"]; apiKey != "" {
		next = newPagerdutyOnCaller(apiKey)
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
}

// RefreshIntegrationCredentials applies a new "pagerduty" credential the
// moment ConfigServer.UpsertIntegrationConfig saves one — calls for any
// other integration_type are ignored, since this resolver owns only
// PagerDuty on-call lookups. Building an OnCaller from credentials never
// fails for PagerDuty (pagerduty.NewClient does no I/O), so this always
// returns nil; the error return exists purely so ConfigServer can reject a
// write outright — before persisting anything — if some future
// integration's construction can fail (e.g. one that validates against a
// live API).
func (r *onCallResolver) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, _ map[string]any) error {
	if integrationType != "pagerduty" {
		return nil
	}
	r.apply(credentials)
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

func (r *onCallResolver) OnCallLookup(ctx context.Context, serviceID string) (string, error) {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return "", nil // OnCaller's documented "nobody on-call" contract
	}
	return current.OnCallLookup(ctx, serviceID)
}

var (
	_ grpchandler.OnCaller                        = (*onCallResolver)(nil)
	_ grpchandler.IntegrationCredentialsRefresher = (*onCallResolver)(nil)
)
