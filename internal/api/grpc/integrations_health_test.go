package grpc_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeChecker is a scripted grpchandler.HealthChecker for tests.
type fakeChecker struct {
	err error
}

func (f fakeChecker) Check(_ context.Context, _ map[string]string, _ map[string]any) error {
	return f.err
}

// slowChecker blocks for delay before reporting healthy, so tests can assert
// that other checks aren't queued up behind it.
type slowChecker struct {
	delay time.Duration
}

func (s slowChecker) Check(ctx context.Context, _ map[string]string, _ map[string]any) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type testHealthHandler struct {
	handler *grpchandler.IntegrationsHealthHandler
	signer  *auth.JWTSigner
	users   *memory.UserStore
	configs *memory.IntegrationConfigStore
}

func newTestHealthHandler(enc grpchandler.Encryptor, checkers map[string]grpchandler.HealthChecker) *testHealthHandler {
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()
	configs := memory.NewIntegrationConfigStore()
	return &testHealthHandler{
		handler: grpchandler.NewIntegrationsHealthHandler(configs, enc, checkers, signer, users),
		signer:  signer,
		users:   users,
		configs: configs,
	}
}

func (h *testHealthHandler) seedUser(t *testing.T, id string, role store.OrgRole) string {
	t.Helper()
	now := time.Now()
	u := &store.User{ID: id, Email: id + "@example.com", Name: id, OrgRole: role, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := h.users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := h.signer.Sign(u.ID, u.Email, string(u.OrgRole))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func TestIntegrationsHealth_MissingToken(t *testing.T) {
	h := newTestHealthHandler(nil, nil)
	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestIntegrationsHealth_InsufficientRole(t *testing.T) {
	h := newTestHealthHandler(nil, nil)
	token := h.seedUser(t, "viewer-1", store.OrgRoleViewer)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestIntegrationsHealth_NoIntegrationsConfigured(t *testing.T) {
	h := newTestHealthHandler(nil, nil)
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"integrations":[]`) {
		t.Errorf("body = %s, want empty integrations list", body)
	}
}

func TestIntegrationsHealth_UnregisteredCheckerReportsUnknown(t *testing.T) {
	h := newTestHealthHandler(nil, nil) // no checkers registered
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{IntegrationType: "datadog"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"status":"unknown"`) {
		t.Errorf("body = %s, want status unknown for an integration with no registered checker", body)
	}
}

func TestIntegrationsHealth_CheckerSuccess(t *testing.T) {
	h := newTestHealthHandler(nil, map[string]grpchandler.HealthChecker{"pagerduty": fakeChecker{}})
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{IntegrationType: "pagerduty"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if body := w.Body.String(); !strings.Contains(body, `"status":"connected"`) {
		t.Errorf("body = %s, want status connected", body)
	}
}

func TestIntegrationsHealth_CheckerFailure(t *testing.T) {
	h := newTestHealthHandler(nil, map[string]grpchandler.HealthChecker{
		"pagerduty": fakeChecker{err: errors.New("401 unauthorized")},
	})
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{IntegrationType: "pagerduty"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `"status":"error"`) || !strings.Contains(body, "401 unauthorized") {
		t.Errorf("body = %s, want status error with the checker's message", body)
	}
}

// Checks for different integrations must run concurrently: a slow checker
// must not delay the others queued behind it in the configured-integrations
// list.
func TestIntegrationsHealth_ChecksRunConcurrently(t *testing.T) {
	const delay = 300 * time.Millisecond
	h := newTestHealthHandler(nil, map[string]grpchandler.HealthChecker{
		"pagerduty": slowChecker{delay: delay},
		"github":    fakeChecker{},
	})
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{IntegrationType: "pagerduty"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{IntegrationType: "github"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	start := time.Now()
	h.handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	// Sequential execution would take at least 2×delay; concurrent execution
	// takes ~1×delay. Use 1.5×delay as a generous cutoff to avoid flaking.
	if elapsed > delay+delay/2 {
		t.Errorf("ServeHTTP took %v, want well under %v (checks should run concurrently, not sequentially)", elapsed, 2*delay)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"integration_type":"pagerduty"`) || !strings.Contains(body, `"integration_type":"github"`) {
		t.Errorf("body = %s, want both integrations reported", body)
	}
	if !strings.Contains(body, `"status":"connected"`) {
		t.Errorf("body = %s, want at least one connected status", body)
	}
}

func TestIntegrationsHealth_DecryptFailureReportsError(t *testing.T) {
	// Credentials were stored (encrypted), but no encryptor is configured to
	// decrypt them for the check — this must surface as an error, not a panic
	// or a silently-skipped check.
	h := newTestHealthHandler(nil, map[string]grpchandler.HealthChecker{"pagerduty": fakeChecker{}})
	if err := h.configs.Upsert(context.Background(), &store.IntegrationConfig{
		IntegrationType:      "pagerduty",
		EncryptedCredentials: []byte("some-ciphertext"),
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	token := h.seedUser(t, "admin-1", store.OrgRoleAdmin)

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if body := w.Body.String(); !strings.Contains(body, `"status":"error"`) {
		t.Errorf("body = %s, want status error when credentials can't be decrypted", body)
	}
}
