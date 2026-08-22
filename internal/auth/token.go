package auth

import (
	"net/http"
	"strings"
)

// ExtractBearerToken pulls a JWT out of an HTTP request, preferring the
// Authorization header over the "token" httpOnly cookie set on login —
// the same precedence the REST gateway uses (see cmd/server/main.go's
// runtime.WithMetadata). Shared by any plain HTTP handler that needs to
// authenticate the same way the gRPC interceptor does (internal/api/ws.Handler,
// internal/api/grpc.IntegrationsHealthHandler).
func ExtractBearerToken(r *http.Request) string {
	if v := r.Header.Get("Authorization"); v != "" {
		after, ok := strings.CutPrefix(v, "Bearer ")
		if !ok {
			return ""
		}
		return after
	}
	if c, err := r.Cookie("token"); err == nil {
		return c.Value
	}
	return ""
}
