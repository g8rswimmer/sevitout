package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/ai"
)

func newAnthropicTestServer(t *testing.T, text string, wantStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("expected x-api-key header to be set")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("expected anthropic-version header to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(wantStatus)
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnthropicProvider_Summarize(t *testing.T) {
	srv := newAnthropicTestServer(t, "the database ran out of connections", http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.Summarize(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001", Title: "db outage"})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "the database ran out of connections" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestAnthropicProvider_SuggestRootCause_ParsesJSON(t *testing.T) {
	srv := newAnthropicTestServer(t, `[{"category":"deployment","rationale":"bad rollout"}]`, http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.SuggestRootCause(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestRootCause: %v", err)
	}
	if len(got) != 1 || got[0].Category != "deployment" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAnthropicProvider_SuggestRootCause_StripsMarkdownFence(t *testing.T) {
	srv := newAnthropicTestServer(t, "```json\n[{\"category\":\"hardware\",\"rationale\":\"disk failure\"}]\n```", http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.SuggestRootCause(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestRootCause: %v", err)
	}
	if len(got) != 1 || got[0].Category != "hardware" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAnthropicProvider_DraftPostmortem(t *testing.T) {
	srv := newAnthropicTestServer(t, `{"summary":"s","customer_impact":"c","timeline":"t","root_cause":"r","contributing_factors":"cf","lessons_learned":"l","action_items":"a"}`, http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.DraftPostmortem(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("DraftPostmortem: %v", err)
	}
	if got.Summary != "s" || got.ActionItems != "a" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestNewAnthropicProvider_DefaultsToRealBaseURL(t *testing.T) {
	// A trivial one-liner (delegates to NewAnthropicProviderWithBaseURL with
	// the real API's base URL), but it's the only constructor callers
	// outside this package actually use — confirms it doesn't panic and
	// returns a usable *AnthropicProvider satisfying Provider.
	p := ai.NewAnthropicProvider("test-key", "claude-sonnet-5")
	if p == nil {
		t.Fatal("NewAnthropicProvider returned nil")
	}
}

func TestAnthropicProvider_DraftAnnouncement(t *testing.T) {
	srv := newAnthropicTestServer(t, "We are investigating a database outage.", http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.DraftAnnouncement(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001", Title: "db outage"})
	if err != nil {
		t.Fatalf("DraftAnnouncement: %v", err)
	}
	if got != "We are investigating a database outage." {
		t.Fatalf("unexpected announcement: %q", got)
	}
}

func TestAnthropicProvider_SuggestTasks(t *testing.T) {
	srv := newAnthropicTestServer(t, `[{"title":"Add retry logic","description":"d","relationship_type":"action-item","priority":"critical"}]`, http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.SuggestTasks(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestTasks: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Add retry logic" || got[0].Priority != "critical" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAnthropicProvider_FindSimilar(t *testing.T) {
	srv := newAnthropicTestServer(t, `[{"sev_id":"SEV-2025-0042","title":"prior outage","reason":"same root cause"}]`, http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.FindSimilar(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(got) != 1 || got[0].SEVID != "SEV-2025-0042" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAnthropicProvider_SuggestResponders(t *testing.T) {
	srv := newAnthropicTestServer(t, `[{"role":"incident-commander","rationale":"on-call for affected service"}]`, http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	got, err := p.SuggestResponders(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestResponders: %v", err)
	}
	if len(got) != 1 || got[0].Role != "incident-commander" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAnthropicProvider_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "invalid x-api-key"}})
	}))
	t.Cleanup(srv.Close)
	p := ai.NewAnthropicProviderWithBaseURL("bad-key", "claude-sonnet-5", srv.URL)

	_, err := p.Summarize(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

func TestAnthropicProvider_StreamAction_FinalChunkCarriesFullContent(t *testing.T) {
	srv := newAnthropicTestServer(t, "one two three four five six seven eight nine ten eleven twelve thirteen", http.StatusOK)
	p := ai.NewAnthropicProviderWithBaseURL("test-key", "claude-sonnet-5", srv.URL)

	ch, err := p.StreamAction(context.Background(), ai.ActionSummarize, &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("StreamAction: %v", err)
	}
	var chunks int
	var last ai.Chunk
	for c := range ch {
		chunks++
		last = c
	}
	if chunks < 2 {
		t.Fatalf("expected at least 2 chunks (intermediate + final), got %d", chunks)
	}
	if !last.Done {
		t.Fatal("expected the last chunk to have Done=true")
	}
	if last.Content != "one two three four five six seven eight nine ten eleven twelve thirteen" {
		t.Fatalf("expected final chunk to carry the full text, got %q", last.Content)
	}
}
