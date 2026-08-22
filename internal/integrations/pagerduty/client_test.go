package pagerduty_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
