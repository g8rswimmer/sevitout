package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// channelNameDisallowed matches everything Slack does not allow in a channel
// name (only lowercase letters, numbers, hyphens, and underscores).
var channelNameDisallowed = regexp.MustCompile(`[^a-z0-9_-]+`)

// repeatedHyphens collapses runs of hyphens left behind when a punctuated
// title (e.g. "Database outage - prod") gets slugified — the disallowed
// spaces around a literal "-" each become their own hyphen, which would
// otherwise render as "---" in the channel name.
var repeatedHyphens = regexp.MustCompile(`-{2,}`)

// slackChannelNameMaxLen is Slack's own channel-name length limit.
const slackChannelNameMaxLen = 80

// incidentChannelName renders convention (e.g. "inc-{id}-{title}",
// docs/requirements.md §13.1) into a valid Slack channel name for the given
// SEV, substituting {level}, {id}, and {title}. An empty convention falls
// back to defaultChannelNamingConvention. The result is lowercased, has
// every Slack-disallowed character collapsed to a hyphen, has repeated
// hyphens collapsed and leading/trailing hyphens trimmed, and is truncated
// to Slack's 80-character channel-name limit.
func incidentChannelName(convention string, severityLevel int32, sevID, title string) string {
	if convention == "" {
		convention = defaultChannelNamingConvention
	}
	name := strings.NewReplacer(
		"{level}", strconv.Itoa(int(severityLevel)),
		"{id}", sevID,
		"{title}", title,
	).Replace(convention)
	name = strings.ToLower(name)
	name = channelNameDisallowed.ReplaceAllString(name, "-")
	name = repeatedHyphens.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
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

// createIncidentChannel creates a dedicated Slack channel for every newly
// opened SEV (docs/requirements.md §13.1), invites every assigned role's
// holder (docs/roadmap.md Phase 10d — widened from on-call-only), the SEV's
// creator (Phase 11d — resolved the same way as a role holder), and
// whoever opened it via `/sev open` (if anyone — see takePendingOpener),
// posts a link back to the SEV, records the mapping so future lifecycle
// notifications for this SEV land in the new channel instead of the default
// one, and writes the channel ID back onto the SEV record (Phase 10e) so
// cmd/server can act on it directly (e.g. RoleService.InviteRoleToSlack)
// without depending on this bot's in-memory-only copy.
//
// createdBy is the SEV's CreatedBy (a Sevitout user ID, from the
// sev.created WebSocket payload) — see resolveCreatorSlackUserID's doc
// comment for why it isn't always the actual human who opened the SEV.
//
// All three invitee sources — role holders, the creator, and the pending
// opener — are combined and deduplicated by resolved Slack user ID into one
// InviteUsers call, rather than one call per source.
//
// Best-effort throughout: a failure at any step is logged, not returned,
// since incident-channel creation must never be the reason a SEV-open
// response fails or blocks.
func (b *bot) createIncidentChannel(ctx context.Context, sevID, title string, severityLevel int32, createdBy string) {
	name := incidentChannelName(b.namingConvention(), severityLevel, sevID, title)

	channelID, err := b.slack.CreateChannel(ctx, name)
	if err != nil {
		b.log.ErrorContext(ctx, "auto-create incident channel failed", "sev_id", sevID, "channel_name", name, "err", err)
		return
	}
	b.setChannelFor(sevID, channelID)
	b.log.InfoContext(ctx, "auto-created incident channel", "sev_id", sevID, "channel_id", channelID, "channel_name", name)

	if _, err := b.api.sevs.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: sevID, SlackChannelId: channelID}); err != nil {
		b.log.ErrorContext(ctx, "write back slack_channel_id failed", "sev_id", sevID, "channel_id", channelID, "err", err)
	}

	userIDs := b.resolveRoleHolderSlackIDs(ctx, sevID)

	creatorID, err := b.resolveCreatorSlackUserID(ctx, createdBy)
	if err != nil {
		b.log.ErrorContext(ctx, "resolve sev creator Slack identity failed", "sev_id", sevID, "created_by", createdBy, "err", err)
	} else if creatorID != "" {
		userIDs = append(userIDs, creatorID)
	}

	if opener := b.takePendingOpener(sevID); opener != "" {
		userIDs = append(userIDs, opener)
	}

	if err := b.slack.InviteUsers(ctx, channelID, dedupeStrings(userIDs)); err != nil {
		b.log.ErrorContext(ctx, "invite participants to new incident channel failed", "sev_id", sevID, "channel_id", channelID, "err", err)
	}

	if err := b.slack.PostMessage(ctx, channelID, fmt.Sprintf(":rotating_light: %s\n%s", title, sevID)); err != nil {
		b.log.ErrorContext(ctx, "post incident channel intro failed", "sev_id", sevID, "channel_id", channelID, "err", err)
	}
}

// resolveRoleHolderSlackIDs looks up every role assigned to sevID (any role
// type — generalized from on-call-only, docs/roadmap.md Phase 10d) and
// resolves each holder to a Slack user ID, in order:
//
//  1. SEVRole.UserID set → batch-resolved via one ListUserDirectory(ids)
//     call for every role → a stored SlackUserID is used directly.
//  2. UserID set but no stored SlackUserID → LookupUserIDByEmail(the
//     directory-returned email).
//  3. No UserID (an older or free-text-only assignment) → the original
//     emailInAngleBrackets regex scrape of DisplayName.
//  4. Otherwise → skipped.
//
// Not every role carries a resolvable identity and not every Sevitout user
// has a Slack account — both are silently skipped rather than treated as
// errors. Resolution only; callers decide when/how to invite the result.
func (b *bot) resolveRoleHolderSlackIDs(ctx context.Context, sevID string) []string {
	resp, err := b.api.roles.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if err != nil {
		b.log.ErrorContext(ctx, "list roles for incident channel invite failed", "sev_id", sevID, "err", err)
		return nil
	}
	roles := resp.GetRoles()

	// One batch directory lookup for every UserID-carrying role, rather than
	// one ListUserDirectory call per role.
	var ids []string
	for _, r := range roles {
		if r.GetUserId() != "" {
			ids = append(ids, r.GetUserId())
		}
	}
	directory := make(map[string]*pb.DirectoryUser, len(ids))
	if len(ids) > 0 && b.api.directory != nil {
		dirResp, err := b.api.directory.ListUserDirectory(ctx, &pb.ListUserDirectoryRequest{Ids: ids})
		if err != nil {
			b.log.ErrorContext(ctx, "list user directory for incident channel invite failed", "sev_id", sevID, "err", err)
		} else {
			for _, u := range dirResp.GetUsers() {
				directory[u.GetId()] = u
			}
		}
	}

	var userIDs []string
	for _, r := range roles {
		userID, err := b.resolveRoleSlackUserID(ctx, r, directory)
		if err != nil {
			b.log.ErrorContext(ctx, "resolve role holder Slack identity failed", "sev_id", sevID, "role_id", r.GetId(), "err", err)
			continue
		}
		if userID != "" {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs
}

// resolveCreatorSlackUserID resolves createdBy (a SEV's CreatedBy — a
// Sevitout user ID) to a Slack user ID: a single-ID ListUserDirectory
// lookup, then a stored SlackUserID if present, else
// LookupUserIDByEmail(the directory-returned email) — the same order
// resolveRoleSlackUserID uses for a role's UserID. Returns ("", nil) — not
// an error — when createdBy is empty or nothing resolves.
//
// createdBy is not always the actual human who opened the SEV: a SEV
// created via `/sev open` authenticates as this bot's own service account
// (the slash-command handler never sets created_by, so the server fills it
// in from the caller's identity — see CreateSEV), so createdBy there
// resolves to the bot's own directory entry, which typically has no Slack
// identity — a harmless no-op, not a duplicate of the human, who is invited
// separately via takePendingOpener/the pending-opener path instead.
func (b *bot) resolveCreatorSlackUserID(ctx context.Context, createdBy string) (string, error) {
	if createdBy == "" || b.api.directory == nil {
		return "", nil
	}
	resp, err := b.api.directory.ListUserDirectory(ctx, &pb.ListUserDirectoryRequest{Ids: []string{createdBy}})
	if err != nil {
		return "", err
	}
	users := resp.GetUsers()
	if len(users) == 0 {
		return "", nil
	}
	du := users[0]
	if du.GetSlackUserId() != "" {
		return du.GetSlackUserId(), nil
	}
	if du.GetEmail() != "" {
		return b.slack.LookupUserIDByEmail(ctx, du.GetEmail())
	}
	return "", nil
}

// dedupeStrings returns ids with empty strings and repeats removed,
// preserving first-occurrence order — used to collapse createIncidentChannel's
// combined role-holder/creator/opener invite list into one InviteUsers call
// with no duplicate Slack API invites (e.g. a role holder who is also the
// SEV's creator).
func dedupeStrings(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// resolveRoleSlackUserID resolves one role assignment to a Slack user ID
// per resolveRoleHolderSlackIDs' doc comment's four-step order. directory is the
// batch ListUserDirectory result, keyed by user ID. Returns ("", nil) — not
// an error — when nothing resolves.
func (b *bot) resolveRoleSlackUserID(ctx context.Context, r *pb.SEVRoleResponse, directory map[string]*pb.DirectoryUser) (string, error) {
	if uid := r.GetUserId(); uid != "" {
		du, ok := directory[uid]
		if !ok {
			return "", nil
		}
		if du.GetSlackUserId() != "" {
			return du.GetSlackUserId(), nil
		}
		if du.GetEmail() != "" {
			return b.slack.LookupUserIDByEmail(ctx, du.GetEmail())
		}
		return "", nil
	}
	m := emailInAngleBrackets.FindStringSubmatch(r.GetDisplayName())
	if len(m) != 2 {
		return "", nil
	}
	return b.slack.LookupUserIDByEmail(ctx, m[1])
}
