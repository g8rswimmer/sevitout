// Command slackbot is Sevitout's Slack integration (docs/project-plan.md
// M11): slash commands, auto-created incident channels, bot notifications,
// announcement push, and chat capture. It holds no state of its own beyond
// an in-memory SEV→incident-channel map — everything else is read from (or
// written to) the API server over gRPC.
package main

import (
	"context"
	"log/slog"
	"sync"

	sevitoutslack "github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

// defaultChannelNamingConvention matches docs/requirements.md §13.1's
// example ("#inc-2026-0042-database-outage") and is used whenever the
// "slack" integration config has no channel_naming_convention setting.
// {level}, {id}, and {title} are substituted with the SEV's severity level,
// ID, and (slugified) title.
const defaultChannelNamingConvention = "inc-{id}-{title}"

// defaultCaptureLimit is how many recent channel messages `/sev capture`
// pulls in when the caller doesn't specify a count.
const defaultCaptureLimit = 20

// maxCaptureLimit bounds how many messages `/sev capture` will ever pull in
// one invocation. Each captured message costs one blocking AddChatEntry gRPC
// call in the same command goroutine (see handleCapture) with no batching or
// concurrency, so an unbounded caller-supplied limit would let one Slack
// command trigger an arbitrarily large, slow sequence of calls.
const maxCaptureLimit = 200

// slackClient is the subset of internal/integrations/slack.Client this bot
// calls, declared here (the consumer) so unit tests can substitute a fake
// instead of hitting Slack's real API.
type slackClient interface {
	CreateChannel(ctx context.Context, name string) (string, error)
	InviteUsers(ctx context.Context, channelID string, userIDs []string) error
	PostMessage(ctx context.Context, channelID, text string) error
	FetchHistory(ctx context.Context, channelID string, limit int) ([]sevitoutslack.Message, error)
	LookupUserIDByEmail(ctx context.Context, email string) (string, error)
}

// bot wires together the Slack client, the API server clients, and the
// bot's configuration. The zero value is not usable; construct via newBot.
type bot struct {
	slack slackClient
	api   apiClients
	log   *slog.Logger

	mu sync.Mutex
	// defaultChannel is where SEV lifecycle notifications go when a SEV has
	// no auto-created incident channel of its own (any SEV-3/4, or a
	// SEV-1/2 opened before the channel finished being created).
	//
	// defaultChannel and channelNamingConvention are set at startup
	// (loadSlackSettings) and kept current afterward by
	// runSettingsRefresher, which polls ConfigService in the background —
	// both are guarded by mu since that goroutine writes them concurrently
	// with request-handling goroutines reading them.
	defaultChannel string
	// channelNamingConvention renders an incident channel's name; see
	// incidentChannelName.
	channelNamingConvention string
	// channels maps a SEV ID to the Slack channel ID auto-created for it.
	// In-memory only: a bot restart forgets the mapping, and future
	// notifications for that SEV fall back to defaultChannel. Acceptable
	// for v1 — see demo/M11-slack-bot.md's Known limitations.
	channels map[string]string
	// pendingOpeners maps a SEV ID to the Slack user ID of whoever opened it
	// via `/sev open`, for a SEV whose incident channel doesn't exist yet at
	// the time handleOpen returns (the common case: the API server publishes
	// sev.created — which drives channel creation, see createIncidentChannel
	// — asynchronously over the WebSocket, so handleOpen usually resumes
	// before that channel exists). channelOrRegisterOpener and
	// takePendingOpener are the only accessors, both locked under mu so the
	// two goroutines racing to create/consume this entry (handleOpen's and
	// the WS event-listener's) linearize cleanly instead of one silently
	// dropping the invite.
	pendingOpeners map[string]string
}

// botParams groups newBot's dependencies. DefaultChannel and
// ChannelNamingConvention come from the "slack" integration config (see
// loadSlackSettings); both may be empty, in which case sensible defaults
// apply.
type botParams struct {
	Slack                   slackClient
	API                     apiClients
	Log                     *slog.Logger // nil defaults to slog.Default()
	DefaultChannel          string
	ChannelNamingConvention string
}

// newBot constructs a bot.
func newBot(p botParams) *bot {
	log := p.Log
	if log == nil {
		log = slog.Default()
	}
	return &bot{
		slack:                   p.Slack,
		api:                     p.API,
		log:                     log,
		defaultChannel:          p.DefaultChannel,
		channelNamingConvention: p.ChannelNamingConvention,
		channels:                make(map[string]string),
		pendingOpeners:          make(map[string]string),
	}
}

// channelFor returns the incident channel mapped to sevID, or "" if none has
// been created (yet, or ever — e.g. a SEV-3/4).
func (b *bot) channelFor(sevID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.channels[sevID]
}

// setChannelFor records the incident channel auto-created for sevID.
func (b *bot) setChannelFor(sevID, channelID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.channels[sevID] = channelID
}

// channelOrRegisterOpener returns sevID's incident channel if one has
// already been created; otherwise it records openerID as the person to
// invite once that channel exists (see takePendingOpener) and returns "".
// Called by handleOpen right after CreateSEV succeeds.
func (b *bot) channelOrRegisterOpener(sevID, openerID string) (channelID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch := b.channels[sevID]; ch != "" {
		return ch
	}
	b.pendingOpeners[sevID] = openerID
	return ""
}

// takePendingOpener pops and returns the Slack user ID registered by
// channelOrRegisterOpener for sevID, or "" if none is pending (the SEV
// wasn't opened via Slack, or its opener was already invited directly).
// Called by createIncidentChannel right after a new channel is recorded.
func (b *bot) takePendingOpener(sevID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	opener := b.pendingOpeners[sevID]
	delete(b.pendingOpeners, sevID)
	return opener
}

// notifyChannel picks the channel a lifecycle notification for sevID should
// go to: sevID's own incident channel if one exists, else the configured
// default channel. Returns "" (meaning "nowhere to send it") when neither is
// set, which callers must treat as a no-op rather than an error — a bot
// with no default channel configured is a valid (if quiet) configuration.
func (b *bot) notifyChannel(sevID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch := b.channels[sevID]; ch != "" {
		return ch
	}
	return b.defaultChannel
}

// namingConvention returns the current channel-naming-convention setting,
// for incidentChannelName. See the mu comment above for why this is locked.
func (b *bot) namingConvention() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.channelNamingConvention
}

// setSlackSettings updates the bot's default-channel and
// channel-naming-convention settings, e.g. after runSettingsRefresher polls
// a change out of ConfigService.
func (b *bot) setSlackSettings(defaultChannel, namingConvention string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.defaultChannel = defaultChannel
	b.channelNamingConvention = namingConvention
}
