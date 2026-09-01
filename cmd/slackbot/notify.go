package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// sevPayload is the subset of a protojson-marshaled pb.SEVResponse (see
// internal/api/grpc's publishProto) this bot reads out of sev.created and
// sev.status_changed WebSocket events.
type sevPayload struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	SeverityLevel int32  `json:"severity_level"`
	// CreatedBy is the SEV's creator (a Sevitout user ID), read by
	// handleSEVCreated and passed to createIncidentChannel so the creator
	// is invited into the new incident channel (docs/roadmap.md Phase 11d)
	// — see resolveCreatorSlackUserID's doc comment for the one case
	// (`/sev open`) where this doesn't resolve to the actual human opener.
	CreatedBy string `json:"created_by"`
}

// announcementPayload is the subset of a protojson-marshaled
// pb.AnnouncementResponse this bot reads out of announcement.created events.
type announcementPayload struct {
	SevID    string `json:"sev_id"`
	Message  string `json:"message"`
	Audience string `json:"audience"`
}

// shouldPushAnnouncement reports whether an announcement with the given
// audience should be pushed to Slack. Per docs/requirements.md §6 and §13.1,
// only announcements meant for people outside the incident response team —
// external or status-page — are pushed; internal-only updates stay in
// Sevitout.
func shouldPushAnnouncement(audience string) bool {
	return audience == string(store.AudienceExternal) || audience == string(store.AudienceStatusPage)
}

// postToNotifyChannel posts text to sevID's notification channel (its own
// incident channel if one exists, else the configured default — see
// notifyChannel), logging and returning early rather than erroring when
// there's nowhere to send it or the post itself fails. eventName identifies
// the WebSocket event this post is for, purely for the log lines.
func (b *bot) postToNotifyChannel(ctx context.Context, sevID, eventName, text string) {
	channel := b.notifyChannel(sevID)
	if channel == "" {
		b.log.WarnContext(ctx, eventName+" notification dropped: no incident channel and no default_channel configured", "sev_id", sevID)
		return
	}
	if err := b.slack.PostMessage(ctx, channel, text); err != nil {
		b.log.ErrorContext(ctx, "post "+eventName+" notification failed", "sev_id", sevID, "err", err)
	}
}

// handleEvent dispatches one WebSocket event to the appropriate notifier.
// Unrecognized event types (anything this bot doesn't act on, e.g.
// task.updated) are silently ignored — this bot only reacts to a subset of
// the full event catalog (docs/architecture.md §3.2).
func (b *bot) handleEvent(ctx context.Context, evt ws.Event) {
	switch evt.Type {
	case "sev.created":
		b.handleSEVCreated(ctx, evt.Payload)
	case "sev.status_changed":
		b.handleSEVStatusChanged(ctx, evt.Payload)
	case "announcement.created":
		b.handleAnnouncementCreated(ctx, evt.Payload)
	}
}

// handleSEVCreated posts a bot notification for every newly opened SEV
// (docs/requirements.md §13.1) and auto-creates a dedicated incident channel
// for it, regardless of severity — every SEV gets its own channel so
// unrelated incidents' discussions never share one, keeping whatever
// channel `/sev open` was run in free to act as pure intake. (Sensitive
// SEVs never reach here at all: CreateSEV skips publishing sev.created for
// them, so they keep today's behavior of no auto-created channel.)
func (b *bot) handleSEVCreated(ctx context.Context, payload json.RawMessage) {
	var sev sevPayload
	if err := json.Unmarshal(payload, &sev); err != nil {
		b.log.ErrorContext(ctx, "decode sev.created payload failed", "err", err)
		return
	}

	if b.channelFor(sev.ID) == "" {
		b.createIncidentChannel(ctx, sev.ID, sev.Title, sev.SeverityLevel, sev.CreatedBy)
	}

	text := fmt.Sprintf(":rotating_light: SEV-%d opened: *%s* (%s)", sev.SeverityLevel, sev.Title, sev.ID)
	b.postToNotifyChannel(ctx, sev.ID, "sev.created", text)
}

// handleSEVStatusChanged posts a bot notification for every status
// transition, including resolution (docs/requirements.md §13.1).
func (b *bot) handleSEVStatusChanged(ctx context.Context, payload json.RawMessage) {
	var sev sevPayload
	if err := json.Unmarshal(payload, &sev); err != nil {
		b.log.ErrorContext(ctx, "decode sev.status_changed payload failed", "err", err)
		return
	}

	text := fmt.Sprintf("%s *%s* is now *%s*", statusEmoji(sev.Status), sev.Title, sev.Status)
	b.postToNotifyChannel(ctx, sev.ID, "sev.status_changed", text)
}

// handleAnnouncementCreated pushes external/status-page announcements to
// Slack (docs/requirements.md §6). Internal-only announcements are dropped
// by shouldPushAnnouncement.
func (b *bot) handleAnnouncementCreated(ctx context.Context, payload json.RawMessage) {
	var a announcementPayload
	if err := json.Unmarshal(payload, &a); err != nil {
		b.log.ErrorContext(ctx, "decode announcement.created payload failed", "err", err)
		return
	}
	if !shouldPushAnnouncement(a.Audience) {
		return
	}

	text := fmt.Sprintf(":mega: *%s* update: %s", a.SevID, a.Message)
	b.postToNotifyChannel(ctx, a.SevID, "announcement.created", text)
}

// statusEmoji returns a short visual marker for a SEV status, purely to make
// notification messages easier to scan in a busy channel.
func statusEmoji(status string) string {
	switch store.SEVStatus(status) {
	case store.SEVStatusResolved:
		return ":white_check_mark:"
	case store.SEVStatusMitigated:
		return ":large_yellow_circle:"
	case store.SEVStatusPostmortemComplete:
		return ":closed_book:"
	default:
		return ":large_orange_diamond:"
	}
}
