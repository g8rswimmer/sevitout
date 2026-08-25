package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const onCallLookupTimeout = 10 * time.Second

const defaultBaseURL = "https://api.pagerduty.com"

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
		args := append([]any{"op", op}, logAttrs...)
		slog.WarnContext(ctx, "pagerduty api call returned non-200", append(args, "status", resp.StatusCode)...)
		return nil, fmt.Errorf("pagerduty: unexpected status %d", resp.StatusCode)
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
