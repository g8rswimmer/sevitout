package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL   = "https://api.github.com"
	requestTimeout   = 10 * time.Second
	githubAPIVersion = "2022-11-28"
	maxErrorBodyLen  = 4096
)

// Issue is a GitHub Issue as returned by the client.
type Issue struct {
	Number  int
	Title   string
	Body    string
	State   string // "open" or "closed"
	HTMLURL string
}

// CreateIssueRequest describes a new GitHub Issue to create.
type CreateIssueRequest struct {
	Owner  string
	Repo   string
	Title  string
	Body   string
	Labels []string
	// Assignees are GitHub logins to assign the new issue to (docs/roadmap.md
	// Phase 10f). Empty creates the issue unassigned.
	Assignees []string
}

// APIError is returned when the GitHub API responds with a non-success
// status code. StatusCode and Message let callers distinguish client errors
// (401/403/404/422) from genuine server-side failures.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("github: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("github: unexpected status %d", e.StatusCode)
}

// HTTPStatus returns the response status code, letting callers branch on it
// without depending on this package's concrete APIError type.
func (e *APIError) HTTPStatus() int { return e.StatusCode }

// Client calls the GitHub REST API v3.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient returns a Client that authenticates with token against the GitHub
// production API.
func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// NewClientWithBaseURL returns a Client that uses a custom base URL, intended
// for use in tests with httptest.Server.
func NewClientWithBaseURL(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// GetIssue fetches a single GitHub Issue by owner, repo, and issue number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	slog.DebugContext(ctx, "github api call", "op", "GetIssue", "owner", owner, "repo", repo, "number", number)
	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.baseURL, url.PathEscape(owner), url.PathEscape(repo), number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "github api call failed", "op", "GetIssue", "err", err)
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "github api call returned error", "op", "GetIssue", "status", resp.StatusCode, "message", apiErr.Message)
		return nil, apiErr
	}
	return decodeIssue(resp.Body)
}

// Ping verifies that token is accepted by GitHub, by calling a lightweight
// authenticated endpoint that requires no special scopes. Used by the
// Configuration API's integration health check (docs/project-plan.md M10) to
// report "connected" vs. "error" for a configured GitHub integration.
func (c *Client) Ping(ctx context.Context) error {
	slog.DebugContext(ctx, "github api call", "op", "Ping")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/rate_limit", nil)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "github api call failed", "op", "Ping", "err", err)
		return fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "github api call returned error", "op", "Ping", "status", resp.StatusCode, "message", apiErr.Message)
		return apiErr
	}
	return nil
}

// CreateIssue creates a new GitHub Issue in the given repository and returns
// the resulting issue. Labels (if any) are set atomically as part of the
// same creation request.
func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	slog.DebugContext(ctx, "github api call", "op", "CreateIssue", "owner", req.Owner, "repo", req.Repo)
	payload := map[string]any{
		"title": req.Title,
		"body":  req.Body,
	}
	if len(req.Labels) > 0 {
		payload["labels"] = req.Labels
	}
	if len(req.Assignees) > 0 {
		payload["assignees"] = req.Assignees
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("github: marshal request: %w", err)
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues", c.baseURL, url.PathEscape(req.Owner), url.PathEscape(req.Repo))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		slog.WarnContext(ctx, "github api call failed", "op", "CreateIssue", "err", err)
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		apiErr := newAPIError(resp)
		slog.WarnContext(ctx, "github api call returned error", "op", "CreateIssue", "status", resp.StatusCode, "message", apiErr.Message)
		return nil, apiErr
	}
	issue, err := decodeIssue(resp.Body)
	if err == nil {
		slog.InfoContext(ctx, "github issue created", "owner", req.Owner, "repo", req.Repo, "number", issue.Number, "url", issue.HTMLURL)
	}
	return issue, err
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
}

type issueDTO struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

func decodeIssue(r io.Reader) (*Issue, error) {
	var dto issueDTO
	if err := json.NewDecoder(r).Decode(&dto); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}
	return &Issue{
		Number:  dto.Number,
		Title:   dto.Title,
		Body:    dto.Body,
		State:   dto.State,
		HTMLURL: dto.HTMLURL,
	}, nil
}

// newAPIError builds an *APIError from a non-success response, attempting to
// extract GitHub's own error message so callers see the actual failure reason
// (bad scope, rate limit, validation error) rather than a bare status code.
func newAPIError(resp *http.Response) *APIError {
	limited := io.LimitReader(resp.Body, maxErrorBodyLen)
	raw, _ := io.ReadAll(limited)

	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &body)

	return &APIError{StatusCode: resp.StatusCode, Message: body.Message}
}
