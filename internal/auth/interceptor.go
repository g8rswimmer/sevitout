package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
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
// sees it (auth.UnaryInterceptor runs inside RequestIDUnaryInterceptor but
// outside LoggingUnaryInterceptor — see cmd/server/main.go — specifically so
// a successful call's logRPC entry can read the *UserContext this function
// attaches to ctx). That means a rejection here would otherwise vanish
// entirely — from metrics as well as logs, since logRPC is also where
// telemetry.RPCRequestsTotal/RPCDurationSeconds are recorded — so every
// failure branch below both logs for itself at Warn (these are exactly the
// "why can't I log in / why is this call failing" cases the whole point of
// Phase 1 was to make visible) and records the same two metrics via reject
// below, so a spike in auth failures shows up on a dashboard exactly like a
// spike in any other error code would. Each log line also carries
// request_id when RequestIDUnaryInterceptor ran first (it does in the real
// chain, being outermost), so a rejected call can still be correlated with
// whatever else that request touched.
func authenticate(ctx context.Context, signer *JWTSigner, users store.UserStore, method string) (context.Context, error) {
	start := time.Now()
	attrs := []any{"method", method}
	if reqID, ok := telemetry.RequestIDFromContext(ctx); ok {
		attrs = append(attrs, "request_id", reqID)
	}
	warn := func(msg string, extra ...any) {
		slog.WarnContext(ctx, msg, append(attrs, extra...)...)
	}
	reject := func(code codes.Code, statusMsg string) error {
		telemetry.RPCRequestsTotal.WithLabelValues(method, code.String()).Inc()
		telemetry.RPCDurationSeconds.WithLabelValues(method, code.String()).Observe(time.Since(start).Seconds())
		return status.Error(code, statusMsg)
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		warn("rpc rejected: missing metadata")
		return nil, reject(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		warn("rpc rejected: missing authorization header")
		return nil, reject(codes.Unauthenticated, "missing authorization header")
	}
	raw := values[0]
	if !strings.HasPrefix(raw, "Bearer ") {
		warn("rpc rejected: malformed authorization header")
		return nil, reject(codes.Unauthenticated, "malformed authorization header")
	}
	tokenStr := raw[len("Bearer "):]

	claims, err := signer.Validate(tokenStr)
	if err != nil {
		warn("rpc rejected: invalid or expired token", "err", err)
		return nil, reject(codes.Unauthenticated, "invalid or expired token")
	}

	user, err := users.Get(ctx, claims.Subject)
	if err != nil || !user.Active {
		warn("rpc rejected: unknown or inactive user", "user_id", claims.Subject)
		return nil, reject(codes.Unauthenticated, "invalid or expired token")
	}

	uc := &UserContext{
		UserID:  claims.Subject,
		Email:   claims.Email,
		OrgRole: store.OrgRole(claims.OrgRole),
	}

	if !HasPermission(uc.OrgRole, method) {
		warn("rpc rejected: insufficient permissions", "user_id", uc.UserID, "org_role", string(uc.OrgRole))
		return nil, reject(codes.PermissionDenied, "insufficient permissions for "+method)
	}

	return WithUser(ctx, uc), nil
}

// wrappedStream replaces the context on a gRPC ServerStream.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
