package grpc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/share"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testShareView struct {
	ts     *httptest.Server
	shares *memory.ShareStore
	sevs   *memory.SEVStore
	anns   *memory.AnnouncementStore
	signer *share.Signer
}

func newTestShareView(t *testing.T) *testShareView {
	t.Helper()
	shares := memory.NewShareStore()
	sevs := memory.NewSEVStore()
	anns := memory.NewAnnouncementStore()
	signer := share.NewSigner("test-secret")
	handler := grpchandler.NewShareViewHandler(shares, sevs, anns, signer)

	mux := http.NewServeMux()
	mux.Handle("/s/{token}", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &testShareView{ts: srv, shares: shares, sevs: sevs, anns: anns, signer: signer}
}

// seedSharedSEV creates a SEV and a matching, valid ShareableLink for it,
// returning the token.
func (tv *testShareView) seedSharedSEV(t *testing.T, sensitive bool, expiresAt time.Time, revoked bool) (sevID, token string) {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:          "Public-facing outage",
		SeverityLevel:  1,
		Status:         store.SEVStatusMitigated,
		Sensitive:      sensitive,
		BusinessImpact: strPtr("5% of checkout requests failed"),
		StartedAt:      &now,
		CreatedBy:      "user-1",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tv.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("Create SEV: %v", err)
	}

	tok, err := tv.signer.Sign(sv.ID, expiresAt)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	link := &store.ShareableLink{
		SEVID:     sv.ID,
		Token:     tok,
		CreatedBy: "user-1",
		ExpiresAt: &expiresAt,
		CreatedAt: now,
	}
	if err := tv.shares.Create(context.Background(), link); err != nil {
		t.Fatalf("Create share link: %v", err)
	}
	if revoked {
		if err := tv.shares.Revoke(context.Background(), tok, "admin-1"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
	}
	return sv.ID, tok
}

func TestShareViewHandler_ValidToken(t *testing.T) {
	tv := newTestShareView(t)
	sevID, token := tv.seedSharedSEV(t, false, time.Now().Add(time.Hour), false)

	_ = tv.anns.Create(context.Background(), &store.Announcement{
		SEVID: sevID, AuthorID: "user-1", Message: "We are investigating", Audience: store.AudienceExternal, CreatedAt: time.Now(),
	})
	_ = tv.anns.Create(context.Background(), &store.Announcement{
		SEVID: sevID, AuthorID: "user-1", Message: "internal-only note", Audience: store.AudienceInternal, CreatedAt: time.Now(),
	})

	resp, err := http.Get(tv.ts.URL + "/s/" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if raw["id"] != sevID {
		t.Errorf("id = %v, want %v", raw["id"], sevID)
	}
	if raw["business_impact"] != "5% of checkout requests failed" {
		t.Errorf("business_impact = %v", raw["business_impact"])
	}
	anns, ok := raw["announcements"].([]any)
	if !ok || len(anns) != 1 {
		t.Fatalf("announcements = %v, want exactly 1 (external only)", raw["announcements"])
	}
	first := anns[0].(map[string]any)
	if first["message"] != "We are investigating" {
		t.Errorf("announcement message = %v, want external one", first["message"])
	}

	// Internal-only fields must not appear at all.
	for _, forbidden := range []string{"root_cause_category", "chat_log", "audit_log", "tags", "created_by", "mttr_seconds"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("public view leaked internal field %q", forbidden)
		}
	}
}

func TestShareViewHandler_UnknownToken(t *testing.T) {
	tv := newTestShareView(t)
	resp, err := http.Get(tv.ts.URL + "/s/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestShareViewHandler_Revoked(t *testing.T) {
	tv := newTestShareView(t)
	_, token := tv.seedSharedSEV(t, false, time.Now().Add(time.Hour), true)

	resp, err := http.Get(tv.ts.URL + "/s/" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Errorf("status = %d, want 410 for revoked link", resp.StatusCode)
	}
}

func TestShareViewHandler_Expired(t *testing.T) {
	tv := newTestShareView(t)
	_, token := tv.seedSharedSEV(t, false, time.Now().Add(-time.Hour), false)

	resp, err := http.Get(tv.ts.URL + "/s/" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Errorf("status = %d, want 410 for expired link", resp.StatusCode)
	}
}

func TestShareViewHandler_SensitiveSEVBlocked(t *testing.T) {
	tv := newTestShareView(t)
	// Simulate a SEV flagged Sensitive after its link was already created
	// (CreateShareLink itself would refuse to mint one) to exercise the
	// handler's own defense-in-depth check.
	_, token := tv.seedSharedSEV(t, true, time.Now().Add(time.Hour), false)

	resp, err := http.Get(tv.ts.URL + "/s/" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for sensitive SEV", resp.StatusCode)
	}
}

func TestShareViewHandler_MethodNotAllowed(t *testing.T) {
	tv := newTestShareView(t)
	_, token := tv.seedSharedSEV(t, false, time.Now().Add(time.Hour), false)

	resp, err := http.Post(tv.ts.URL+"/s/"+token, "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
