package main

import (
	"context"
	"strings"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

func TestHandleMention_StripsMentionMarkup(t *testing.T) {
	sevs := &fakeSevAPI{getResp: &pb.SEVResponse{Id: "SEV-1", Title: "checkout down", Status: "open", SeverityLevel: 1}}
	b := newTestBot(nil, sevs, nil, nil, nil, "", "")

	reply := b.handleMention(context.Background(), "<@U0BOT123> status SEV-1")

	if sevs.lastGetReq.GetId() != "SEV-1" {
		t.Errorf("GetSEV request = %+v", sevs.lastGetReq)
	}
	if !strings.Contains(reply, "checkout down") {
		t.Errorf("reply = %q", reply)
	}
}

func TestHandleMention_UnknownActionReturnsUsage(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	reply := b.handleMention(context.Background(), "<@U0BOT123> frobnicate SEV-1")
	if !strings.Contains(reply, "usage") {
		t.Errorf("reply = %q, want a usage message", reply)
	}
}

func TestHandleMention_Timeline_ListsAnnouncements(t *testing.T) {
	ann := &fakeAnnouncementAPI{listResp: &pb.ListAnnouncementsResponse{
		Announcements: []*pb.AnnouncementResponse{
			{Message: "SEV opened"},
			{Message: "mitigation applied"},
		},
	}}
	b := newTestBot(nil, nil, nil, ann, nil, "", "")

	reply := b.handleMention(context.Background(), "<@U0BOT123> timeline SEV-1")

	if !strings.Contains(reply, "SEV opened") || !strings.Contains(reply, "mitigation applied") {
		t.Errorf("reply = %q, want both announcements listed", reply)
	}
}

func TestHandleMention_Timeline_EmptyReportsNoAnnouncements(t *testing.T) {
	ann := &fakeAnnouncementAPI{listResp: &pb.ListAnnouncementsResponse{}}
	b := newTestBot(nil, nil, nil, ann, nil, "", "")

	reply := b.handleMention(context.Background(), "<@U0BOT123> timeline SEV-1")

	if !strings.Contains(reply, "no announcements") {
		t.Errorf("reply = %q", reply)
	}
}
