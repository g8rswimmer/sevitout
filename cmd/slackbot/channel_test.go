package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

func TestIncidentChannelName(t *testing.T) {
	cases := []struct {
		name       string
		convention string
		level      int32
		sevID      string
		title      string
		want       string
	}{
		{"default convention", "", 1, "SEV-2026-0042", "database outage", "inc-sev-2026-0042-database-outage"},
		{"custom convention", "incidents-{level}-{id}", 2, "SEV-2026-0007", "", "incidents-2-sev-2026-0007"},
		{"every severity level", "sev{level}", 4, "SEV-2026-0001", "", "sev4"},
		{"disallowed characters collapsed", "inc {level}/{id}!", 1, "SEV-2026-0001", "", "inc-1-sev-2026-0001"},
		{"punctuated title doesn't produce repeated hyphens", "inc-{id}-{title}", 1, "SEV-1", "Database outage - prod", "inc-sev-1-database-outage-prod"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := incidentChannelName(c.convention, c.level, c.sevID, c.title)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIncidentChannelName_TruncatesToSlackLimit(t *testing.T) {
	got := incidentChannelName("inc-{level}-{id}", 1, "SEV-2026-0000000000000000000000000000000000000000000000000000000000000000000000000000", "")
	if len(got) != slackChannelNameMaxLen {
		t.Errorf("len(got) = %d, want %d", len(got), slackChannelNameMaxLen)
	}
}

func TestEmailInAngleBrackets(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Alice <alice@example.com>", "alice@example.com"},
		{"On-Call Team", ""},
		{"", ""},
	}
	for _, c := range cases {
		m := emailInAngleBrackets.FindStringSubmatch(c.in)
		got := ""
		if len(m) == 2 {
			got = m[1]
		}
		if got != c.want {
			t.Errorf("emailInAngleBrackets(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCreateIncidentChannel_CreatesInvitesAndPostsLink(t *testing.T) {
	fs := &fakeSlack{
		emailToUserID: map[string]string{"alice@example.com": "U1"},
	}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{
			{RoleType: "on-call", DisplayName: "Alice <alice@example.com>"},
			{RoleType: "responder", DisplayName: "Bob"}, // unresolvable, must be silently skipped
		},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "inc-sev{level}-{id}")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if fs.createChannelName != "inc-sev1-sev-1" {
		t.Errorf("created channel name = %q", fs.createChannelName)
	}
	if b.channelFor("SEV-1") == "" {
		t.Error("expected the new channel to be recorded for SEV-1")
	}
	if len(fs.invitedUsers) != 1 || fs.invitedUsers[0] != "U1" {
		t.Errorf("invited users = %v, want [U1] (Alice resolved by DisplayName-regex email; Bob unresolvable)", fs.invitedUsers)
	}
	if len(fs.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(fs.posted))
	}
}

// TestCreateIncidentChannel_WritesBackSlackChannelID asserts the Phase 10e
// UpdateSEV write-back happens right after channel creation, with the new
// channel ID.
func TestCreateIncidentChannel_WritesBackSlackChannelID(t *testing.T) {
	fs := &fakeSlack{createChannelID: "C-NEW-1"}
	sevs := &fakeSevAPI{}
	b := newTestBot(fs, sevs, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if sevs.lastUpdateReq == nil {
		t.Fatal("expected UpdateSEV to be called")
	}
	if sevs.lastUpdateReq.GetId() != "SEV-1" || sevs.lastUpdateReq.GetSlackChannelId() != "C-NEW-1" {
		t.Errorf("UpdateSEV req = %+v, want {Id: SEV-1, SlackChannelId: C-NEW-1}", sevs.lastUpdateReq)
	}
}

// TestCreateIncidentChannel_UpdateSEVFailureIsNotFatal asserts a failed
// write-back doesn't block the rest of channel creation (best-effort).
func TestCreateIncidentChannel_UpdateSEVFailureIsNotFatal(t *testing.T) {
	fs := &fakeSlack{}
	sevs := &fakeSevAPI{updateErr: errAlways}
	b := newTestBot(fs, sevs, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")

	// Must not panic.
	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if b.channelFor("SEV-1") == "" {
		t.Error("expected the channel to still be recorded despite the UpdateSEV failure")
	}
}

// TestResolveRoleHolderSlackIDs_ResolvesViaStoredSlackUserID exercises the
// highest-priority resolution path (§10d): a role's UserID resolves through
// one batch ListUserDirectory call to a stored SlackUserID, used directly
// without any LookupUserIDByEmail call.
func TestResolveRoleHolderSlackIDs_ResolvesViaStoredSlackUserID(t *testing.T) {
	fs := &fakeSlack{}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{Id: 1, RoleType: "incident-commander", UserId: "user-1", DisplayName: "Alice"}},
	}}
	dir := &fakeDirectoryAPI{resp: &pb.ListUserDirectoryResponse{
		Users: []*pb.DirectoryUser{{Id: "user-1", Name: "Alice", Email: "alice@example.com", SlackUserId: "U-STORED"}},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")
	b.api.directory = dir

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if !reflect.DeepEqual(got, []string{"U-STORED"}) {
		t.Errorf("resolved = %v, want [U-STORED]", got)
	}
	if dir.lastReq == nil || len(dir.lastReq.GetIds()) != 1 || dir.lastReq.GetIds()[0] != "user-1" {
		t.Errorf("directory lookup ids = %v, want [user-1]", dir.lastReq.GetIds())
	}
}

// TestResolveRoleHolderSlackIDs_FallsBackToEmailLookupWhenNoStoredSlackUserID
// exercises the second-priority path: UserID resolves via the directory,
// but that user has no stored SlackUserID, so the directory-returned email
// is looked up instead.
func TestResolveRoleHolderSlackIDs_FallsBackToEmailLookupWhenNoStoredSlackUserID(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{"alice@example.com": "U-EMAIL"}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{Id: 1, RoleType: "responder", UserId: "user-1", DisplayName: "Alice"}},
	}}
	dir := &fakeDirectoryAPI{resp: &pb.ListUserDirectoryResponse{
		Users: []*pb.DirectoryUser{{Id: "user-1", Name: "Alice", Email: "alice@example.com"}}, // no SlackUserId
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")
	b.api.directory = dir

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if !reflect.DeepEqual(got, []string{"U-EMAIL"}) {
		t.Errorf("resolved = %v, want [U-EMAIL]", got)
	}
}

// TestResolveRoleHolderSlackIDs_NoUserIDFallsBackToDisplayNameRegex
// exercises the third-priority path: a role with no UserID (e.g. an older
// or free-text-only assignment) falls back to the DisplayName regex scrape.
func TestResolveRoleHolderSlackIDs_NoUserIDFallsBackToDisplayNameRegex(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{"bob@example.com": "U-BOB"}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{Id: 1, RoleType: "comms-lead", DisplayName: "Bob <bob@example.com>"}},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if !reflect.DeepEqual(got, []string{"U-BOB"}) {
		t.Errorf("resolved = %v, want [U-BOB]", got)
	}
}

// TestResolveRoleHolderSlackIDs_EveryRoleTypeCovered asserts the RoleType
// filter present before Phase 10d (on-call only) is gone.
func TestResolveRoleHolderSlackIDs_EveryRoleTypeCovered(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{
		"a@example.com": "U-A", "b@example.com": "U-B", "c@example.com": "U-C",
	}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{
			{RoleType: "on-call", DisplayName: "A <a@example.com>"},
			{RoleType: "detected-by", DisplayName: "B <b@example.com>"},
			{RoleType: "recorder", DisplayName: "C <c@example.com>"},
		},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if len(got) != 3 {
		t.Errorf("resolved = %v, want 3 (every role type)", got)
	}
}

func TestResolveRoleHolderSlackIDs_NoRoles_ResolvesNobody(t *testing.T) {
	fs := &fakeSlack{}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if len(got) != 0 {
		t.Errorf("resolved = %v, want none", got)
	}
}

func TestResolveRoleHolderSlackIDs_UnresolvableEmailSkipsWithoutError(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{RoleType: "on-call", DisplayName: "Alice <alice@example.com>"}},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	got := b.resolveRoleHolderSlackIDs(context.Background(), "SEV-1")

	if len(got) != 0 {
		t.Errorf("resolved = %v, want none (no Slack account for that email)", got)
	}
}

func TestCreateIncidentChannel_InvitesPendingOpener(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")
	b.channelOrRegisterOpener("SEV-1", "U-OPENER")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if len(fs.invitedUsers) != 1 || fs.invitedUsers[0] != "U-OPENER" {
		t.Errorf("invited users = %v, want [U-OPENER]", fs.invitedUsers)
	}
	if opener := b.takePendingOpener("SEV-1"); opener != "" {
		t.Errorf("pending opener = %q, want it consumed by createIncidentChannel", opener)
	}
}

func TestCreateIncidentChannel_NoPendingOpenerInvitesNobodyExtra(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if len(fs.invitedUsers) != 0 {
		t.Errorf("invited users = %v, want none", fs.invitedUsers)
	}
}

func TestCreateIncidentChannel_CreateFailureIsNotFatal(t *testing.T) {
	fs := &fakeSlack{createChannelErr: errAlways}
	b := newTestBot(fs, nil, nil, nil, nil, "", "")

	// Must not panic; must not record a channel mapping for a failed create.
	b.createIncidentChannel(context.Background(), "SEV-1", "title", 1, "")

	if b.channelFor("SEV-1") != "" {
		t.Error("expected no channel recorded after a failed create")
	}
}

// ── Phase 11d: SEV-creator invite ────────────────────────────────────────────

// TestCreateIncidentChannel_InvitesCreatorViaStoredSlackUserID exercises the
// core bug report this closes: a user with a stored SlackUserID who creates
// a SEV (e.g. from the web UI) is invited to its incident channel, even
// when they hold no role on it.
func TestCreateIncidentChannel_InvitesCreatorViaStoredSlackUserID(t *testing.T) {
	fs := &fakeSlack{}
	dir := &fakeDirectoryAPI{resp: &pb.ListUserDirectoryResponse{
		Users: []*pb.DirectoryUser{{Id: "user-1", Name: "Alice", Email: "alice@example.com", SlackUserId: "U-CREATOR"}},
	}}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")
	b.api.directory = dir

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "user-1")

	if !reflect.DeepEqual(fs.invitedUsers, []string{"U-CREATOR"}) {
		t.Errorf("invited users = %v, want [U-CREATOR]", fs.invitedUsers)
	}
	if dir.lastReq == nil || !reflect.DeepEqual(dir.lastReq.GetIds(), []string{"user-1"}) {
		t.Errorf("directory lookup ids = %v, want [user-1]", dir.lastReq.GetIds())
	}
}

// TestCreateIncidentChannel_InvitesCreatorViaEmailLookup exercises the
// fallback path: no stored SlackUserID, so the directory-returned email is
// looked up instead.
func TestCreateIncidentChannel_InvitesCreatorViaEmailLookup(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{"alice@example.com": "U-EMAIL"}}
	dir := &fakeDirectoryAPI{resp: &pb.ListUserDirectoryResponse{
		Users: []*pb.DirectoryUser{{Id: "user-1", Name: "Alice", Email: "alice@example.com"}},
	}}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")
	b.api.directory = dir

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "user-1")

	if !reflect.DeepEqual(fs.invitedUsers, []string{"U-EMAIL"}) {
		t.Errorf("invited users = %v, want [U-EMAIL]", fs.invitedUsers)
	}
}

// TestCreateIncidentChannel_EmptyCreatedByInvitesNobodyExtra covers the
// `/sev open` known-limitation case (the slash-command handler never sets
// created_by, so it's usually empty or the bot's own service-account ID)
// and the general "no creator identity available" case: no directory call,
// no error, nobody extra invited.
func TestCreateIncidentChannel_EmptyCreatedByInvitesNobodyExtra(t *testing.T) {
	fs := &fakeSlack{}
	dir := &fakeDirectoryAPI{}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: &pb.ListRolesResponse{}}, nil, nil, "", "")
	b.api.directory = dir

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "")

	if len(fs.invitedUsers) != 0 {
		t.Errorf("invited users = %v, want none", fs.invitedUsers)
	}
	if dir.lastReq != nil {
		t.Error("expected no directory lookup for an empty created_by")
	}
}

// TestCreateIncidentChannel_CreatorLookupFailureIsNotFatal asserts a failed
// directory lookup for the creator doesn't block channel creation or the
// rest of the invite list (best-effort).
func TestCreateIncidentChannel_CreatorLookupFailureIsNotFatal(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{"alice@example.com": "U1"}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{RoleType: "on-call", DisplayName: "Alice <alice@example.com>"}},
	}}
	dir := &fakeDirectoryAPI{err: errAlways}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")
	b.api.directory = dir

	// Must not panic; the role holder must still be invited despite the
	// creator-lookup failure.
	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "user-1")

	if !reflect.DeepEqual(fs.invitedUsers, []string{"U1"}) {
		t.Errorf("invited users = %v, want [U1]", fs.invitedUsers)
	}
}

// TestCreateIncidentChannel_DedupesAndInvitesOnce covers the combined-call
// requirement: a role holder who is also the SEV's creator and also the
// slash-command opener is invited exactly once, via exactly one InviteUsers
// call.
func TestCreateIncidentChannel_DedupesAndInvitesOnce(t *testing.T) {
	fs := &fakeSlack{}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{Id: 1, RoleType: "responder", UserId: "user-1"}},
	}}
	dir := &fakeDirectoryAPI{resp: &pb.ListUserDirectoryResponse{
		Users: []*pb.DirectoryUser{{Id: "user-1", Name: "Alice", Email: "alice@example.com", SlackUserId: "U-SAME"}},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")
	b.api.directory = dir
	b.channelOrRegisterOpener("SEV-1", "U-SAME")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1, "user-1")

	if !reflect.DeepEqual(fs.invitedUsers, []string{"U-SAME"}) {
		t.Errorf("invited users = %v, want [U-SAME] exactly once", fs.invitedUsers)
	}
	if fs.inviteCalls != 1 {
		t.Errorf("InviteUsers called %d times, want 1 (combined call)", fs.inviteCalls)
	}
}

func TestDedupeStrings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"no dupes", []string{"A", "B"}, []string{"A", "B"}},
		{"dupes collapsed, order preserved", []string{"A", "B", "A", "C", "B"}, []string{"A", "B", "C"}},
		{"empty strings dropped", []string{"", "A", ""}, []string{"A"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dedupeStrings(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}
