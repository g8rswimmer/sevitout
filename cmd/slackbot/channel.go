package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// channelNameDisallowed matches everything Slack does not allow in a channel
// name (only lowercase letters, numbers, hyphens, and underscores).
var channelNameDisallowed = regexp.MustCompile(`[^a-z0-9_-]+`)

// slackChannelNameMaxLen is Slack's own channel-name length limit.
const slackChannelNameMaxLen = 80

// incidentChannelName renders convention (e.g. "inc-sev{level}-{id}",
// docs/requirements.md §13.1) into a valid Slack channel name for the given
// SEV, substituting {level} and {id}. An empty convention falls back to
// defaultChannelNamingConvention. The result is lowercased, has every
// Slack-disallowed character collapsed to a hyphen, and is truncated to
// Slack's 80-character channel-name limit.
func incidentChannelName(convention string, severityLevel int32, sevID string) string {
	if convention == "" {
		convention = defaultChannelNamingConvention
	}
	name := strings.NewReplacer(
		"{level}", strconv.Itoa(int(severityLevel)),
		"{id}", sevID,
	).Replace(convention)
	name = strings.ToLower(name)
	name = channelNameDisallowed.ReplaceAllString(name, "-")
	if len(name) > slackChannelNameMaxLen {
		name = name[:slackChannelNameMaxLen]
	}
	return name
}

// emailInAngleBrackets pulls the email out of an on-call display name of the
// form "Alice <alice@example.com>" — the shape internal/integrations/pagerduty.
// Client.OnCallLookup produces and internal/api/grpc's on-call auto-assign
// stores verbatim as SEVRole.DisplayName.
var emailInAngleBrackets = regexp.MustCompile(`<([^>@\s]+@[^>]+)>`)

// createIncidentChannel creates a dedicated Slack channel for a newly opened
// SEV-1/SEV-2 (docs/requirements.md §13.1), invites its on-call person (if
// one is assigned and resolvable to a Slack account), posts a link back to
// the SEV, and records the mapping so future lifecycle notifications for
// this SEV land in the new channel instead of the default one.
//
// Best-effort throughout: a failure at any step is logged, not returned,
// since incident-channel creation must never be the reason a SEV-open
// response fails or blocks.
func (b *bot) createIncidentChannel(ctx context.Context, sevID, title string, severityLevel int32) {
	name := incidentChannelName(b.namingConvention(), severityLevel, sevID)

	channelID, err := b.slack.CreateChannel(ctx, name)
	if err != nil {
		b.log.ErrorContext(ctx, "auto-create incident channel failed", "sev_id", sevID, "channel_name", name, "err", err)
		return
	}
	b.setChannelFor(sevID, channelID)
	b.log.InfoContext(ctx, "auto-created incident channel", "sev_id", sevID, "channel_id", channelID, "channel_name", name)

	b.inviteOnCall(ctx, sevID, channelID)

	if err := b.slack.PostMessage(ctx, channelID, fmt.Sprintf(":rotating_light: %s\n%s", title, sevID)); err != nil {
		b.log.ErrorContext(ctx, "post incident channel intro failed", "sev_id", sevID, "channel_id", channelID, "err", err)
	}
}

// inviteOnCall looks up sevID's on-call role (if any) and invites the
// matching Slack user (resolved by email) into channelID. Not every on-call
// entry carries a resolvable email (a free-form team name, e.g.) and not
// every Sevitout user has a Slack account — both are silently skipped rather
// than treated as errors.
func (b *bot) inviteOnCall(ctx context.Context, sevID, channelID string) {
	resp, err := b.api.roles.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if err != nil {
		b.log.ErrorContext(ctx, "list roles for incident channel invite failed", "sev_id", sevID, "err", err)
		return
	}

	var userIDs []string
	for _, r := range resp.GetRoles() {
		if r.GetRoleType() != string(store.SEVRoleOnCall) {
			continue
		}
		m := emailInAngleBrackets.FindStringSubmatch(r.GetDisplayName())
		if len(m) != 2 {
			continue
		}
		userID, err := b.slack.LookupUserIDByEmail(ctx, m[1])
		if err != nil {
			b.log.ErrorContext(ctx, "look up on-call Slack user failed", "sev_id", sevID, "email", m[1], "err", err)
			continue
		}
		if userID != "" {
			userIDs = append(userIDs, userID)
		}
	}

	if err := b.slack.InviteUsers(ctx, channelID, userIDs); err != nil {
		b.log.ErrorContext(ctx, "invite on-call to incident channel failed", "sev_id", sevID, "channel_id", channelID, "err", err)
	}
}
