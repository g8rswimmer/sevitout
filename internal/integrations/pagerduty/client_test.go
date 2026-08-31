package pagerduty_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
)

// oncallResponse models the subset of the PagerDuty /oncalls response we parse.
type oncallResponse struct {
	OnCalls []struct {
		EscalationLevel int `json:"escalation_level"`
		User            struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
	} `json:"oncalls"`
}

func newTestServer(t *testing.T, statusCode int, body any) (*httptest.Server, *pagerduty.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	c := pagerduty.NewClientWithBaseURL("test-key", srv.URL)
	return srv, c
}

func TestOnCallLookup_ReturnsNameAndEmail(t *testing.T) {
	body := oncallResponse{}
	body.OnCalls = append(body.OnCalls, struct {
		EscalationLevel int `json:"escalation_level"`
		User            struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
	}{
		EscalationLevel: 1,
		User: struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}{Name: "Alice", Email: "alice@example.com"},
	})

	_, c := newTestServer(t, http.StatusOK, body)
	got, err := c.OnCallLookup(context.Background(), "SVC-001")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "Alice <alice@example.com>" {
		t.Errorf("got %q, want %q", got, "Alice <alice@example.com>")
	}
}

func TestOnCallLookup_ReturnsNameOnlyWhenNoEmail(t *testing.T) {
	body := oncallResponse{}
	body.OnCalls = append(body.OnCalls, struct {
		EscalationLevel int `json:"escalation_level"`
		User            struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
	}{
		EscalationLevel: 1,
		User: struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}{Name: "Bob"},
	})

	_, c := newTestServer(t, http.StatusOK, body)
	got, err := c.OnCallLookup(context.Background(), "SVC-001")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "Bob" {
		t.Errorf("got %q, want %q", got, "Bob")
	}
}

func TestOnCallLookup_EmptyWhenNoOnCall(t *testing.T) {
	body := oncallResponse{}

	_, c := newTestServer(t, http.StatusOK, body)
	got, err := c.OnCallLookup(context.Background(), "SVC-001")
	if err != nil {
		t.Fatalf("OnCallLookup: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestOnCallLookup_ErrorOnNonOK(t *testing.T) {
	_, c := newTestServer(t, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	_, err := c.OnCallLookup(context.Background(), "SVC-001")
	if err == nil {
		t.Fatal("want error for non-200 status, got nil")
	}
}

func TestOnCallLookup_SendsAuthHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oncallResponse{})
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("my-key", srv.URL)
	_, _ = c.OnCallLookup(context.Background(), "SVC-001")
	if gotHeader != "Token token=my-key" {
		t.Errorf("Authorization header = %q, want %q", gotHeader, "Token token=my-key")
	}
}

func TestPing_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abilities" {
			t.Errorf("path = %q, want /abilities", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("my-key", srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestPing_ErrorOnNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("bad-key", srv.URL)
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping should error on a non-200 response")
	}
}

// TestPing_ErrorIncludesPagerDutyMessage covers the fix for a real health
// check gap: an admin viewing a PagerDuty connectivity error previously saw
// only a bare "unexpected status 401" with no indication of what PagerDuty
// itself said was wrong (docs/roadmap.md Phase 9's admin integrations page
// surfaces this message directly). PagerDuty's real error envelope is
// {"error":{"message":"...","code":...}} — this asserts that message reaches
// both Error() and the exported APIError type's fields.
func TestPing_ErrorIncludesPagerDutyMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Not Authorized", "code": 2006},
		})
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("bad-key", srv.URL)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should error on a non-200 response")
	}
	if !strings.Contains(err.Error(), "Not Authorized") {
		t.Errorf("Error() = %q, want it to include PagerDuty's message", err.Error())
	}

	var apiErr *pagerduty.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T, want *pagerduty.APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if apiErr.Message != "Not Authorized" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "Not Authorized")
	}
	if apiErr.HTTPStatus() != http.StatusUnauthorized {
		t.Errorf("HTTPStatus() = %d, want %d", apiErr.HTTPStatus(), http.StatusUnauthorized)
	}
}

// TestPing_NonJSONErrorBodyFallsBackToBareStatus covers the forgiving path:
// a response body that isn't PagerDuty's expected error shape (or isn't
// JSON at all) must not make newAPIError panic or itself return a decode
// error — Message just stays empty, same as github/jira's equivalent.
func TestPing_NonJSONErrorBodyFallsBackToBareStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("bad-key", srv.URL)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should error on a non-200 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 503") {
		t.Errorf("Error() = %q, want the bare-status fallback message", err.Error())
	}
}

func TestOnCallLookup_SendsServiceIDQueryParam(t *testing.T) {
	var gotServiceIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotServiceIDs = r.URL.Query()["service_ids[]"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oncallResponse{})
	}))
	t.Cleanup(srv.Close)

	c := pagerduty.NewClientWithBaseURL("key", srv.URL)
	_, _ = c.OnCallLookup(context.Background(), "MY-SVC-ID")
	if !reflect.DeepEqual(gotServiceIDs, []string{"MY-SVC-ID"}) {
		t.Errorf("service_ids[] = %v, want [MY-SVC-ID]", gotServiceIDs)
	}
}
