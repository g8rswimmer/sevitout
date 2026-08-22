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
