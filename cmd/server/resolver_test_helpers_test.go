package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
)

// mustKey generates a random, valid AES-256-GCM key for tests that need a
// real crypto.KeyEncryptor — mirrors internal/store/crypto/crypto_test.go's
// unexported helper of the same name, which lives in a different package and
// so isn't reusable directly.
func mustKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// putIntegrationConfig upserts an integration config row into integrations,
// with creds (if non-nil) encrypted under enc using the same JSON-then-AES-
// 256-GCM encoding internal/api/grpc.ConfigServer.UpsertIntegrationConfig
// uses — so resolver tests exercise the real encrypt/decrypt path, not a
// stand-in for it.
func putIntegrationConfig(t *testing.T, integrations store.IntegrationConfigStore, enc grpchandler.Encryptor, integrationType string, creds map[string]string, settings map[string]any) {
	t.Helper()
	cfg := &store.IntegrationConfig{IntegrationType: integrationType, Settings: settings}
	if creds != nil {
		raw, err := json.Marshal(creds)
		if err != nil {
			t.Fatalf("marshal credentials: %v", err)
		}
		sealed, err := enc.Encrypt(raw)
		if err != nil {
			t.Fatalf("encrypt credentials: %v", err)
		}
		cfg.EncryptedCredentials = sealed
	}
	if err := integrations.Upsert(context.Background(), cfg); err != nil {
		t.Fatalf("upsert integration config: %v", err)
	}
}

// fakeOnCaller records whether it was invoked, letting resolver tests assert
// fallback behavior without a real PagerDuty HTTP call.
type fakeOnCaller struct {
	called bool
	name   string
	err    error
}

func (f *fakeOnCaller) OnCallLookup(_ context.Context, _ string) (string, error) {
	f.called = true
	return f.name, f.err
}

// fakeIssueClient records whether it was invoked, letting resolver tests
// assert fallback behavior without a real GitHub HTTP call.
type fakeIssueClient struct {
	called bool
	issue  *grpchandler.CreatedIssue
	err    error
}

func (f *fakeIssueClient) CreateIssue(_ context.Context, _, _, _, _ string, _ []string) (*grpchandler.CreatedIssue, error) {
	f.called = true
	return f.issue, f.err
}

// fakeJiraIssueClient is fakeIssueClient's JiraIssueClient-shaped twin.
type fakeJiraIssueClient struct {
	called bool
	issue  *grpchandler.CreatedIssue
	err    error
}

func (f *fakeJiraIssueClient) CreateIssue(_ context.Context, _, _, _, _ string, _ []string) (*grpchandler.CreatedIssue, error) {
	f.called = true
	return f.issue, f.err
}

var (
	_ grpchandler.OnCaller        = (*fakeOnCaller)(nil)
	_ grpchandler.IssueClient     = (*fakeIssueClient)(nil)
	_ grpchandler.JiraIssueClient = (*fakeJiraIssueClient)(nil)
)
