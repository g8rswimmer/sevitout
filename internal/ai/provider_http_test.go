package ai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"

	"github.com/g8rswimmer/sevitout/internal/ai"
)

func TestHTTPProvider_Summarize(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["action"] != "summarize" {
			t.Errorf("expected action=summarize, got %v", req["action"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "handled externally"})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "shared-secret")
	got, err := p.Summarize(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001", Title: "outage"})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "handled externally" {
		t.Fatalf("unexpected result: %q", got)
	}
	if gotAuth != "Bearer shared-secret" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
}

func TestHTTPProvider_SuggestTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]string{{"title": "patch dependency", "priority": "critical"}},
		})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.SuggestTasks(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestTasks: %v", err)
	}
	if len(got) != 1 || got[0].Title != "patch dependency" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestHTTPProvider_DraftAnnouncement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["action"] != "draft_announcement" {
			t.Errorf("expected action=draft_announcement, got %v", req["action"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "external announcement draft"})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.DraftAnnouncement(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("DraftAnnouncement: %v", err)
	}
	if got != "external announcement draft" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestHTTPProvider_SuggestRootCause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"root_causes": []map[string]string{{"category": "deployment", "rationale": "bad rollout"}},
		})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.SuggestRootCause(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestRootCause: %v", err)
	}
	if len(got) != 1 || got[0].Category != "deployment" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestHTTPProvider_DraftPostmortem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"postmortem": map[string]string{"summary": "s", "action_items": "a"},
		})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.DraftPostmortem(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("DraftPostmortem: %v", err)
	}
	if got.Summary != "s" || got.ActionItems != "a" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestHTTPProvider_FindSimilar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"similar": []map[string]string{{"sev_id": "SEV-2025-0042", "title": "prior outage", "reason": "same root cause"}},
		})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.FindSimilar(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(got) != 1 || got[0].SEVID != "SEV-2025-0042" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestHTTPProvider_SuggestResponders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"responders": []map[string]string{{"role": "incident-commander", "rationale": "on-call"}},
		})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	got, err := p.SuggestResponders(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("SuggestResponders: %v", err)
	}
	if len(got) != 1 || got[0].Role != "incident-commander" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestHTTPProvider_ErrorFieldSurfacesAsGoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "upstream model unavailable"})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	_, err := p.Summarize(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err == nil {
		t.Fatal("expected an error when the response carries an error field")
	}
}

func TestHTTPProvider_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	_, err := p.Summarize(context.Background(), &ai.SEVContext{ID: "SEV-2026-0001"})
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestHTTPProvider_StreamAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "streamed result"})
	}))
	t.Cleanup(srv.Close)

	p := ai.NewHTTPProvider(srv.URL, "")
	ch, err := p.StreamAction(context.Background(), ai.ActionSummarize, &ai.SEVContext{ID: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("StreamAction: %v", err)
	}
	var last ai.Chunk
	for c := range ch {
		last = c
	}
	if !last.Done || last.Content != "streamed result" {
		t.Fatalf("unexpected final chunk: %+v", last)
	}
}
