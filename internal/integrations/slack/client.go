// Package slack wraps the subset of the Slack Web API that Sevitout's Slack
// bot needs: creating incident channels, inviting participants, posting
// notifications, and pulling channel history for chat-log capture (see
// docs/architecture.md §7, docs/requirements.md §13.1).
package slack

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

// errUserNotFound is the Slack API's error string when GetUserByEmail finds
// no matching account. Not every Sevitout user has a Slack account, so this
// is treated as a normal "no match" result rather than an error.
const errUserNotFound = "users_not_found"

// Message is one entry from a channel's history, used for chat-log capture.
type Message struct {
	UserID    string
	Text      string
	Timestamp string // Slack's "ts": seconds.microseconds since epoch, as a string
}

// Client wraps the Slack Web API using a bot token (xoxb-...).
type Client struct {
	api *slack.Client
}

// NewClient returns a Client authenticating with botToken.
func NewClient(botToken string) *Client {
	return &Client{api: slack.New(botToken)}
}

// NewClientWithBaseURL returns a Client pointed at a custom API base URL,
// intended for use in tests with httptest.Server (mirrors
// internal/integrations/pagerduty and .../github).
func NewClientWithBaseURL(botToken, baseURL string) *Client {
	return &Client{api: slack.New(botToken, slack.OptionAPIURL(baseURL))}
}

// CreateChannel creates a new public channel named name and returns its
// Slack channel ID.
func (c *Client) CreateChannel(ctx context.Context, name string) (string, error) {
	ch, err := c.api.CreateConversationContext(ctx, slack.CreateConversationParams{ChannelName: name})
	if err != nil {
		return "", fmt.Errorf("slack: create channel %q: %w", name, err)
	}
	return ch.ID, nil
}

// InviteUsers invites userIDs (Slack user IDs, not emails) to channelID. A
// no-op if userIDs is empty, so callers don't need to special-case "nobody
// to invite" (e.g. no on-call configured yet).
func (c *Client) InviteUsers(ctx context.Context, channelID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	if _, err := c.api.InviteUsersToConversationContext(ctx, channelID, userIDs...); err != nil {
		return fmt.Errorf("slack: invite users to %s: %w", channelID, err)
	}
	return nil
}

// PostMessage posts a plain-text message to channelID.
func (c *Client) PostMessage(ctx context.Context, channelID, text string) error {
	if _, _, err := c.api.PostMessageContext(ctx, channelID, slack.MsgOptionText(text, false)); err != nil {
		return fmt.Errorf("slack: post message to %s: %w", channelID, err)
	}
	return nil
}

// FetchHistory returns the most recent limit messages in channelID, in
// chronological (oldest-first) order — Slack's API itself returns
// newest-first, which callers appending to a SEV's chat log would otherwise
// have to reverse themselves.
func (c *Client) FetchHistory(ctx context.Context, channelID string, limit int) ([]Message, error) {
	resp, err := c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("slack: fetch history for %s: %w", channelID, err)
	}
	msgs := make([]Message, len(resp.Messages))
	for i, m := range resp.Messages {
		msgs[len(resp.Messages)-1-i] = Message{UserID: m.User, Text: m.Text, Timestamp: m.Timestamp}
	}
	return msgs, nil
}

// LookupUserIDByEmail resolves email to a Slack user ID, for inviting a
// Sevitout user (identified by email) into an incident channel. Returns
// ("", nil) rather than an error when no Slack account matches, since that's
// an expected case, not a failure.
func (c *Client) LookupUserIDByEmail(ctx context.Context, email string) (string, error) {
	u, err := c.api.GetUserByEmailContext(ctx, email)
	if err != nil {
		if err.Error() == errUserNotFound {
			return "", nil
		}
		return "", fmt.Errorf("slack: lookup user by email %s: %w", email, err)
	}
	return u.ID, nil
}

// Ping verifies the bot token is accepted by Slack. Used by the Configuration
// API's integration health check (docs/project-plan.md M10).
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.AuthTestContext(ctx); err != nil {
		return fmt.Errorf("slack: auth test: %w", err)
	}
	return nil
}
