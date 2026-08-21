package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/github"
)

func issueHandler(t *testing.T, number int, title, state string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":   number,
			"title":    title,
			"body":     "issue body",
			"state":    state,
			"html_url": "https://github.com/owner/repo/issues/1",
		})
	}
}

func TestGetIssue(t *testing.T) {
	srv := httptest.NewServer(issueHandler(t, 42, "Test Issue", "open"))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	issue, err := c.GetIssue(context.Background(), "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Number != 42 {
		t.Errorf("number: got %d, want 42", issue.Number)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("title: got %q, want %q", issue.Title, "Test Issue")
	}
	if issue.State != "open" {
		t.Errorf("state: got %q, want %q", issue.State, "open")
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.GetIssue(context.Background(), "owner", "repo", 999)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestCreateIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["title"] != "SEV issue" {
			t.Errorf("title: got %q, want %q", body["title"], "SEV issue")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":   7,
			"title":    body["title"],
			"body":     body["body"],
			"state":    "open",
			"html_url": "https://github.com/owner/repo/issues/7",
		})
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	issue, err := c.CreateIssue(context.Background(), "owner", "repo", "SEV issue", "details")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Number != 7 {
		t.Errorf("number: got %d, want 7", issue.Number)
	}
	if issue.State != "open" {
		t.Errorf("state: got %q, want open", issue.State)
	}
}

func TestCreateIssue_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.CreateIssue(context.Background(), "owner", "repo", "title", "body")
	if err == nil {
		t.Fatal("expected error for non-201 status, got nil")
	}
}
