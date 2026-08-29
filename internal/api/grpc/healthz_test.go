package grpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
)

// fakePinger is a scripted grpchandler.Pinger for tests.
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(_ context.Context) error {
	return f.err
}

func TestHealthzHandler_Reachable_ReturnsOK(t *testing.T) {
	h := grpchandler.NewHealthzHandler(fakePinger{})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
}

func TestHealthzHandler_Unreachable_ReturnsServiceUnavailable(t *testing.T) {
	h := grpchandler.NewHealthzHandler(fakePinger{err: errors.New("db exploded")})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "unavailable" {
		t.Errorf("status field = %q, want %q", body["status"], "unavailable")
	}
	// The underlying error detail is logged server-side (see
	// NewHealthzHandler's doc comment) but must never cross the wire to an
	// unauthenticated caller.
	if _, leaked := body["error"]; leaked {
		t.Errorf("response body leaked underlying error detail: %v", body)
	}
}

func TestHealthzHandler_WrongMethod_ReturnsMethodNotAllowed(t *testing.T) {
	h := grpchandler.NewHealthzHandler(fakePinger{})

	req := httptest.NewRequest("POST", "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 405 {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
