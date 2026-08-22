package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	sevitoutslack "github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

func TestParseAction_Empty(t *testing.T) {
	if _, _, err := parseAction(""); err == nil {
		t.Error("expected error for empty text")
	}
	if _, _, err := parseAction("   "); err == nil {
		t.Error("expected error for whitespace-only text")
	}
}

func TestParseAction_SplitsActionAndArgs(t *testing.T) {
	action, args, err := parseAction("Open 2 database is down")
	if err != nil {
		t.Fatalf("parseAction: %v", err)
	}
	if action != "open" {
		t.Errorf("action = %q, want lowercased %q", action, "open")
	}
	if strings.Join(args, " ") != "2 database is down" {
		t.Errorf("args = %v", args)
	}
}

func TestParseOpenArgs(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantSeverity int32
		wantTitle    string
		wantErr      bool
	}{
		{"no args", nil, 0, "", true},
		{"title only defaults to severity 3", []string{"checkout", "is", "down"}, 3, "checkout is down", false},
		{"leading severity consumed", []string{"1", "checkout", "down"}, 1, "checkout down", false},
		{"severity out of range", []string{"9", "title"}, 0, "", true},
		{"severity with nothing after it", []string{"2"}, 0, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sev, title, err := parseOpenArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOpenArgs: %v", err)
			}
			if sev != c.wantSeverity || title != c.wantTitle {
				t.Errorf("got (%d, %q), want (%d, %q)", sev, title, c.wantSeverity, c.wantTitle)
			}
		})
	}
}

func TestParseIDAndText(t *testing.T) {
	if _, _, err := parseIDAndText([]string{"SEV-1"}, "usage"); err == nil {
		t.Error("expected error when text is missing")
	}
	id, text, err := parseIDAndText([]string{"SEV-1", "mitigating", "now"}, "usage")
	if err != nil {
		t.Fatalf("parseIDAndText: %v", err)
	}
	if id != "SEV-1" || text != "mitigating now" {
		t.Errorf("got (%q, %q)", id, text)
	}
}

func TestParseID(t *testing.T) {
	if _, err := parseID(nil, "usage"); err == nil {
		t.Error("expected error for no args")
	}
	id, err := parseID([]string{"SEV-1", "ignored"}, "usage")
	if err != nil {
		t.Fatalf("parseID: %v", err)
	}
	if id != "SEV-1" {
		t.Errorf("id = %q, want SEV-1 (extra tokens ignored)", id)
	}
}

func TestParseCaptureArgs(t *testing.T) {
	if _, _, err := parseCaptureArgs(nil); err == nil {
		t.Error("expected error for no args")
	}
	id, limit, err := parseCaptureArgs([]string{"SEV-1"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if id != "SEV-1" || limit != defaultCaptureLimit {
		t.Errorf("got (%q, %d), want (%q, %d)", id, limit, "SEV-1", defaultCaptureLimit)
	}
	id, limit, err = parseCaptureArgs([]string{"SEV-1", "5"})
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}
	if id != "SEV-1" || limit != 5 {
		t.Errorf("got (%q, %d), want (%q, 5)", id, limit, "SEV-1")
	}
	if _, _, err := parseCaptureArgs([]string{"SEV-1", "not-a-number"}); err == nil {
		t.Error("expected error for non-numeric limit")
	}
	if _, _, err := parseCaptureArgs([]string{"SEV-1", "0"}); err == nil {
		t.Error("expected error for non-positive limit")
	}
}

func TestParseSlackTimestamp(t *testing.T) {
	got := parseSlackTimestamp("1700000000.000100")
	want := time.Unix(1700000000, 100000).UTC()
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSlackTimestamp_Unparseable_FallsBackToNow(t *testing.T) {
	got := parseSlackTimestamp("not-a-timestamp")
	if time.Since(got) > time.Minute {
		t.Errorf("expected a recent fallback time, got %v", got)
	}
}

func TestHandleOpen_CreatesSEVAndReportsID(t *testing.T) {
	sevs := &fakeSevAPI{createResp: &pb.SEVResponse{Id: "SEV-2026-0001", Title: "checkout down"}}
	b := newTestBot(nil, sevs, nil, nil, nil, "", "")

	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{Text: "open 1 checkout down"})

	if sevs.lastCreateReq.GetSeverityLevel() != 1 || sevs.lastCreateReq.GetTitle() != "checkout down" {
		t.Errorf("CreateSEV request = %+v", sevs.lastCreateReq)
	}
	if !strings.Contains(reply, "SEV-2026-0001") {
		t.Errorf("reply = %q, want it to mention the new SEV ID", reply)
	}
}

func TestHandleOpen_InvalidArgsReturnsUsage(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{Text: "open"})
	if !strings.Contains(reply, "usage") {
		t.Errorf("reply = %q, want a usage message", reply)
	}
}

func TestHandleOpen_APIErrorReportedInReply(t *testing.T) {
	sevs := &fakeSevAPI{createErr: errAlways}
	b := newTestBot(nil, sevs, nil, nil, nil, "", "")
	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{Text: "open title here"})
	if !strings.Contains(reply, "failed") {
		t.Errorf("reply = %q, want a failure message", reply)
	}
}

func TestHandleUpdate_PostsInternalAnnouncement(t *testing.T) {
	ann := &fakeAnnouncementAPI{createResp: &pb.AnnouncementResponse{}}
	b := newTestBot(nil, nil, nil, ann, nil, "", "")

	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{
		Text: "update SEV-1 mitigation in progress", UserName: "alice",
	})

	if ann.lastCreateReq.GetSevId() != "SEV-1" {
		t.Errorf("SevId = %q, want SEV-1", ann.lastCreateReq.GetSevId())
	}
	if ann.lastCreateReq.GetAudience() != "internal" {
		t.Errorf("Audience = %q, want internal", ann.lastCreateReq.GetAudience())
	}
	if !strings.Contains(ann.lastCreateReq.GetMessage(), "mitigation in progress") {
		t.Errorf("Message = %q, want it to contain the update text", ann.lastCreateReq.GetMessage())
	}
	if !strings.Contains(reply, "SEV-1") {
		t.Errorf("reply = %q", reply)
	}
}

func TestHandleResolve_TransitionsToResolved(t *testing.T) {
	sevs := &fakeSevAPI{transResp: &pb.SEVResponse{Id: "SEV-1", Title: "checkout down"}}
	b := newTestBot(nil, sevs, nil, nil, nil, "", "")

	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{Text: "resolve SEV-1"})

	if sevs.lastTransReq.GetId() != "SEV-1" || sevs.lastTransReq.GetToStatus() != "resolved" {
		t.Errorf("TransitionStatus request = %+v", sevs.lastTransReq)
	}
	if !strings.Contains(reply, "resolved") {
		t.Errorf("reply = %q", reply)
	}
}

func TestHandleCapture_AddsEachMessageToChatLog(t *testing.T) {
	fs := &fakeSlack{history: []sevitoutslack.Message{
		{UserID: "U1", Text: "investigating now", Timestamp: "1700000000.000000"},
		{UserID: "U2", Text: "found the culprit", Timestamp: "1700000010.000000"},
	}}
	chats := &fakeChatAPI{}
	b := newTestBot(fs, nil, nil, nil, chats, "", "")

	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{
		Text: "capture SEV-1", ChannelID: "C123", ChannelName: "incident-room", UserName: "alice",
	})

	if len(chats.entries) != 2 {
		t.Fatalf("added %d chat entries, want 2", len(chats.entries))
	}
	for _, e := range chats.entries {
		if e.GetSevId() != "SEV-1" || e.GetSource() != "slack" || e.GetAddedBy() != "alice" {
			t.Errorf("entry = %+v", e)
		}
	}
	if !strings.Contains(reply, "2/2") {
		t.Errorf("reply = %q, want it to report 2/2 captured", reply)
	}
}

func TestHandleCapture_SkipsFailedEntriesButReportsPartialCount(t *testing.T) {
	fs := &fakeSlack{history: []sevitoutslack.Message{
		{UserID: "U1", Text: "one", Timestamp: "1700000000.0"},
		{UserID: "U2", Text: "two", Timestamp: "1700000001.0"},
	}}
	chats := &fakeChatAPI{err: errAlways}
	b := newTestBot(fs, nil, nil, nil, chats, "", "")

	reply := b.handleSlashCommand(context.Background(), slack.SlashCommand{Text: "capture SEV-1"})

	if !strings.Contains(reply, "0/2") {
		t.Errorf("reply = %q, want it to report 0/2 captured when every AddChatEntry call fails", reply)
	}
}
