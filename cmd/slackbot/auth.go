package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// tokenExpiryMargin is how far ahead of a cached token's actual expiry
// tokenSource treats it as already stale and refreshes proactively, so a
// call never races a token expiring mid-flight.
const tokenExpiryMargin = 5 * time.Minute

// tokenRefreshInterval is how often runTokenRefresher re-logs-in in the
// background, keeping the cached token far from expiry so request-handling
// goroutines essentially never hit tokenSource's own lazy refresh path.
// Declared as a var (not a const) so tests can shrink it.
var tokenRefreshInterval = 30 * time.Minute

// fallbackTokenTTL is the assumed lifetime of a token whose exp claim
// couldn't be decoded (which should never happen against a well-behaved
// server) — conservative enough that a refresh still eventually happens
// rather than a bad token being cached forever.
const fallbackTokenTTL = time.Hour

// tokenSource self-mints and refreshes the JWT the bot uses to authenticate
// to the API server, replacing a manually pre-issued, manually rotated
// SLACKBOT_SERVICE_TOKEN with a durable email/password login the bot
// performs itself. It implements credentials.PerRPCCredentials (attaching
// the current token to every gRPC call) and, via retryOnUnauthenticated, a
// grpc.UnaryClientInterceptor (forcing a fresh login and one retry on any
// call the server rejects as unauthenticated).
//
// Three independent layers keep the cached token valid:
//  1. GetRequestMetadata refreshes lazily whenever the cached token is
//     within tokenExpiryMargin of its own decoded expiry — the correctness
//     guarantee every call falls back on, including the very first one at
//     startup (no separate "wait for the API to be ready" retry loop needed;
//     whatever RPC goes out first just retries via its own existing logic,
//     e.g. loadSlackSettings).
//  2. runTokenRefresher proactively re-logs-in on a fixed interval well
//     under any reasonable token TTL, so layer 1 rarely has to block a live
//     request on a login round trip.
//  3. retryOnUnauthenticated is a safety net for anything layers 1–2 didn't
//     anticipate (e.g. the server's JWT_SECRET rotates, invalidating
//     outstanding tokens immediately regardless of their claimed expiry).
type tokenSource struct {
	httpClient *http.Client
	loginURL   string
	email      string
	password   string
	log        *slog.Logger

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// tokenSourceParams groups newTokenSource's dependencies.
type tokenSourceParams struct {
	// APIAddr is the same host:port the bot dials for gRPC — the API server
	// multiplexes gRPC and HTTP on one port (cmd/server/main.go's cmux
	// setup), so POST /auth/login is already reachable there without any
	// separate address configuration.
	APIAddr  string
	Email    string
	Password string
	Log      *slog.Logger // nil defaults to slog.Default()
}

// newTokenSource constructs a tokenSource.
func newTokenSource(p tokenSourceParams) *tokenSource {
	log := p.Log
	if log == nil {
		log = slog.Default()
	}
	return &tokenSource{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		loginURL:   "http://" + p.APIAddr + "/auth/login",
		email:      p.Email,
		password:   p.Password,
		log:        log,
	}
}

// GetRequestMetadata implements credentials.PerRPCCredentials, attaching the
// current (refreshed if necessary) token as a bearer authorization header.
func (t *tokenSource) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	token, err := t.Token(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + token}, nil
}

// RequireTransportSecurity implements credentials.PerRPCCredentials. The bot
// dials the API server with insecure.NewCredentials() (see main.go), so this
// matches that: false.
func (t *tokenSource) RequireTransportSecurity() bool { return false }

// Token returns a token guaranteed not to be within tokenExpiryMargin of
// expiry, logging in first if the cached one is missing or stale. Used both
// by GetRequestMetadata (for gRPC calls) and directly by the WebSocket event
// listener (wsclient.go), which needs a bearer token for its own handshake
// outside of gRPC.
func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == "" || time.Now().Add(tokenExpiryMargin).After(t.expiresAt) {
		if err := t.loginLocked(ctx); err != nil {
			return "", err
		}
	}
	return t.token, nil
}

// retryOnUnauthenticated is a grpc.UnaryClientInterceptor: it calls invoker
// once, and if the RPC failed because the server rejected the token as
// unauthenticated, forces a fresh login and retries exactly once more,
// returning whatever that second attempt yields either way.
func (t *tokenSource) retryOnUnauthenticated(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	err := invoker(ctx, method, req, reply, cc, opts...)
	if status.Code(err) != codes.Unauthenticated {
		return err
	}
	t.log.WarnContext(ctx, "rpc rejected as unauthenticated, forcing re-login and retrying once", "method", method)
	if loginErr := t.forceLogin(ctx); loginErr != nil {
		t.log.ErrorContext(ctx, "re-login after unauthenticated rpc failed", "method", method, "err", loginErr)
		return err
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

// forceLogin re-logs-in unconditionally, regardless of the cached token's
// apparent expiry — used by retryOnUnauthenticated, since the server having
// rejected the current token is more authoritative than our own guess at
// when it expires.
func (t *tokenSource) forceLogin(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loginLocked(ctx)
}

// runTokenRefresher periodically re-logs-in in the background until ctx is
// canceled. A failed refresh logs a warning and leaves the previously cached
// token in place — same fail-safe posture as runSettingsRefresher: a
// transient blip talking to the API server shouldn't discard a token that
// still works.
func (t *tokenSource) runTokenRefresher(ctx context.Context) {
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.forceLogin(ctx); err != nil {
				t.log.WarnContext(ctx, "periodic token refresh failed, keeping previous token", "err", err)
			}
		}
	}
}

// loginRequest/loginResponse mirror internal/auth/password.go's request and
// response JSON shapes. Declared locally (not imported from internal/auth)
// since cmd/slackbot only needs these two fields and talks to the API
// server exclusively over the network, never by importing its packages.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// loginLocked performs the login HTTP call and updates t.token/t.expiresAt.
// Callers must hold t.mu.
func (t *tokenSource) loginLocked(ctx context.Context) error {
	body, err := json.Marshal(loginRequest{Email: t.email, Password: t.password})
	if err != nil {
		return fmt.Errorf("marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.loginURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: server returned %s", resp.Status)
	}

	var loginResp loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	if loginResp.Token == "" {
		return errors.New("login response had no token")
	}

	t.token = loginResp.Token
	t.expiresAt = tokenExpiry(loginResp.Token, t.log)
	return nil
}

// jwtClaims is the subset of a JWT's payload this bot reads — just enough
// to know when to proactively refresh, without validating the token's
// signature (that's the server's job on every call; this is purely a
// client-side scheduling hint).
type jwtClaims struct {
	Exp int64 `json:"exp"`
}

// tokenExpiry decodes token's unverified exp claim. Any decode failure
// (malformed token, missing claim — neither of which a well-behaved server
// should ever produce) falls back to fallbackTokenTTL from now, so a
// refresh still eventually happens rather than the token being treated as
// eternally valid.
func tokenExpiry(token string, log *slog.Logger) time.Time {
	fallback := time.Now().Add(fallbackTokenTTL)

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		log.Warn("could not parse token expiry: not a 3-part JWT, using fallback TTL")
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		log.Warn("could not decode token payload, using fallback TTL", "err", err)
		return fallback
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		log.Warn("could not decode token exp claim, using fallback TTL", "err", err)
		return fallback
	}
	return time.Unix(claims.Exp, 0)
}
