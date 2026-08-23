package grpc_test

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

func TestCreateAIPlugin_EncryptsAPIKey(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()

	resp, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{
		Name:        "anthropic",
		Version:     "1.0.0",
		HandlerType: "builtin",
		Provider:    "anthropic",
		Model:       "claude-sonnet-5",
		ApiKey:      "sk-super-secret",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}
	if !resp.GetApiKeyConfigured() {
		t.Error("api_key_configured should be true")
	}
	if bytes.Contains([]byte(resp.String()), []byte("sk-super-secret")) {
		t.Fatal("response leaked the plaintext API key")
	}

	stored, err := ts.aiPlugins.Get(ctx, resp.GetId())
	if err != nil {
		t.Fatalf("Get from store: %v", err)
	}
	if bytes.Contains(stored.EncryptedAPIKey, []byte("sk-super-secret")) {
		t.Fatal("store holds the plaintext API key, not ciphertext")
	}
}

func TestCreateAIPlugin_MissingName(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	_, err := ts.server.CreateAIPlugin(context.Background(), &pb.CreateAIPluginRequest{HandlerType: "builtin"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAIPlugin_InvalidHandlerType(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	_, err := ts.server.CreateAIPlugin(context.Background(), &pb.CreateAIPluginRequest{Name: "p", HandlerType: "carrier-pigeon"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAIPlugin_NoEncryptorConfiguredWithAPIKey(t *testing.T) {
	ts := newTestConfigServer(nil) // no ENCRYPTION_KEY
	_, err := ts.server.CreateAIPlugin(context.Background(), &pb.CreateAIPluginRequest{
		Name: "p", HandlerType: "builtin", ApiKey: "secret",
	})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %v", grpcCode(err))
	}
}

func TestCreateAIPlugin_NoAPIKeyWorksWithoutEncryptor(t *testing.T) {
	ts := newTestConfigServer(nil) // no ENCRYPTION_KEY, but no api_key either
	resp, err := ts.server.CreateAIPlugin(context.Background(), &pb.CreateAIPluginRequest{
		Name: "http-plugin", HandlerType: "http", HttpEndpoint: "https://example.com/ai",
	})
	if err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}
	if resp.GetApiKeyConfigured() {
		t.Error("api_key_configured should be false when no key was supplied")
	}
}

func TestCreateAIPlugin_DuplicateName(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	ctx := context.Background()
	req := &pb.CreateAIPluginRequest{Name: "dup", HandlerType: "builtin"}
	if _, err := ts.server.CreateAIPlugin(ctx, req); err != nil {
		t.Fatalf("first CreateAIPlugin: %v", err)
	}
	_, err := ts.server.CreateAIPlugin(ctx, req)
	if grpcCode(err) != codes.AlreadyExists {
		t.Fatalf("want AlreadyExists, got %v", grpcCode(err))
	}
}

func TestGetAIPlugin_NotFound(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	_, err := ts.server.GetAIPlugin(context.Background(), &pb.GetAIPluginRequest{Id: 999})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", grpcCode(err))
	}
}

func TestUpdateAIPlugin_PartialUpdate(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	ctx := context.Background()
	created, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{
		Name: "p", HandlerType: "builtin", Enabled: false, TriggerOnOpen: true,
	})
	if err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}

	updated, err := ts.server.UpdateAIPlugin(ctx, &pb.UpdateAIPluginRequest{
		Id:      created.GetId(),
		Enabled: wrapperspb.Bool(true),
	})
	if err != nil {
		t.Fatalf("UpdateAIPlugin: %v", err)
	}
	if !updated.GetEnabled() {
		t.Error("Enabled should now be true")
	}
	if !updated.GetTriggerOnOpen() {
		t.Error("TriggerOnOpen should be left unchanged (true)")
	}
	if updated.GetName() != "p" {
		t.Errorf("Name should be left unchanged, got %q", updated.GetName())
	}
}

func TestUpdateAIPlugin_ReplacesAPIKey(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()
	created, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{
		Name: "p", HandlerType: "builtin", ApiKey: "old-key",
	})
	if err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}
	before, _ := ts.aiPlugins.Get(ctx, created.GetId())

	if _, err := ts.server.UpdateAIPlugin(ctx, &pb.UpdateAIPluginRequest{Id: created.GetId(), ApiKey: "new-key"}); err != nil {
		t.Fatalf("UpdateAIPlugin: %v", err)
	}
	after, _ := ts.aiPlugins.Get(ctx, created.GetId())
	if bytes.Equal(before.EncryptedAPIKey, after.EncryptedAPIKey) {
		t.Fatal("expected the encrypted API key to change")
	}
}

func TestDeleteAIPlugin(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	ctx := context.Background()
	created, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{Name: "p", HandlerType: "builtin"})
	if err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}

	if _, err := ts.server.DeleteAIPlugin(ctx, &pb.DeleteAIPluginRequest{Id: created.GetId()}); err != nil {
		t.Fatalf("DeleteAIPlugin: %v", err)
	}
	if _, err := ts.server.GetAIPlugin(ctx, &pb.GetAIPluginRequest{Id: created.GetId()}); grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound after delete, got %v", grpcCode(err))
	}
}

func TestListAIPlugins(t *testing.T) {
	ts := newTestConfigServer(testEncryptor(t))
	ctx := context.Background()
	if _, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{Name: "a", HandlerType: "builtin"}); err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}
	if _, err := ts.server.CreateAIPlugin(ctx, &pb.CreateAIPluginRequest{Name: "b", HandlerType: "http", HttpEndpoint: "https://example.com"}); err != nil {
		t.Fatalf("CreateAIPlugin: %v", err)
	}

	resp, err := ts.server.ListAIPlugins(ctx, &pb.ListAIPluginsRequest{})
	if err != nil {
		t.Fatalf("ListAIPlugins: %v", err)
	}
	if len(resp.GetPlugins()) != 2 {
		t.Fatalf("got %d plugins, want 2", len(resp.GetPlugins()))
	}
}
