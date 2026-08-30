package main

import (
	"context"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// onCallResolver implements grpchandler.OnCaller, preferring datastore-
// configured PagerDuty credentials (integration_type "pagerduty", credential
// key "api_key" — the same convention pagerdutyHealthChecker already uses)
// over fallback, the process's static PAGERDUTY_API_KEY client (which may
// itself be nil when that env var is unset).
type onCallResolver struct {
	integrations store.IntegrationConfigStore
	crypto       grpchandler.Encryptor
	fallback     grpchandler.OnCaller
}

// newOnCallResolver always returns a non-nil grpchandler.OnCaller, even when
// fallback is nil: the datastore may be configured later, at any time,
// without a restart, via the Config API, so the value wired into
// grpchandler.SEVServerParams.OnCaller must not be a literal nil that could
// never pick that up.
func newOnCallResolver(integrations store.IntegrationConfigStore, crypto grpchandler.Encryptor, fallback grpchandler.OnCaller) grpchandler.OnCaller {
	return &onCallResolver{integrations: integrations, crypto: crypto, fallback: fallback}
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
	if creds, _, ok := resolveIntegrationCredentials(ctx, r.integrations, r.crypto, "pagerduty"); ok {
		if apiKey := creds["api_key"]; apiKey != "" {
			return newPagerdutyOnCaller(apiKey).OnCallLookup(ctx, serviceID)
		}
	}
	if r.fallback == nil {
		return "", nil // OnCaller's documented "nobody on-call" contract
	}
	return r.fallback.OnCallLookup(ctx, serviceID)
}
