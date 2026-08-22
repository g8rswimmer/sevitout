package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
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

func TestPing_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rate_limit" {
			t.Errorf("path = %q, want /rate_limit", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestPing_ErrorOnNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("bad-token", srv.URL)
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping should error on a non-200 response")
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
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["title"] != "SEV issue" {
			t.Errorf("title: got %v, want %q", body["title"], "SEV issue")
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
	issue, err := c.CreateIssue(context.Background(), github.CreateIssueRequest{
		Owner: "owner", Repo: "repo", Title: "SEV issue", Body: "details",
	})
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

func TestCreateIssue_SendsLabels(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "https://github.com/owner/repo/issues/1"})
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.CreateIssue(context.Background(), github.CreateIssueRequest{
		Owner:  "owner",
		Repo:   "repo",
		Title:  "SEV issue",
		Body:   "details",
		Labels: []string{"SEV-2026-0009", "critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	labels, _ := gotBody["labels"].([]any)
	if len(labels) != 2 || labels[0] != "SEV-2026-0009" || labels[1] != "critical" {
		t.Errorf("labels: got %v, want [SEV-2026-0009 critical]", gotBody["labels"])
	}
}

func TestCreateIssue_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.CreateIssue(context.Background(), github.CreateIssueRequest{Owner: "owner", Repo: "repo", Title: "title", Body: "body"})
	if err == nil {
		t.Fatal("expected error for non-201 status, got nil")
	}
}

func TestCreateIssue_UnexpectedStatus_SurfacesAPIMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Resource not accessible by personal access token"})
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.CreateIssue(context.Background(), github.CreateIssueRequest{Owner: "owner", Repo: "repo", Title: "title", Body: "body"})

	var apiErr *github.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *github.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("status code: got %d, want %d", apiErr.StatusCode, http.StatusForbidden)
	}
	if apiErr.Message != "Resource not accessible by personal access token" {
		t.Errorf("message: got %q, want the GitHub-provided message", apiErr.Message)
	}
}

func TestCreateIssue_EscapesOwnerAndRepo(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "https://github.com/x/y/issues/1"})
	}))
	defer srv.Close()

	c := github.NewClientWithBaseURL("test-token", srv.URL)
	_, err := c.CreateIssue(context.Background(), github.CreateIssueRequest{
		Owner: "foo/../bar", Repo: "repo?evil=1", Title: "title", Body: "body",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/repos/foo%2F..%2Fbar/repo%3Fevil=1/issues"
	if gotPath != want {
		t.Errorf("request path: got %q, want %q (owner/repo must be escaped, not interpreted as path separators)", gotPath, want)
	}
	if gotQuery != "" {
		t.Errorf("request query: got %q, want empty (repo's literal \"?\" must not start a query string)", gotQuery)
	}
}
