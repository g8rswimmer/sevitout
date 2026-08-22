package main

import (
	"context"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

func TestIncidentChannelName(t *testing.T) {
	cases := []struct {
		name       string
		convention string
		level      int32
		sevID      string
		want       string
	}{
		{"default convention", "", 1, "SEV-2026-0042", "inc-sev1-sev-2026-0042"},
		{"custom convention", "incidents-{level}-{id}", 2, "SEV-2026-0007", "incidents-2-sev-2026-0007"},
		{"every severity level", "sev{level}", 4, "SEV-2026-0001", "sev4"},
		{"disallowed characters collapsed", "inc {level}/{id}!", 1, "SEV-2026-0001", "inc-1-sev-2026-0001-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := incidentChannelName(c.convention, c.level, c.sevID)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIncidentChannelName_TruncatesToSlackLimit(t *testing.T) {
	got := incidentChannelName("inc-{level}-{id}", 1, "SEV-2026-0000000000000000000000000000000000000000000000000000000000000000000000000000")
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
			{RoleType: "responder", DisplayName: "Bob"}, // wrong role type, must be ignored
		},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "inc-sev{level}-{id}")

	b.createIncidentChannel(context.Background(), "SEV-1", "checkout down", 1)

	if fs.createChannelName != "inc-sev1-sev-1" {
		t.Errorf("created channel name = %q", fs.createChannelName)
	}
	if b.channelFor("SEV-1") == "" {
		t.Error("expected the new channel to be recorded for SEV-1")
	}
	if len(fs.invitedUsers) != 1 || fs.invitedUsers[0] != "U1" {
		t.Errorf("invited users = %v, want [U1] (only the on-call role, resolved by email)", fs.invitedUsers)
	}
	if len(fs.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(fs.posted))
	}
}

func TestCreateIncidentChannel_CreateFailureIsNotFatal(t *testing.T) {
	fs := &fakeSlack{createChannelErr: errAlways}
	b := newTestBot(fs, nil, nil, nil, nil, "", "")

	// Must not panic; must not record a channel mapping for a failed create.
	b.createIncidentChannel(context.Background(), "SEV-1", "title", 1)

	if b.channelFor("SEV-1") != "" {
		t.Error("expected no channel recorded after a failed create")
	}
}

func TestInviteOnCall_NoOnCallRole_InvitesNobody(t *testing.T) {
	fs := &fakeSlack{}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	b.inviteOnCall(context.Background(), "SEV-1", "C123")

	if len(fs.invitedUsers) != 0 {
		t.Errorf("invited users = %v, want none", fs.invitedUsers)
	}
}

func TestInviteOnCall_UnresolvableEmailSkipsInviteWithoutError(t *testing.T) {
	fs := &fakeSlack{emailToUserID: map[string]string{}}
	roles := &fakeRoleAPI{resp: &pb.ListRolesResponse{
		Roles: []*pb.SEVRoleResponse{{RoleType: "on-call", DisplayName: "Alice <alice@example.com>"}},
	}}
	b := newTestBot(fs, nil, roles, nil, nil, "", "")

	b.inviteOnCall(context.Background(), "SEV-1", "C123")

	if len(fs.invitedUsers) != 0 {
		t.Errorf("invited users = %v, want none (no Slack account for that email)", fs.invitedUsers)
	}
}
