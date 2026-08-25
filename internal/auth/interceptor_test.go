package auth_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeServerStream is a minimal grpc.ServerStream whose only interesting
// behavior is the context it was constructed with — enough to drive
// StreamInterceptor without a real network connection.
type fakeServerStream struct {
	ctx context.Context
}

func (f *fakeServerStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeServerStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeServerStream) SetTrailer(metadata.MD)       {}
func (f *fakeServerStream) Context() context.Context     { return f.ctx }
func (f *fakeServerStream) SendMsg(m any) error          { return nil }
func (f *fakeServerStream) RecvMsg(m any) error          { return nil }

var _ grpc.ServerStream = (*fakeServerStream)(nil)

func seedActiveUser(t *testing.T, users store.UserStore, id string, role store.OrgRole) {
	t.Helper()
	now := time.Now()
	if err := users.Create(context.Background(), &store.User{
		ID: id, Email: id + "@example.com", Name: id, OrgRole: role, Active: true,
		PasswordHash: "x", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestStreamInterceptor_RejectsMissingMetadata(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	interceptor := auth.StreamInterceptor(signer, memory.NewUserStore())

	called := false
	handler := func(srv any, ss grpc.ServerStream) error {
		called = true
		return nil
	}
	info := &grpc.StreamServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}
	stream := &fakeServerStream{ctx: context.Background()}

	err := interceptor(nil, stream, info, handler)
	if err == nil {
		t.Fatal("want an error for a stream with no metadata")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
	if called {
		t.Error("handler should not be called when auth fails")
	}
}

func TestStreamInterceptor_RejectsInsufficientPermissions(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()
	seedActiveUser(t, users, "user-viewer", store.OrgRoleViewer)
	interceptor := auth.StreamInterceptor(signer, users)

	token, err := signer.Sign("user-viewer", "user-viewer@example.com", string(store.OrgRoleViewer))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	stream := &fakeServerStream{ctx: metadata.NewIncomingContext(context.Background(), md)}
	// Viewer may GetSEV but not CreateSEV (internal/auth/rbac_test.go covers
	// the full permission matrix; this just needs one denied combination).
	info := &grpc.StreamServerInfo{FullMethod: "/sevitout.v1.SEVService/CreateSEV"}

	called := false
	handler := func(srv any, ss grpc.ServerStream) error {
		called = true
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", status.Code(err))
	}
	if called {
		t.Error("handler should not be called when RBAC denies the call")
	}
}

func TestStreamInterceptor_AttachesUserAndCallsHandler(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()
	seedActiveUser(t, users, "user-responder", store.OrgRoleResponder)
	interceptor := auth.StreamInterceptor(signer, users)

	token, err := signer.Sign("user-responder", "user-responder@example.com", string(store.OrgRoleResponder))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	md := metadata.Pairs("authorization", "Bearer "+token)
	stream := &fakeServerStream{ctx: metadata.NewIncomingContext(context.Background(), md)}
	info := &grpc.StreamServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	var gotCtx context.Context
	handler := func(srv any, ss grpc.ServerStream) error {
		// Exercises wrappedStream.Context(): the interceptor must pass a
		// stream whose Context() returns the auth-populated context, not the
		// original stream's bare context.
		gotCtx = ss.Context()
		return nil
	}

	if err := interceptor(nil, stream, info, handler); err != nil {
		t.Fatalf("interceptor: %v", err)
	}

	uc, ok := auth.UserFromContext(gotCtx)
	if !ok {
		t.Fatal("UserFromContext: ok = false, want the authenticated user attached")
	}
	if uc.UserID != "user-responder" {
		t.Errorf("UserID = %q, want %q", uc.UserID, "user-responder")
	}
	if uc.OrgRole != store.OrgRoleResponder {
		t.Errorf("OrgRole = %q, want %q", uc.OrgRole, store.OrgRoleResponder)
	}
}
