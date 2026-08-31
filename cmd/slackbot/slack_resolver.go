package main

import (
	"context"
	"sync"

	sevitoutslack "github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

// slackClientResolver holds the live REST client behind bot.slack
// (CreateChannel/InviteUsers/PostMessage/FetchHistory/LookupUserIDByEmail),
// swappable in place whenever a fresher bot_token becomes available — either
// resolved at startup (preferring a datastore-configured credential over the
// static SLACK_BOT_TOKEN env var, mirroring PagerDuty/GitHub/Jira's
// preference in cmd/server's *Resolver types) or picked up later by
// runSettingsRefresher's periodic poll of ConfigService.GetSlackBotCredential.
//
// This mirrors cmd/server's *Resolver.apply pattern (see e.g.
// pagerdutyResolver), but lives here rather than there: unlike PagerDuty/
// GitHub/Jira, whose live clients run in-process inside cmd/server, the
// Slack REST client this resolver swaps runs inside cmd/slackbot — a
// separate binary with no direct datastore/encryption-key access (see
// docs/roadmap.md Phase 8's "why this isn't the same fix" section).
//
// Deliberately scoped to just this one REST client, not the Socket Mode
// connection (smClient in main.go) — reconnecting Socket Mode on a token
// change needs its own retry/backoff design and is an explicit follow-up,
// not built here. Safe for concurrent use.
type slackClientResolver struct {
	mu         sync.RWMutex
	current    slackClient
	appliedTok string // the bot token current was last built from, "" for the initial client
}

// newSlackClientResolver returns a resolver whose initial client is
// initial, built from initialToken (typically whatever token — datastore or
// static SLACK_BOT_TOKEN — was resolved at startup). Recording initialToken
// up front means the first periodic apply() call is a genuine no-op when
// nothing has actually changed, instead of an unconditional one-time rebuild.
func newSlackClientResolver(initial slackClient, initialToken string) *slackClientResolver {
	return &slackClientResolver{current: initial, appliedTok: initialToken}
}

// apply swaps in a fresh client built from botToken, unless botToken is the
// same one current was already built from — runSettingsRefresher calls this
// every tick regardless of whether anything actually changed, and rebuilding
// (and reconnecting) an identical client on every poll would be pure churn.
// A no-op call (e.g. a refresh that found nothing datastore-configured)
// simply isn't made at all — callers only call apply with a botToken
// they've already confirmed is non-empty and worth switching to.
func (r *slackClientResolver) apply(botToken string) (changed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if botToken == r.appliedTok {
		return false
	}
	r.current = newSlackAPIClient(botToken)
	r.appliedTok = botToken
	return true
}

// newSlackAPIClient builds the live client for a resolved bot token. A
// package-level var (rather than calling sevitoutslack.NewClient directly)
// so tests can substitute a fake and assert a swap actually happened without
// making a real Slack API call.
var newSlackAPIClient = func(botToken string) slackClient {
	return sevitoutslack.NewClient(botToken)
}

func (r *slackClientResolver) client() slackClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func (r *slackClientResolver) CreateChannel(ctx context.Context, name string) (string, error) {
	return r.client().CreateChannel(ctx, name)
}

func (r *slackClientResolver) InviteUsers(ctx context.Context, channelID string, userIDs []string) error {
	return r.client().InviteUsers(ctx, channelID, userIDs)
}

func (r *slackClientResolver) PostMessage(ctx context.Context, channelID, text string) error {
	return r.client().PostMessage(ctx, channelID, text)
}

func (r *slackClientResolver) FetchHistory(ctx context.Context, channelID string, limit int) ([]sevitoutslack.Message, error) {
	return r.client().FetchHistory(ctx, channelID, limit)
}

func (r *slackClientResolver) LookupUserIDByEmail(ctx context.Context, email string) (string, error) {
	return r.client().LookupUserIDByEmail(ctx, email)
}

var _ slackClient = (*slackClientResolver)(nil)
