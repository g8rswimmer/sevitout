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

// Ping verifies that apiKey is accepted by PagerDuty, by calling a
// lightweight authenticated endpoint that requires no special scopes. Used
// by the Configuration API's integration health check (docs/project-plan.md
// M10) to report "connected" vs. "error" for a configured PagerDuty integration.
func (c *Client) Ping(ctx context.Context) error {
	slog.DebugContext(ctx, "pagerduty api call", "op", "Ping")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/abilities", nil)
	if err != nil {
		return fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Authorization", "Token token="+c.apiKey)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "pagerduty api call failed", "op", "Ping", "err", err)
		return fmt.Errorf("pagerduty: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.WarnContext(ctx, "pagerduty api call returned non-200", "op", "Ping", "status", resp.StatusCode)
		return fmt.Errorf("pagerduty: unexpected status %d", resp.StatusCode)
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Authorization", "Token token="+c.apiKey)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")

	resp, err := c.http.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "pagerduty api call failed", "op", "OnCallLookup", "service_id", serviceID, "err", err)
		return "", fmt.Errorf("pagerduty: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.WarnContext(ctx, "pagerduty api call returned non-200", "op", "OnCallLookup", "service_id", serviceID, "status", resp.StatusCode)
		return "", fmt.Errorf("pagerduty: unexpected status %d", resp.StatusCode)
	}

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
