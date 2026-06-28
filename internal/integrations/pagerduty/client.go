package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

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
		http:    &http.Client{},
	}
}

// NewClientWithBaseURL returns a Client that uses a custom base URL, intended
// for use in tests with httptest.Server.
func NewClientWithBaseURL(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

// OnCallLookup returns the display name of the current primary on-call user for
// the given PagerDuty service ID. Returns ("", nil) when no one is currently
// on-call for that service.
func (c *Client) OnCallLookup(ctx context.Context, serviceID string) (string, error) {
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
		return "", fmt.Errorf("pagerduty: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
	u2 := body.OnCalls[0].User
	if u2.Name == "" {
		return "", nil
	}
	if u2.Email != "" {
		return fmt.Sprintf("%s <%s>", u2.Name, u2.Email), nil
	}
	return u2.Name, nil
}
