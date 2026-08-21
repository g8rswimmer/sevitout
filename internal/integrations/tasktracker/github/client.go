package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL   = "https://api.github.com"
	requestTimeout   = 10 * time.Second
	githubAPIVersion = "2022-11-28"
)

// Issue is a GitHub Issue as returned by the client.
type Issue struct {
	Number  int
	Title   string
	Body    string
	State   string // "open" or "closed"
	HTMLURL string
}

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
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.baseURL, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("github: issue %s/%s#%d not found", owner, repo, number)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}
	return &Issue{
		Number:  body.Number,
		Title:   body.Title,
		Body:    body.Body,
		State:   body.State,
		HTMLURL: body.HTMLURL,
	}, nil
}

// CreateIssue creates a new GitHub Issue in the given repository and returns
// the resulting issue.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string) (*Issue, error) {
	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
	})
	if err != nil {
		return nil, fmt.Errorf("github: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues", c.baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("github: unexpected status %d", resp.StatusCode)
	}

	var created struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}
	return &Issue{
		Number:  created.Number,
		Title:   created.Title,
		Body:    created.Body,
		State:   created.State,
		HTMLURL: created.HTMLURL,
	}, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
}
