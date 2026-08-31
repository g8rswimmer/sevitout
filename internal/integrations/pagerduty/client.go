package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const onCallLookupTimeout = 10 * time.Second

const defaultBaseURL = "https://api.pagerduty.com"

// maxErrorBodyLen bounds how much of an error response body newAPIError
// reads, matching github.APIError/jira.APIError's own limit — plenty for a
// JSON error object, small enough that a misbehaving server can't make this
// read unbounded amounts of memory.
const maxErrorBodyLen = 4096

// APIError is returned by any Client method that gets back a non-200
// response from PagerDuty's REST API v2. Its Error() message includes
// whatever message PagerDuty's own error body carried — mirroring
// github.APIError/jira.APIError's friendlier equivalents. Previously this
// package discarded the response body entirely on error and reported only
// the bare HTTP status code, which the admin integrations health check
// (docs/roadmap.md Phase 9) then surfaced to an admin as an unhelpful
// "unexpected status 401" with no indication of what PagerDuty itself said
// was actually wrong.
type APIError struct {
	StatusCode int
	// Message is PagerDuty's own error.message field, when the response
	// body parses as PagerDuty's standard {"error":{"message":...}} shape;
	// empty otherwise (a non-JSON body, or one that doesn't match that shape).
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("pagerduty: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("pagerduty: unexpected status %d", e.StatusCode)
}

// HTTPStatus returns the response status code, letting callers branch on it
// without depending on this package's concrete APIError type — mirrors
// github.APIError.HTTPStatus/jira.APIError.HTTPStatus.
func (e *APIError) HTTPStatus() int { return e.StatusCode }

// newAPIError reads (and closes, via the caller's defer) resp.Body, tolerating
// any shape that isn't valid JSON or doesn't match PagerDuty's error
// envelope — Message just stays empty in that case, same forgiving behavior
// as github/jira's equivalent, so a body newAPIError doesn't recognize never
// panics or returns a decode error of its own.
func newAPIError(resp *http.Response) *APIError {
	limited := io.LimitReader(resp.Body, maxErrorBodyLen)
	raw, _ := io.ReadAll(limited)

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)

	return &APIError{StatusCode: resp.StatusCode, Message: body.Error.Message}
}

// Client calls the PagerDuty REST API v2.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewClient returns a Client that authenticates with apiKey against the
// PagerDuty production API.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: onCallLookupTimeout},
	}
}

// NewClientWithBaseURL returns a Client that uses a custom base URL, intended
// for use in tests with httptest.Server.
func NewClientWithBaseURL(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: onCallLookupTimeout},
	}
}

// newRequest builds a GET request against c.baseURL+path with PagerDuty's
// standard auth/accept headers set, shared by every method below so the
// header-setting logic exists in exactly one place.
func (c *Client) newRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Authorization", "Token token="+c.apiKey)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	return req, nil
}

// do sends req and validates the response status, logging and returning an
// error for either a transport failure or a non-200 response — the same
// "do → check status → log on failure" sequence every method below needs.
// logAttrs are extra slog key-value pairs specific to the call (e.g.
// OnCallLookup's service_id), appended after "op" and before the final
// err/status pair so the field order matches what each method logged before
// this was factored out. The caller is responsible for closing resp.Body on
// a nil error.
func (c *Client) do(ctx context.Context, op string, req *http.Request, logAttrs ...any) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		args := append([]any{"op", op}, logAttrs...)
		slog.WarnContext(ctx, "pagerduty api call failed", append(args, "err", err)...)
		return nil, fmt.Errorf("pagerduty: request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		apiErr := newAPIError(resp)
		args := append([]any{"op", op}, logAttrs...)
		slog.WarnContext(ctx, "pagerduty api call returned non-200", append(args, "status", resp.StatusCode, "message", apiErr.Message)...)
		return nil, apiErr
	}
	return resp, nil
}

// Ping verifies that apiKey is accepted by PagerDuty, by calling a
// lightweight authenticated endpoint that requires no special scopes. Used
// by the Configuration API's integration health check (docs/project-plan.md
// M10) to report "connected" vs. "error" for a configured PagerDuty integration.
func (c *Client) Ping(ctx context.Context) error {
	slog.DebugContext(ctx, "pagerduty api call", "op", "Ping")
	req, err := c.newRequest(ctx, c.baseURL+"/abilities")
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, "Ping", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// OnCallLookup returns the display name of the current primary on-call user for
// the given PagerDuty service ID. Returns ("", nil) when no one is currently
// on-call for that service.
func (c *Client) OnCallLookup(ctx context.Context, serviceID string) (string, error) {
	slog.DebugContext(ctx, "pagerduty api call", "op", "OnCallLookup", "service_id", serviceID)
	u, _ := url.Parse(c.baseURL + "/oncalls")
	q := u.Query()
	q.Set("time_zone", "UTC")
	q.Add("include[]", "users")
	q.Add("service_ids[]", serviceID)
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, u.String())
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, "OnCallLookup", req, "service_id", serviceID)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var body struct {
		OnCalls []struct {
			EscalationLevel int `json:"escalation_level"`
			User            struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"user"`
		} `json:"oncalls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("pagerduty: decode response: %w", err)
	}
	if len(body.OnCalls) == 0 {
		return "", nil
	}
	// Find primary on-call by lowest escalation level; response order is not guaranteed.
	primary := body.OnCalls[0]
	for _, oc := range body.OnCalls[1:] {
		if oc.EscalationLevel < primary.EscalationLevel {
			primary = oc
		}
	}
	u2 := primary.User
	if u2.Name == "" {
		return "", nil
	}
	if u2.Email != "" {
		return fmt.Sprintf("%s <%s>", u2.Name, u2.Email), nil
	}
	return u2.Name, nil
}
