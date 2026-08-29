package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
)

func TestGatewayMetadata_NoHeadersOrCookie_ReturnsNil(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	if got := gatewayMetadata(r); got != nil {
		t.Errorf("gatewayMetadata = %v, want nil", got)
	}
}

func TestGatewayMetadata_ForwardsAuthorizationHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	r.Header.Set("Authorization", "Bearer abc123")

	md := gatewayMetadata(r)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer abc123" {
		t.Errorf("authorization metadata = %v, want [Bearer abc123]", got)
	}
}

func TestGatewayMetadata_FallsBackToTokenCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token-value"})

	md := gatewayMetadata(r)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer cookie-token-value" {
		t.Errorf("authorization metadata = %v, want [Bearer cookie-token-value]", got)
	}
}

func TestGatewayMetadata_HeaderTakesPrecedenceOverCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	r.Header.Set("Authorization", "Bearer header-value")
	r.AddCookie(&http.Cookie{Name: "token", Value: "cookie-value"})

	md := gatewayMetadata(r)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer header-value" {
		t.Errorf("authorization metadata = %v, want [Bearer header-value] (header should win over cookie)", got)
	}
}

func TestGatewayMetadata_ForwardsRequestID(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	r.Header.Set("Authorization", "Bearer abc123")
	r.Header.Set("X-Request-Id", "req-xyz")

	md := gatewayMetadata(r)
	if got := md.Get(grpchandler.RequestIDMetadataKey); len(got) != 1 || got[0] != "req-xyz" {
		t.Errorf("%s metadata = %v, want [req-xyz]", grpchandler.RequestIDMetadataKey, got)
	}
}

func TestGatewayMetadata_NoAuthButHasRequestID_StillReturnsIt(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/sevs", nil)
	r.Header.Set("X-Request-Id", "req-xyz")

	md := gatewayMetadata(r)
	if md == nil {
		t.Fatal("gatewayMetadata = nil, want metadata carrying the request ID")
	}
	if got := md.Get(grpchandler.RequestIDMetadataKey); len(got) != 1 || got[0] != "req-xyz" {
		t.Errorf("%s metadata = %v, want [req-xyz]", grpchandler.RequestIDMetadataKey, got)
	}
	if got := md.Get("authorization"); len(got) != 0 {
		t.Errorf("authorization metadata = %v, want empty", got)
	}
}
