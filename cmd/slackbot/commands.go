package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// usageText is returned whenever a /sev invocation can't be parsed.
const usageText = "usage: `/sev open [severity 1-4] <title>` | `/sev update <sev-id> <message>` | `/sev transition <sev-id> <status>` | `/sev resolve <sev-id>` | `/sev capture <sev-id> [limit]`"

// validStatusNames lists every status the state machine
// (internal/sev.ValidateTransition) recognizes, purely so `/sev transition`
// can remind the caller what's valid on a rejected transition — the actual
// legality of a given from→to move is still enforced server-side, since it
// depends on the SEV's current status, which this bot doesn't track.
const validStatusNames = "open, investigating, mitigated, resolved, postmortem_in_progress, postmortem_complete"

// parseAction splits a command's free-text argument into its first
// whitespace-separated token (the action) and everything after it, for
// `/sev <action> ...` and `@sevbot <action> ...` alike.
func parseAction(text string) (action string, args []string, err error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil, errors.New(usageText)
	}
	return strings.ToLower(fields[0]), fields[1:], nil
}

// parseOpenArgs parses the arguments to `/sev open`: an optional leading
// severity level (1-4, defaulting to 3 — "Minor" — when omitted, matching
// the least-alarming default) followed by the required title.
func parseOpenArgs(args []string) (severity int32, title string, err error) {
	if len(args) == 0 {
		return 0, "", errors.New("usage: `/sev open [severity 1-4] <title>`")
	}
	severity, rest := int32(3), args
	if n, convErr := strconv.Atoi(args[0]); convErr == nil {
		if n < 1 || n > 4 {
			return 0, "", fmt.Errorf("severity must be between 1 and 4, got %d", n)
		}
		severity, rest = int32(n), args[1:]
	}
	if len(rest) == 0 {
		return 0, "", errors.New("usage: `/sev open [severity 1-4] <title>`")
	}
	return severity, strings.Join(rest, " "), nil
}

// parseIDAndText parses "<sev-id> <free text...>", used by `/sev update`.
func parseIDAndText(args []string, usage string) (id, text string, err error) {
	if len(args) < 2 {
		return "", "", errors.New(usage)
	}
	return args[0], strings.Join(args[1:], " "), nil
}

// parseTransitionArgs parses "<sev-id> <status>", used by `/sev transition`.
// status is lowercased so `/sev transition SEV-1 Resolved` works the same as
// `/sev transition SEV-1 resolved`; anything after the second token is
// ignored rather than rejected, matching parseID's tolerance.
func parseTransitionArgs(args []string) (id, toStatus string, err error) {
	const usage = "usage: `/sev transition <sev-id> <status>` (one of: " + validStatusNames + ")"
	if len(args) < 2 {
		return "", "", errors.New(usage)
	}
	return args[0], strings.ToLower(args[1]), nil
}

// parseID parses a single required "<sev-id>" argument, used by `/sev
// resolve`. Extra trailing tokens are ignored rather than rejected.
func parseID(args []string, usage string) (string, error) {
	if len(args) == 0 {
		return "", errors.New(usage)
	}
	return args[0], nil
}

// parseCaptureArgs parses "<sev-id> [limit]", used by `/sev capture`.
func parseCaptureArgs(args []string) (id string, limit int, err error) {
	const usage = "usage: `/sev capture <sev-id> [limit]`"
	if len(args) == 0 {
		return "", 0, errors.New(usage)
	}
	if len(args) == 1 {
		return args[0], defaultCaptureLimit, nil
	}
	n, convErr := strconv.Atoi(args[1])
	if convErr != nil || n <= 0 {
		return "", 0, fmt.Errorf("limit must be a positive number, got %q", args[1])
	}
	return args[0], n, nil
}

// parseSlackTimestamp converts a Slack message "ts" (seconds.microseconds
// since the epoch, as a string) to a time.Time. It splits on the decimal
// point and parses each half as an integer rather than going through
// float64 — float64 can't represent a 10-digit seconds count plus a
// 6-digit fraction exactly, which would silently lose sub-second precision.
// An unparseable timestamp (which should never happen for a real Slack
// message) falls back to now rather than failing the whole capture.
func parseSlackTimestamp(ts string) time.Time {
	secPart, fracPart, hasFrac := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return time.Now().UTC()
	}
	if !hasFrac {
		return time.Unix(sec, 0).UTC()
	}
	// Pad or truncate the fractional part to exactly 9 digits (nanosecond
	// precision) before parsing, since Slack's "ts" carries microseconds.
	switch {
	case len(fracPart) < 9:
		fracPart += strings.Repeat("0", 9-len(fracPart))
	case len(fracPart) > 9:
		fracPart = fracPart[:9]
	}
	nsec, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Unix(sec, nsec).UTC()
}

// handleSlashCommand parses and executes a `/sev ...` invocation, returning
// the text to post back to the invoking channel.
func (b *bot) handleSlashCommand(ctx context.Context, cmd slack.SlashCommand) string {
	action, args, err := parseAction(cmd.Text)
	if err != nil {
		return err.Error()
	}
	switch action {
	case "open":
		return b.handleOpen(ctx, cmd, args)
	case "update":
		return b.handleUpdate(ctx, cmd, args)
	case "transition":
		return b.handleTransition(ctx, cmd, args)
	case "resolve":
		return b.handleResolve(ctx, cmd, args)
	case "capture":
		return b.handleCapture(ctx, cmd, args)
	default:
		return fmt.Sprintf("unknown command %q\n%s", action, usageText)
	}
}

func (b *bot) handleOpen(ctx context.Context, cmd slack.SlashCommand, args []string) string {
	severity, title, err := parseOpenArgs(args)
	if err != nil {
		return err.Error()
	}
	resp, err := b.api.sevs.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:           title,
		SeverityLevel:   severity,
		DetectionMethod: "slack",
	})
	if err != nil {
		return fmt.Sprintf("failed to open SEV: %v", err)
	}
	return fmt.Sprintf(":rotating_light: Opened %s (SEV-%d): %s", resp.GetId(), severity, resp.GetTitle())
}

func (b *bot) handleUpdate(ctx context.Context, cmd slack.SlashCommand, args []string) string {
	id, text, err := parseIDAndText(args, "usage: `/sev update <sev-id> <message>`")
	if err != nil {
		return err.Error()
	}
	if _, err := b.api.announcements.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    id,
		Message:  fmt.Sprintf("[via Slack from %s]: %s", cmd.UserName, text),
		Audience: string(store.AudienceInternal),
	}); err != nil {
		return fmt.Sprintf("failed to post update on %s: %v", id, err)
	}
	return fmt.Sprintf("Posted update on %s", id)
}

// handleTransition moves a SEV to an arbitrary status, e.g. `open` →
// `investigating` → `mitigated` — the intermediate steps `/sev resolve`
// can't skip, since resolved is only reachable from mitigated
// (internal/sev.ValidateTransition). The state machine itself still enforces
// which from→to moves are legal; this command doesn't second-guess it, it
// just gives Slack users a way to drive any of them without dropping to the
// REST API.
func (b *bot) handleTransition(ctx context.Context, cmd slack.SlashCommand, args []string) string {
	id, toStatus, err := parseTransitionArgs(args)
	if err != nil {
		return err.Error()
	}
	resp, err := b.api.sevs.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       id,
		ToStatus: toStatus,
	})
	if err != nil {
		return fmt.Sprintf("failed to transition %s to %q: %v\nvalid statuses: %s", id, toStatus, err, validStatusNames)
	}
	return fmt.Sprintf(":arrows_counterclockwise: %s is now *%s*: %s", id, resp.GetStatus(), resp.GetTitle())
}

func (b *bot) handleResolve(ctx context.Context, cmd slack.SlashCommand, args []string) string {
	id, err := parseID(args, "usage: `/sev resolve <sev-id>`")
	if err != nil {
		return err.Error()
	}
	resp, err := b.api.sevs.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:         id,
		ToStatus:   string(store.SEVStatusResolved),
		ResolvedAt: timestamppb.Now(),
	})
	if err != nil {
		return fmt.Sprintf("failed to resolve %s: %v", id, err)
	}
	return fmt.Sprintf(":white_check_mark: %s resolved: %s", id, resp.GetTitle())
}

func (b *bot) handleCapture(ctx context.Context, cmd slack.SlashCommand, args []string) string {
	id, limit, err := parseCaptureArgs(args)
	if err != nil {
		return err.Error()
	}
	msgs, err := b.slack.FetchHistory(ctx, cmd.ChannelID, limit)
	if err != nil {
		return fmt.Sprintf("failed to fetch channel history: %v", err)
	}
	captured := 0
	for _, m := range msgs {
		_, err := b.api.chats.AddChatEntry(ctx, &pb.AddChatEntryRequest{
			SevId:      id,
			OccurredAt: timestamppb.New(parseSlackTimestamp(m.Timestamp)),
			Source:     "slack",
			Author:     m.UserID,
			Content:    m.Text,
			AddedBy:    cmd.UserName,
		})
		if err != nil {
			b.log.ErrorContext(ctx, "add chat entry failed during capture", "sev_id", id, "err", err)
			continue
		}
		captured++
	}
	return fmt.Sprintf("Captured %d/%d messages from #%s into %s", captured, len(msgs), cmd.ChannelName, id)
}
