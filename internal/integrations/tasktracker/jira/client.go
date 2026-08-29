// Package jira implements a minimal client for Jira Cloud's REST API v3,
// mirroring internal/integrations/tasktracker/github's client shape (see
// that package's doc comments for the pattern being followed here) closely
// enough that internal/api/grpc/task.go's CreateJiraIssue handler reads
// almost identically to its CreateGitHubIssue sibling.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiPath         = "/rest/api/3"
	requestTimeout  = 10 * time.Second
	maxErrorBodyLen = 4096

	// gatewayBaseURL is Atlassian's API gateway host: every Jira Cloud
	// tenant is reached at gatewayBaseURL + "/ex/jira/{cloudId}", not at
	// the tenant's own https://{site}.atlassian.net host directly — see
	// https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/.
	gatewayBaseURL = "https://api.atlassian.com/ex/jira"
)

// Issue is a Jira issue as returned by the client. URL is the REST API's own
// "self" link (the machine-readable resource URL, e.g.
// "https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3/issue/10007") —
// unlike github.Issue.HTMLURL, this isn't a human-clickable "browse" page:
// with only a cloudId (not the tenant's own https://{site}.atlassian.net
// host — the API gateway and the browsable web UI are different hosts, and
// the site name isn't derivable from cloudId alone), there's no browse link
// this client can construct. See CreateIssueRequest's caller
// (internal/api/grpc/task.go's CreateJiraIssue) for how that's surfaced.
type Issue struct {
	Key         string
	Summary     string
	Description string
	Status      string // e.g. "To Do", "In Progress", "Done" — project-defined, unlike GitHub's fixed open/closed.
	URL         string
}

// CreateIssueRequest describes a new Jira issue to create.
type CreateIssueRequest struct {
	ProjectKey  string
	IssueType   string // e.g. "Task", "Bug" — must already exist on the target project.
	Summary     string
	Description string
	Labels      []string
}

// APIError is returned when the Jira API responds with a non-success status
// code. Jira's error payload is shaped differently from GitHub's single
// "message" field — {"errorMessages": [...], "errors": {field: reason}} —
// so Messages is a slice; Error() joins them into one string for callers
// that just want a readable message.
type APIError struct {
	StatusCode int
	Messages   []string
}

func (e *APIError) Error() string {
	if len(e.Messages) > 0 {
		return fmt.Sprintf("jira: status %d: %s", e.StatusCode, strings.Join(e.Messages, "; "))
	}
	return fmt.Sprintf("jira: unexpected status %d", e.StatusCode)
}

// HTTPStatus returns the response status code, letting callers branch on it
// without depending on this package's concrete APIError type — mirrors
// github.APIError.HTTPStatus.
func (e *APIError) HTTPStatus() int { return e.StatusCode }

// Client calls the Jira Cloud REST API v3 for one tenant instance, via
// Atlassian's api.atlassian.com gateway rather than the tenant's own
// https://{site}.atlassian.net host directly.
type Client struct {
	baseURL  string
	apiToken string
	http     *http.Client
}

// NewClient returns a Client for the Jira Cloud tenant identified by
// cloudID, authenticating with apiToken as a Bearer token in the
// Authorization header. Per
// https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/,
// requests through the api.atlassian.com gateway use Bearer token auth, not
// HTTP Basic Auth — no account email is needed alongside the token. cloudID
// is the tenant's Cloud ID (a UUID, not its site name) — see that page for
// how to find it under admin.atlassian.com.
func NewClient(cloudID, apiToken string) *Client {
	return NewClientWithBaseURL(gatewayBaseURL+"/"+cloudID, apiToken)
}

// NewClientWithBaseURL returns a Client that uses a custom base URL instead
// of deriving one from a Cloud ID, intended for use in tests with
// httptest.Server — mirrors github.NewClientWithBaseURL.
func NewClientWithBaseURL(baseURL, apiToken string) *Client {
	return &Client{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		apiToken: apiToken,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// Ping verifies that apiToken is accepted by Jira, by calling the
// lightweight "who am I" endpoint every authenticated Jira Cloud user can
// reach regardless of project permissions. Used by the Configuration API's
// integration health check (see internal/api/grpc/integrations_health.go),
// mirroring github.Client.Ping.
func (c *Client) Ping(ctx context.Context) error {
	slog.DebugContext(ctx, "jira api call", "op", "Ping")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+apiPath+"/myself", nil)
	if err != nil {
		return fmt.Errorf("jira: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "jira api call failed", "op", "Ping", "err", err)
		return fmt.Errorf("jira: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "jira api call returned error", "op", "Ping", "status", resp.StatusCode)
		return apiErr
	}
	return nil
}

// GetIssue fetches a single Jira issue by its key (e.g. "PROJ-42").
func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	slog.DebugContext(ctx, "jira api call", "op", "GetIssue", "key", key)
	reqURL := fmt.Sprintf("%s%s/issue/%s", c.baseURL, apiPath, url.PathEscape(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jira: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "jira api call failed", "op", "GetIssue", "err", err)
		return nil, fmt.Errorf("jira: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "jira api call returned error", "op", "GetIssue", "status", resp.StatusCode)
		return nil, apiErr
	}
	return decodeIssue(resp.Body)
}

// CreateIssue creates a new Jira issue in the given project and returns the
// resulting issue. Jira's create-issue response only carries id/key/self —
// unlike GitHub's, which echoes the full created object — so the returned
// Issue's Summary/Description are req's own values, not read back from the
// API, and Status is left empty (a freshly created issue's initial status is
// project-workflow-defined; querying it would cost a second round trip this
// caller doesn't need).
func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	slog.DebugContext(ctx, "jira api call", "op", "CreateIssue", "project_key", req.ProjectKey, "issue_type", req.IssueType)

	fields := map[string]any{
		"project":   map[string]string{"key": req.ProjectKey},
		"issuetype": map[string]string{"name": req.IssueType},
		"summary":   req.Summary,
	}
	if req.Description != "" {
		fields["description"] = plainTextToADF(req.Description)
	}
	if len(req.Labels) > 0 {
		fields["labels"] = req.Labels
	}
	payload := map[string]any{"fields": fields}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jira: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+apiPath+"/issue", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jira: build request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		slog.WarnContext(ctx, "jira api call failed", "op", "CreateIssue", "err", err)
		return nil, fmt.Errorf("jira: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "jira api call returned error", "op", "CreateIssue", "status", resp.StatusCode)
		return nil, apiErr
	}

	var created struct {
		Key  string `json:"key"`
		Self string `json:"self"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("jira: decode response: %w", err)
	}

	issue := &Issue{
		Key:         created.Key,
		Summary:     req.Summary,
		Description: req.Description,
		URL:         created.Self,
	}
	slog.InfoContext(ctx, "jira issue created", "project_key", req.ProjectKey, "key", issue.Key, "url", issue.URL)
	return issue, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Accept", "application/json")
}

// issueDTO mirrors the subset of Jira's GET /issue/{key} response shape
// this client reads.
type issueDTO struct {
	Key    string `json:"key"`
	Self   string `json:"self"`
	Fields struct {
		Summary     string `json:"summary"`
		Description any    `json:"description"` // Atlassian Document Format (ADF), or null.
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
	} `json:"fields"`
}

func decodeIssue(r io.Reader) (*Issue, error) {
	var dto issueDTO
	if err := json.NewDecoder(r).Decode(&dto); err != nil {
		return nil, fmt.Errorf("jira: decode response: %w", err)
	}
	return &Issue{
		Key:         dto.Key,
		Summary:     dto.Fields.Summary,
		Description: adfToPlainText(dto.Fields.Description),
		Status:      dto.Fields.Status.Name,
		URL:         dto.Self,
	}, nil
}

// plainTextToADF wraps text in the minimal Atlassian Document Format
// structure Jira Cloud's REST API v3 requires for the description field —
// unlike v2, v3 rejects a bare string. A single paragraph is sufficient for
// the SEV-context text this client sends; it isn't a general Markdown/ADF
// converter.
func plainTextToADF(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": text},
				},
			},
		},
	}
}

// adfToPlainText extracts a best-effort plain-text rendering of an ADF
// description: every "text" node's content, in document order, paragraphs
// joined by blank lines. It's read-side only (GetIssue), for display — not
// a lossless inverse of plainTextToADF, since real Jira descriptions can
// carry formatting, mentions, and other node types this doesn't render.
// A nil/non-object description (Jira returns null when none is set) yields
// "".
func adfToPlainText(node any) string {
	obj, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	if obj["type"] == "text" {
		text, _ := obj["text"].(string)
		return text
	}
	content, _ := obj["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	var parts []string
	for _, child := range content {
		if s := adfToPlainText(child); s != "" {
			parts = append(parts, s)
		}
	}
	sep := ""
	if obj["type"] == "paragraph" {
		sep = "\n\n"
	}
	if sep == "" {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, sep)
}

// newAPIError builds an *APIError from a non-success response, attempting to
// extract Jira's own error messages so callers see the actual failure
// reason (invalid project key, missing required field, permission denied)
// rather than a bare status code.
func newAPIError(resp *http.Response) *APIError {
	limited := io.LimitReader(resp.Body, maxErrorBodyLen)
	raw, _ := io.ReadAll(limited)

	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(raw, &body)

	messages := body.ErrorMessages
	for field, reason := range body.Errors {
		messages = append(messages, fmt.Sprintf("%s: %s", field, reason))
	}
	return &APIError{StatusCode: resp.StatusCode, Messages: messages}
}
