package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func newTestHandler(t *testing.T) (*auth.PasswordHandler, store.UserStore) {
	t.Helper()
	signer := auth.NewJWTSigner("test-secret-32-bytes-long-enough!", 1)
	users := memory.NewUserStore()
	return auth.NewPasswordHandler(signer, users), users
}

func doPost(t *testing.T, mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func registerMux(h *auth.PasswordHandler) *http.ServeMux {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func TestRegister_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	rr := doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Token string `json:"token"`
		User  struct {
			Email   string `json:"email"`
			OrgRole string `json:"org_role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", resp.User.Email)
	}
}

func TestRegister_FirstUserGetsAdmin(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	rr := doPost(t, mux, "/auth/register", map[string]string{
		"email": "admin@example.com", "name": "Admin", "password": "password123",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		User struct {
			OrgRole string `json:"org_role"`
		} `json:"user"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.User.OrgRole != string(store.OrgRoleAdmin) {
		t.Errorf("first user: expected role %q, got %q", store.OrgRoleAdmin, resp.User.OrgRole)
	}
}

func TestRegister_SubsequentUsersGetViewer(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "admin@example.com", "name": "Admin", "password": "password123",
	})

	rr := doPost(t, mux, "/auth/register", map[string]string{
		"email": "viewer@example.com", "name": "Viewer", "password": "password123",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		User struct {
			OrgRole string `json:"org_role"`
		} `json:"user"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.User.OrgRole != string(store.OrgRoleViewer) {
		t.Errorf("second user: expected role %q, got %q", store.OrgRoleViewer, resp.User.OrgRole)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})

	rr := doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice2", "password": "different123",
	})
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestRegister_MissingFields(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	cases := []map[string]string{
		{"email": "", "name": "Alice", "password": "password123"},
		{"email": "alice@example.com", "name": "", "password": "password123"},
		{"email": "alice@example.com", "name": "Alice", "password": ""},
	}
	for _, c := range cases {
		rr := doPost(t, mux, "/auth/register", c)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d for input %v", rr.Code, c)
		}
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	rr := doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "short",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})

	rr := doPost(t, mux, "/auth/login", map[string]string{
		"email": "alice@example.com", "password": "password123",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})

	rr := doPost(t, mux, "/auth/login", map[string]string{
		"email": "alice@example.com", "password": "wrongpassword",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	rr := doPost(t, mux, "/auth/login", map[string]string{
		"email": "nobody@example.com", "password": "password123",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_TokenIsValid(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-32-bytes-long-enough!", 1)
	users := memory.NewUserStore()
	h := auth.NewPasswordHandler(signer, users)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})
	rr := doPost(t, mux, "/auth/login", map[string]string{
		"email": "alice@example.com", "password": "password123",
	})

	var resp struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	claims, err := signer.Validate(resp.Token)
	if err != nil {
		t.Fatalf("token should be valid: %v", err)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("expected email in claims, got %q", claims.Email)
	}
}
