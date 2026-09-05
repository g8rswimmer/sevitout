package grpc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SlackSender is the narrow Slack capability the notification dispatcher
// needs — post a plain-text message to a channel. Declared here (the
// consumer) per this repo's interface-ownership convention. Distinct from
// role.go's SlackInviteClient (invite/lookup, not posting) — a notify-only
// caller shouldn't have to satisfy methods it never calls.
// internal/integrations/slack.Client satisfies this implicitly.
type SlackSender interface {
	PostMessage(ctx context.Context, channelID, text string) error
}

// EmailSender is the narrow email capability the notification dispatcher
// needs. internal/integrations/email.Client satisfies this implicitly.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SlackSenderFactory builds a SlackSender from a decrypted Slack bot token.
// Injected (rather than this package importing internal/integrations/slack
// directly) so tests can substitute a fake — mirrors role.go's
// SlackClientFactory.
type SlackSenderFactory func(botToken string) SlackSender

// EmailSenderFactory builds an EmailSender from the "email" integration's
// merged credentials+settings (smtp_username/smtp_password from credentials,
// smtp_host/smtp_port/from_address from settings — see
// internal/integrations/catalog's "email" entry).
type EmailSenderFactory func(config map[string]string) EmailSender

// NotifyEvent is one lifecycle event a NotificationConfig rule can match
// against (docs/roadmap.md Phase 15). SEV is nil for an event type with no
// severity to filter rows by (none exist today, but keeps the contract
// honest). Message, when set, overrides the default rendered text.
type NotifyEvent struct {
	Type    string
	SEV     *store.SEV
	Message string
}

// Notifier dispatches a NotifyEvent to every NotificationConfig rule that
// matches its event type and severity filter (docs/requirements.md §16,
// docs/roadmap.md Phase 15). Notify is best-effort — the same contract as
// auditAppendBestEffort: a delivery failure is logged, never returned to the
// caller, and must never block or fail the mutation the event is attached
// to. A nil *Notifier is a safe no-op, matching Publisher's
// nil-is-a-no-op convention (e.g. in tests that don't wire one up).
type Notifier struct {
	configs      store.NotificationConfigStore
	integrations store.IntegrationConfigStore
	crypto       Encryptor
	slackFactory SlackSenderFactory
	emailFactory EmailSenderFactory
}

// NotifierParams groups NewNotifier's dependencies. SlackFactory/EmailFactory
// may be nil, in which case rules routed to that channel type are silently
// skipped (logged, not treated as an error) — the same "optional at deploy
// time" posture every other integration in this codebase uses.
type NotifierParams struct {
	Configs      store.NotificationConfigStore
	Integrations store.IntegrationConfigStore
	Crypto       Encryptor
	SlackFactory SlackSenderFactory
	EmailFactory EmailSenderFactory
}

// NewNotifier returns a Notifier backed by p.
func NewNotifier(p NotifierParams) *Notifier {
	return &Notifier{
		configs:      p.Configs,
		integrations: p.Integrations,
		crypto:       p.Crypto,
		slackFactory: p.SlackFactory,
		emailFactory: p.EmailFactory,
	}
}

// Notify looks up every NotificationConfig rule matching ev.Type (filtered
// by ev.SEV's severity level, when set — a rule with a MaxSeverityLevel
// stricter than ev.SEV's own severity is skipped) and delivers to each
// matching rule's channel. Safe to call on a nil *Notifier or with a nil
// configs store (both no-op).
func (n *Notifier) Notify(ctx context.Context, ev NotifyEvent) {
	if n == nil || n.configs == nil {
		return
	}
	var severity *int16
	if ev.SEV != nil {
		lvl := ev.SEV.SeverityLevel
		severity = &lvl
	}
	rows, err := n.configs.ListForEvent(ctx, ev.Type, severity)
	if err != nil {
		slog.ErrorContext(ctx, "notify: list notification config failed", "event", ev.Type, "err", err)
		return
	}
	for _, row := range rows {
		n.deliver(ctx, row, ev)
	}
}

func (n *Notifier) deliver(ctx context.Context, row *store.NotificationConfig, ev NotifyEvent) {
	switch row.ChannelType {
	case store.NotificationChannelSlack:
		n.deliverSlack(ctx, row, ev)
	case store.NotificationChannelEmail:
		n.deliverEmail(ctx, row, ev)
	default:
		slog.ErrorContext(ctx, "notify: unknown channel type", "channel_type", row.ChannelType, "event", row.Event)
	}
}

func (n *Notifier) deliverSlack(ctx context.Context, row *store.NotificationConfig, ev NotifyEvent) {
	if n.slackFactory == nil || n.integrations == nil {
		return
	}
	creds, err := n.decryptedIntegration(ctx, "slack")
	if err != nil {
		slog.ErrorContext(ctx, "notify: slack integration unavailable", "err", err)
		return
	}
	botToken := creds["bot_token"]
	if botToken == "" {
		return
	}
	text := notifyText(ev)
	// <#CHANNELID> is Slack's own channel-mention syntax — it renders as a
	// clickable "#channel-name" link regardless of the posting channel, no
	// separate name lookup needed. Only ever populated once cmd/slackbot has
	// created the incident channel for this SEV (§13.1) — a race against the
	// very first "sev.created" notification, which fires from the same event
	// that triggers that channel's creation in a separate process, so it's
	// normal for that one delivery to go out without this line and a later
	// event (sev.updated/status_changed) to include it once the channel
	// exists.
	if ev.SEV != nil && ev.SEV.SlackChannelID != nil && *ev.SEV.SlackChannelID != "" {
		text = fmt.Sprintf("%s\nIncident channel: <#%s>", text, *ev.SEV.SlackChannelID)
	}
	if err := n.slackFactory(botToken).PostMessage(ctx, row.ChannelTarget, text); err != nil {
		slog.ErrorContext(ctx, "notify: slack delivery failed", "channel", row.ChannelTarget, "event", row.Event, "err", err)
	}
}

func (n *Notifier) deliverEmail(ctx context.Context, row *store.NotificationConfig, ev NotifyEvent) {
	if n.emailFactory == nil || n.integrations == nil {
		return
	}
	cfg, err := n.integrations.Get(ctx, "email")
	if err != nil {
		slog.ErrorContext(ctx, "notify: email integration unavailable", "err", err)
		return
	}
	creds, err := DecryptIntegrationCredentials(n.crypto, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "notify: failed to decrypt email credentials", "err", err)
		return
	}
	merged := make(map[string]string, len(creds)+len(cfg.Settings))
	for k, v := range creds {
		merged[k] = v
	}
	for k, v := range cfg.Settings {
		if sv, ok := v.(string); ok {
			merged[k] = sv
		}
	}
	if merged["smtp_host"] == "" || merged["from_address"] == "" {
		return
	}
	body := notifyText(ev)
	if ev.SEV != nil && ev.SEV.SlackChannelID != nil && *ev.SEV.SlackChannelID != "" {
		// Slack's universal deep link — works from any workspace context,
		// unlike a workspace-specific https://<team>.slack.com/... URL, which
		// this codebase has no clean way to construct (see catalog's "slack"
		// entry: no team/workspace domain is stored, only the bot token).
		body = fmt.Sprintf("%s\nSlack channel: https://slack.com/app_redirect?channel=%s", body, *ev.SEV.SlackChannelID)
	}
	if err := n.emailFactory(merged).Send(ctx, row.ChannelTarget, notifySubject(ev), body); err != nil {
		slog.ErrorContext(ctx, "notify: email delivery failed", "to", row.ChannelTarget, "event", row.Event, "err", err)
	}
}

func (n *Notifier) decryptedIntegration(ctx context.Context, integrationType string) (map[string]string, error) {
	cfg, err := n.integrations.Get(ctx, integrationType)
	if err != nil {
		return nil, err
	}
	return DecryptIntegrationCredentials(n.crypto, cfg)
}

// notifySubject renders an email subject line for ev. ev.SEV.ID is the
// human-readable case number (e.g. "SEV-2026-0042", docs/requirements.md
// §4.1) — not to be confused with ev.SEV.SeverityLevel (an int16, 1-4),
// which gets its own "severity N" label instead of overloading the
// "SEV-" prefix, a confusion an earlier version of this function had.
func notifySubject(ev NotifyEvent) string {
	if ev.SEV != nil {
		return fmt.Sprintf("[Sevitout] %s (%s, severity %d): %s", ev.Type, ev.SEV.ID, ev.SEV.SeverityLevel, ev.SEV.Title)
	}
	return fmt.Sprintf("[Sevitout] %s", ev.Type)
}

// notifyText renders a short human-readable line for ev, reused across both
// Slack and email delivery — ev.Message overrides this default when set.
func notifyText(ev NotifyEvent) string {
	if ev.Message != "" {
		return ev.Message
	}
	if ev.SEV != nil {
		return fmt.Sprintf("%s: %s — %q (severity %d, %s)", ev.Type, ev.SEV.ID, ev.SEV.Title, ev.SEV.SeverityLevel, ev.SEV.Status)
	}
	return ev.Type
}
