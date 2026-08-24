package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeJWT builds a syntactically valid 3-segment JWT with the given exp
// claim (as a Unix timestamp) in its payload segment. The header and
// signature segments are placeholders — tokenExpiry never validates a
// signature, it only decodes the unverified payload.
func fakeJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(jwtClaims{Exp: exp.Unix()})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// loginServer stands in for POST /auth/login. Each call increments hits;
// the response token/status are set per test.
type loginServer struct {
	*httptest.Server
	hits  atomic.Int32
	token string
	// failNextN causes the next N logins to fail with 401 before token is
	// returned successfully.
	failNextN atomic.Int32
}

func newLoginServer(t *testing.T, initialToken string) *loginServer {
	t.Helper()
	ls := &loginServer{token: initialToken}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ls.hits.Add(1)
		if r.URL.Path != "/auth/login" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req loginRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if ls.failNextN.Load() > 0 {
			ls.failNextN.Add(-1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loginResponse{Token: ls.token})
	}))
	t.Cleanup(ls.Close)
	return ls
}

// newTestTokenSource builds a tokenSource pointed at ls, bypassing
// newTokenSource's "http://"+addr URL construction so the httptest.Server's
// own URL (including its random port) is used directly.
func newTestTokenSource(ls *loginServer) *tokenSource {
	ts := newTokenSource(tokenSourceParams{APIAddr: "unused", Email: "svc@example.com", Password: "hunter2"})
	ts.loginURL = ls.URL + "/auth/login"
	return ts
}

func TestTokenSource_GetRequestMetadata_LogsInOnFirstUse(t *testing.T) {
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	md, err := ts.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if md["authorization"] != "Bearer "+ls.token {
		t.Errorf("authorization = %q, want Bearer %s", md["authorization"], ls.token)
	}
	if got := ls.hits.Load(); got != 1 {
		t.Errorf("login hits = %d, want 1", got)
	}
}

func TestTokenSource_GetRequestMetadata_FailedLoginSurfacesError(t *testing.T) {
	ls := newLoginServer(t, "")
	ls.failNextN.Store(1)
	ts := newTestTokenSource(ls)

	if _, err := ts.GetRequestMetadata(context.Background()); err == nil {
		t.Fatal("expected an error when login fails")
	}
}

func TestTokenSource_GetRequestMetadata_ReusesUnexpiredToken(t *testing.T) {
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	if _, err := ts.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := ts.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := ls.hits.Load(); got != 1 {
		t.Errorf("login hits = %d, want 1 (second call should reuse the cached token)", got)
	}
}

func TestTokenSource_GetRequestMetadata_RefreshesWhenNearExpiry(t *testing.T) {
	// Expires within tokenExpiryMargin of "now" — must trigger a re-login.
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(tokenExpiryMargin/2)))
	ts := newTestTokenSource(ls)

	if _, err := ts.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := ts.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := ls.hits.Load(); got != 2 {
		t.Errorf("login hits = %d, want 2 (token was within the expiry margin)", got)
	}
}

func TestTokenExpiry_MalformedTokenFallsBackToDefaultTTL(t *testing.T) {
	before := time.Now().Add(fallbackTokenTTL)
	got := tokenExpiry("not-a-jwt", slog.Default())
	after := time.Now().Add(fallbackTokenTTL)
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("got %v, want close to now+%v", got, fallbackTokenTTL)
	}
}

func TestRetryOnUnauthenticated_ReLoginsAndRetriesOnce(t *testing.T) {
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	calls := 0
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		calls++
		if calls == 1 {
			return status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		return nil
	}

	err := ts.retryOnUnauthenticated(context.Background(), "/pkg.Service/Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("retryOnUnauthenticated: %v", err)
	}
	if calls != 2 {
		t.Errorf("invoker called %d times, want 2 (original + one retry)", calls)
	}
	if got := ls.hits.Load(); got != 1 {
		t.Errorf("login hits = %d, want 1 (forced re-login before the retry)", got)
	}
}

func TestRetryOnUnauthenticated_GivesUpAfterSecondFailure(t *testing.T) {
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	calls := 0
	wantErr := status.Error(codes.Unauthenticated, "still no good")
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		calls++
		return wantErr
	}

	err := ts.retryOnUnauthenticated(context.Background(), "/pkg.Service/Method", nil, nil, nil, invoker)
	if !errors.Is(err, wantErr) && status.Code(err) != codes.Unauthenticated {
		t.Errorf("err = %v, want the second failure's Unauthenticated error", err)
	}
	if calls != 2 {
		t.Errorf("invoker called %d times, want exactly 2 (no infinite retry loop)", calls)
	}
}

func TestRetryOnUnauthenticated_NonAuthErrorIsNotRetried(t *testing.T) {
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	calls := 0
	wantErr := status.Error(codes.InvalidArgument, "bad request")
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		calls++
		return wantErr
	}

	err := ts.retryOnUnauthenticated(context.Background(), "/pkg.Service/Method", nil, nil, nil, invoker)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("err = %v, want the original InvalidArgument error unchanged", err)
	}
	if calls != 1 {
		t.Errorf("invoker called %d times, want 1 (non-auth errors aren't retried)", calls)
	}
	if got := ls.hits.Load(); got != 0 {
		t.Errorf("login hits = %d, want 0 (no re-login for a non-auth error)", got)
	}
}

func withFastTokenRefresh(t *testing.T, interval time.Duration) {
	t.Helper()
	orig := tokenRefreshInterval
	tokenRefreshInterval = interval
	t.Cleanup(func() { tokenRefreshInterval = orig })
}

func TestRunTokenRefresher_AppliesRefreshedToken(t *testing.T) {
	withFastTokenRefresh(t, 10*time.Millisecond)
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ts.runTokenRefresher(ctx)

	deadline := time.After(time.Second)
	for {
		if ls.hits.Load() >= 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("runTokenRefresher never logged in")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunTokenRefresher_FailedRefreshKeepsPreviousToken(t *testing.T) {
	withFastTokenRefresh(t, 10*time.Millisecond)
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)
	if _, err := ts.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("initial login: %v", err)
	}
	original := ts.token
	ls.failNextN.Store(1000) // every subsequent refresh fails

	ctx, cancel := context.WithCancel(context.Background())
	go ts.runTokenRefresher(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	ts.mu.Lock()
	got := ts.token
	ts.mu.Unlock()
	if got != original {
		t.Errorf("token = %q, want it unchanged (%q) after failed refreshes", got, original)
	}
}

func TestRunTokenRefresher_StopsOnContextCancel(t *testing.T) {
	withFastTokenRefresh(t, 10*time.Millisecond)
	ls := newLoginServer(t, fakeJWT(t, time.Now().Add(time.Hour)))
	ts := newTestTokenSource(ls)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ts.runTokenRefresher(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runTokenRefresher did not return promptly after ctx was canceled")
	}
}
