package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/auth"
)

func TestExtractBearerToken_FromAuthorizationHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "Bearer abc.def.ghi")

	got := auth.ExtractBearerToken(r)
	if got != "abc.def.ghi" {
		t.Errorf("got %q, want %q", got, "abc.def.ghi")
	}
}

func TestExtractBearerToken_AuthorizationHeaderPreferredOverCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "Bearer header-token")
	r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})

	got := auth.ExtractBearerToken(r)
	if got != "header-token" {
		t.Errorf("got %q, want the header token to take precedence", got)
	}
}

func TestExtractBearerToken_MalformedAuthorizationHeader(t *testing.T) {
	// A present-but-non-"Bearer " Authorization header must not fall through
	// to the cookie — an explicit-but-wrong header is a caller error, not an
	// invitation to try a different credential.
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})

	got := auth.ExtractBearerToken(r)
	if got != "" {
		t.Errorf("got %q, want empty string for a malformed header", got)
	}
}

func TestExtractBearerToken_FallsBackToCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})

	got := auth.ExtractBearerToken(r)
	if got != "cookie-token" {
		t.Errorf("got %q, want %q", got, "cookie-token")
	}
}

func TestExtractBearerToken_NeitherPresent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	got := auth.ExtractBearerToken(r)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}
