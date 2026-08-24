package auth

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// UnaryInterceptor returns a gRPC unary interceptor that validates JWT tokens,
// checks the caller's active status in the store, and enforces RBAC.
func UnaryInterceptor(signer *JWTSigner, users store.UserStore) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := authenticate(ctx, signer, users, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamInterceptor returns a gRPC stream interceptor that validates JWT tokens,
// checks the caller's active status in the store, and enforces RBAC.
func StreamInterceptor(signer *JWTSigner, users store.UserStore) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authenticate(ss.Context(), signer, users, info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// authenticate rejects a call before grpchandler.LoggingUnaryInterceptor ever
// sees it (auth.UnaryInterceptor runs outermost — see cmd/server/main.go —
// specifically so a successful call's logRPC entry can read the *UserContext
// this function attaches to ctx). That means a rejection here would
// otherwise vanish entirely, so every failure branch below logs for itself
// at Warn: these are exactly the "why can't I log in / why is this call
// failing" cases the whole point of this work is to make visible.
func authenticate(ctx context.Context, signer *JWTSigner, users store.UserStore, method string) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		slog.WarnContext(ctx, "rpc rejected: missing metadata", "method", method)
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		slog.WarnContext(ctx, "rpc rejected: missing authorization header", "method", method)
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}
	raw := values[0]
	if !strings.HasPrefix(raw, "Bearer ") {
		slog.WarnContext(ctx, "rpc rejected: malformed authorization header", "method", method)
		return nil, status.Error(codes.Unauthenticated, "malformed authorization header")
	}
	tokenStr := raw[len("Bearer "):]

	claims, err := signer.Validate(tokenStr)
	if err != nil {
		slog.WarnContext(ctx, "rpc rejected: invalid or expired token", "method", method, "err", err)
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	user, err := users.Get(ctx, claims.Subject)
	if err != nil || !user.Active {
		slog.WarnContext(ctx, "rpc rejected: unknown or inactive user", "method", method, "user_id", claims.Subject)
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	uc := &UserContext{
		UserID:  claims.Subject,
		Email:   claims.Email,
		OrgRole: store.OrgRole(claims.OrgRole),
	}

	if !HasPermission(uc.OrgRole, method) {
		slog.WarnContext(ctx, "rpc rejected: insufficient permissions", "method", method, "user_id", uc.UserID, "org_role", string(uc.OrgRole))
		return nil, status.Error(codes.PermissionDenied, "insufficient permissions for "+method)
	}

	return WithUser(ctx, uc), nil
}

// wrappedStream replaces the context on a gRPC ServerStream.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
