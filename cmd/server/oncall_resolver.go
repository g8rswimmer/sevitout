package main

import (
	"context"
	"sync"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// onCallResolver implements both grpchandler.OnCaller and
// grpchandler.IntegrationCredentialsRefresher. Rather than hitting the
// datastore on every OnCallLookup call, it resolves once — at construction
// (server startup) and again only when notified that the "pagerduty"
// integration's config changed via the Config API — and caches the result,
// so the hot path (OnCallLookup) never touches the datastore. This trades
// "always current, one DB read per request" for "current as of the last
// startup or config write, zero DB reads per request," which is the right
// trade for a value that only ever changes through an explicit admin
// action, not on some other schedule OnCallLookup would need to notice on
// its own.
type onCallResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.OnCaller // the static PAGERDUTY_API_KEY client, or nil

	mu      sync.RWMutex
	current grpchandler.OnCaller // resolved datastore client, or fallback; nil if neither is configured
}

// newOnCallResolver resolves current immediately (datastore config first,
// fallback second) before returning, so the very first request after
// startup already sees whatever was configured at that point.
func newOnCallResolver(ctx context.Context, integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.OnCaller) *onCallResolver {
	r := &onCallResolver{integrations: integrations, crypto: crypto, fallback: fallback}
	r.refresh(ctx)
	return r
}

// refresh re-resolves current from the datastore (falling back to the
// static client when the datastore has nothing usable) and swaps it in
// under mu.
func (r *onCallResolver) refresh(ctx context.Context) {
	next := r.fallback
	if creds, _, ok := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "pagerduty"); ok {
		if apiKey := creds["api_key"]; apiKey != "" {
			next = newPagerdutyOnCaller(apiKey)
		}
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
}

// RefreshIntegrationCredentials re-resolves current when the "pagerduty"
// integration's config changes via the Config API; calls for any other
// integration_type are ignored, since this resolver owns only PagerDuty
// on-call lookups.
func (r *onCallResolver) RefreshIntegrationCredentials(ctx context.Context, integrationType string) {
	if integrationType != "pagerduty" {
		return
	}
	r.refresh(ctx)
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
