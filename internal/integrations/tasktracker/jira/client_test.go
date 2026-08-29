package jira_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/jira"
)

func TestPing_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("path = %q, want /rest/api/3/myself", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestPing_ErrorOnNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "bad-token")
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping should error on a non-200 response")
	}
}

func TestGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-42" {
			t.Errorf("path = %q, want /rest/api/3/issue/PROJ-42", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":  "PROJ-42",
			"self": "https://api.atlassian.com/ex/jira/some-cloud-id/rest/api/3/issue/10042",
			"fields": map[string]any{
				"summary": "Test Issue",
				"status":  map[string]any{"name": "In Progress"},
				"description": map[string]any{
					"type": "doc", "version": 1,
					"content": []any{
						map[string]any{
							"type": "paragraph",
							"content": []any{
								map[string]any{"type": "text", "text": "issue body"},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	issue, err := c.GetIssue(context.Background(), "PROJ-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Key != "PROJ-42" {
		t.Errorf("key: got %q, want PROJ-42", issue.Key)
	}
	if issue.Summary != "Test Issue" {
		t.Errorf("summary: got %q, want %q", issue.Summary, "Test Issue")
	}
	if issue.Status != "In Progress" {
		t.Errorf("status: got %q, want %q", issue.Status, "In Progress")
	}
	if issue.Description != "issue body" {
		t.Errorf("description: got %q, want %q", issue.Description, "issue body")
	}
	wantURL := "https://api.atlassian.com/ex/jira/some-cloud-id/rest/api/3/issue/10042"
	if issue.URL != wantURL {
		t.Errorf("url: got %q, want the API's own self link %q", issue.URL, wantURL)
	}
}

func TestGetIssue_NullDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":    "PROJ-1",
			"fields": map[string]any{"summary": "no description", "status": map[string]any{"name": "To Do"}},
		})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	issue, err := c.GetIssue(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Description != "" {
		t.Errorf("description: got %q, want empty for a null ADF field", issue.Description)
	}
}

func TestGetIssue_EscapesKey(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "PROJ-1", "fields": map[string]any{}})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	if _, err := c.GetIssue(context.Background(), "PROJ-1/../evil"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/rest/api/3/issue/PROJ-1%2F..%2Fevil"
	if gotPath != want {
		t.Errorf("request path: got %q, want %q (issue key must be escaped, not interpreted as path separators)", gotPath, want)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.GetIssue(context.Background(), "PROJ-999")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestCreateIssue(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("path: got %q, want /rest/api/3/issue", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "10007", "key": "PROJ-7", "self": selfURL(),
		})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	issue, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{
		ProjectKey: "PROJ", IssueType: "Task", Summary: "SEV follow-up", Description: "details",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Key != "PROJ-7" {
		t.Errorf("key: got %q, want PROJ-7", issue.Key)
	}
	if issue.URL != selfURL() {
		t.Errorf("url: got %q, want the response's own self link %q", issue.URL, selfURL())
	}
	if issue.Summary != "SEV follow-up" {
		t.Errorf("summary: got %q, want the request's own summary (Jira's create response doesn't echo it)", issue.Summary)
	}

	fields, _ := gotBody["fields"].(map[string]any)
	project, _ := fields["project"].(map[string]any)
	if project["key"] != "PROJ" {
		t.Errorf("request fields.project.key = %v, want PROJ", project["key"])
	}
	issuetype, _ := fields["issuetype"].(map[string]any)
	if issuetype["name"] != "Task" {
		t.Errorf("request fields.issuetype.name = %v, want Task", issuetype["name"])
	}
	if fields["summary"] != "SEV follow-up" {
		t.Errorf("request fields.summary = %v, want %q", fields["summary"], "SEV follow-up")
	}
	desc, _ := fields["description"].(map[string]any)
	if desc["type"] != "doc" {
		t.Errorf("request fields.description must be ADF (type=doc), got %v", fields["description"])
	}
}

func TestCreateIssue_OmitsDescriptionWhenEmpty(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "PROJ-1"})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{
		ProjectKey: "PROJ", IssueType: "Task", Summary: "no description",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if _, present := fields["description"]; present {
		t.Errorf("fields.description should be omitted when Description is empty, got %v", fields["description"])
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
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "PROJ-1"})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{
		ProjectKey: "PROJ", IssueType: "Task", Summary: "s",
		Labels: []string{"SEV-2026-0009", "critical"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields, _ := gotBody["fields"].(map[string]any)
	labels, _ := fields["labels"].([]any)
	if len(labels) != 2 || labels[0] != "SEV-2026-0009" || labels[1] != "critical" {
		t.Errorf("labels: got %v, want [SEV-2026-0009 critical]", fields["labels"])
	}
}

func TestCreateIssue_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{ProjectKey: "PROJ", IssueType: "Task", Summary: "s"})
	if err == nil {
		t.Fatal("expected error for non-201 status, got nil")
	}
}

func TestCreateIssue_NonJSONErrorBody_SurfacedVerbatim(t *testing.T) {
	// The api.atlassian.com gateway returns a 404 with a plain-text/HTML
	// body (not Jira's errorMessages/errors JSON shape) when a request
	// never reaches Jira's own handler at all — e.g. an invalid Cloud ID.
	// That's a materially different, more useful signal than a bare
	// "unexpected status 404", so it must survive into APIError.Messages
	// rather than being silently discarded by the JSON parse failing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><body>Not Found</body></html>"))
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{ProjectKey: "PROJ", IssueType: "Task", Summary: "s"})

	var apiErr *jira.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *jira.APIError, got %T: %v", err, err)
	}
	if len(apiErr.Messages) != 1 || apiErr.Messages[0] != "<html><body>Not Found</body></html>" {
		t.Errorf("Messages = %v, want the raw body surfaced verbatim", apiErr.Messages)
	}
}

func TestCreateIssue_EmptyErrorBody_NoMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{ProjectKey: "PROJ", IssueType: "Task", Summary: "s"})

	var apiErr *jira.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *jira.APIError, got %T: %v", err, err)
	}
	if len(apiErr.Messages) != 0 {
		t.Errorf("Messages = %v, want empty for a genuinely empty body", apiErr.Messages)
	}
	if err.Error() != "jira: unexpected status 404" {
		t.Errorf("Error() = %q, want the generic fallback", err.Error())
	}
}

func TestCreateIssue_UnexpectedStatus_SurfacesAPIMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errorMessages": []string{"project key is required"},
			"errors":        map[string]string{"issuetype": "issue type is required"},
		})
	}))
	defer srv.Close()

	c := jira.NewClientWithBaseURL(srv.URL, "test-token")
	_, err := c.CreateIssue(context.Background(), jira.CreateIssueRequest{Summary: "s"})

	var apiErr *jira.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *jira.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status code: got %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if len(apiErr.Messages) != 2 {
		t.Errorf("messages: got %v, want 2 (one from errorMessages, one from errors)", apiErr.Messages)
	}
}

// selfURL is a fixed "self" link value used by TestCreateIssue's mock
// server response — CreateIssue now reads this field directly as the
// returned Issue.URL (see Issue's doc comment: with only a cloudId, this
// client can't construct a human "browse" link the way it could when a
// full site base URL was configured directly).
func selfURL() string {
	return "https://api.atlassian.com/ex/jira/some-cloud-id/rest/api/3/issue/10007"
}
