package grpc

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// healthCheckTimeout bounds each integration's connectivity check
// independently of whatever timeout (if any) its own HTTP client applies —
// so one slow or misbehaving checker can't stall the response past this,
// regardless of how it's implemented.
const healthCheckTimeout = 10 * time.Second

// integrationsHealthMethod is a pseudo gRPC-method path used only to look up
// the minimum org role required to call the health-check endpoint, via the
// same rbac.go table every real RPC is checked against. GET
// /admin/integrations/health is a plain HTTP handler (not part of
// ConfigService's proto — see docs/project-plan.md M10) so it bypasses the
// gRPC interceptor entirely and needs its own RBAC floor, mirroring how
// internal/api/ws.Handler checks "/sevitout.v1.WebSocket/Subscribe".
const integrationsHealthMethod = "/sevitout.v1.ConfigService/IntegrationsHealth"

// HealthChecker verifies connectivity for one integration type using its
// decrypted credentials and non-secret settings. A nil error means healthy.
// Declared here (the consumer) so this package depends only on the one
// operation it needs from any given integration client.
type HealthChecker interface {
	Check(ctx context.Context, credentials map[string]string, settings map[string]any) error
}

// integrationStatus is the health of one configured integration, as reported
// by GET /admin/integrations/health.
type integrationStatus struct {
	IntegrationType string `json:"integration_type"`
	// Status is one of: "connected", "error", "not_configured", "unknown".
	// "unknown" means a config row exists but no HealthChecker is
	// registered for this integration_type, so connectivity was not tested.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// tokenValidator validates a signed JWT and returns its claims. Implemented
// by *auth.JWTSigner; declared here (the consumer) so this package depends
// only on the behavior it needs — mirrors internal/api/ws's copy of the same
// interface.
type tokenValidator interface {
	Validate(tokenStr string) (*auth.Claims, error)
}

// IntegrationsHealthHandler serves GET /admin/integrations/health: for every
// configured integration, it reports whether a live connectivity check
// (when a HealthChecker is registered for that integration_type) succeeded.
type IntegrationsHealthHandler struct {
	integrations store.IntegrationConfigStore
	crypto       Encryptor
	checkers     map[string]HealthChecker
	signer       tokenValidator
	users        store.UserStore
}

// NewIntegrationsHealthHandler returns a handler that authenticates each
// request the same way the gRPC interceptor does (JWT + active user + RBAC),
// then runs the registered checker (if any) for every configured integration.
func NewIntegrationsHealthHandler(
	integrations store.IntegrationConfigStore,
	crypto Encryptor,
	checkers map[string]HealthChecker,
	signer tokenValidator,
	users store.UserStore,
) *IntegrationsHealthHandler {
	return &IntegrationsHealthHandler{
		integrations: integrations,
		crypto:       crypto,
		checkers:     checkers,
		signer:       signer,
		users:        users,
	}
}

func (h *IntegrationsHealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := auth.ExtractBearerToken(r)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	claims, err := h.signer.Validate(token)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	user, err := h.users.Get(r.Context(), claims.Subject)
	if err != nil || !user.Active {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	if !auth.HasPermission(user.OrgRole, integrationsHealthMethod) {
		http.Error(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	cfgs, err := h.integrations.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list integration configs", http.StatusInternalServerError)
		return
	}

	// Run all checks concurrently — sequentially, one slow or unresponsive
	// integration would delay every check behind it in the list, so overall
	// latency would scale with the number of configured integrations rather
	// than with the single slowest one.
	statuses := make([]integrationStatus, len(cfgs))
	var wg sync.WaitGroup
	wg.Add(len(cfgs))
	for i, cfg := range cfgs {
		go func(i int, cfg *store.IntegrationConfig) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
			defer cancel()
			statuses[i] = h.checkOne(ctx, cfg)
		}(i, cfg)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"integrations": statuses})
}

func (h *IntegrationsHealthHandler) checkOne(ctx context.Context, cfg *store.IntegrationConfig) integrationStatus {
	checker, ok := h.checkers[cfg.IntegrationType]
	if !ok {
		return integrationStatus{IntegrationType: cfg.IntegrationType, Status: "unknown"}
	}

	creds, err := DecryptIntegrationCredentials(h.crypto, cfg)
	if err != nil {
		return integrationStatus{IntegrationType: cfg.IntegrationType, Status: "error", Error: err.Error()}
	}
	if err := checker.Check(ctx, creds, cfg.Settings); err != nil {
		return integrationStatus{IntegrationType: cfg.IntegrationType, Status: "error", Error: err.Error()}
	}
	return integrationStatus{IntegrationType: cfg.IntegrationType, Status: "connected"}
}
