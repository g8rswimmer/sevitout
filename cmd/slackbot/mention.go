package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// mentionPrefix strips the leading "<@BOTID>" markup Slack includes at the
// start of an app_mention event's text.
var mentionPrefix = regexp.MustCompile(`^<@[^>]+>\s*`)

// mentionUsageText is returned whenever an @sevbot mention can't be parsed.
const mentionUsageText = "usage: `@sevbot status <sev-id>` | `@sevbot timeline <sev-id>`"

// handleMention parses and executes an in-channel "@sevbot ..." command
// (docs/requirements.md §13.1), returning the text to post back into the
// channel the mention occurred in.
func (b *bot) handleMention(ctx context.Context, text string) string {
	text = mentionPrefix.ReplaceAllString(text, "")
	action, args, err := parseAction(text)
	if err != nil {
		return mentionUsageText
	}
	switch action {
	case "status":
		return b.handleStatus(ctx, args)
	case "timeline":
		return b.handleTimeline(ctx, args)
	default:
		return fmt.Sprintf("unknown command %q\n%s", action, mentionUsageText)
	}
}

func (b *bot) handleStatus(ctx context.Context, args []string) string {
	id, err := parseID(args, "usage: `@sevbot status <sev-id>`")
	if err != nil {
		return err.Error()
	}
	resp, err := b.api.sevs.GetSEV(ctx, &pb.GetSEVRequest{Id: id})
	if err != nil {
		return fmt.Sprintf("failed to look up %s: %v", id, err)
	}
	return fmt.Sprintf("*%s* (SEV-%d): *%s* — %s", resp.GetId(), resp.GetSeverityLevel(), resp.GetStatus(), resp.GetTitle())
}

func (b *bot) handleTimeline(ctx context.Context, args []string) string {
	id, err := parseID(args, "usage: `@sevbot timeline <sev-id>`")
	if err != nil {
		return err.Error()
	}
	resp, err := b.api.announcements.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{SevId: id})
	if err != nil {
		return fmt.Sprintf("failed to load timeline for %s: %v", id, err)
	}
	if len(resp.GetAnnouncements()) == 0 {
		return fmt.Sprintf("%s has no announcements yet", id)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Timeline for %s:\n", id)
	for _, a := range resp.GetAnnouncements() {
		ts := ""
		if a.GetCreatedAt() != nil {
			ts = a.GetCreatedAt().AsTime().Format("15:04 MST")
		}
		fmt.Fprintf(&out, "• [%s] %s\n", ts, a.GetMessage())
	}
	return out.String()
}
