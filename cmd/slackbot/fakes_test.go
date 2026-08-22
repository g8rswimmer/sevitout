package main

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/grpc"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	sevitoutslack "github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

// postedMessage records one fakeSlack.PostMessage call.
type postedMessage struct {
	channel string
	text    string
}

// fakeSlack is a slackClient that records calls instead of talking to Slack.
type fakeSlack struct {
	mu sync.Mutex

	createChannelName string
	createChannelID   string
	createChannelErr  error

	invitedChannel string
	invitedUsers   []string
	inviteErr      error

	posted  []postedMessage
	postErr error

	history    []sevitoutslack.Message
	historyErr error

	emailToUserID map[string]string
	lookupErr     error
}

func (f *fakeSlack) CreateChannel(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createChannelName = name
	if f.createChannelErr != nil {
		return "", f.createChannelErr
	}
	id := f.createChannelID
	if id == "" {
		id = "C-NEW"
	}
	return id, nil
}

func (f *fakeSlack) InviteUsers(_ context.Context, channelID string, userIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invitedChannel = channelID
	f.invitedUsers = append(f.invitedUsers, userIDs...)
	return f.inviteErr
}

func (f *fakeSlack) PostMessage(_ context.Context, channelID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, postedMessage{channel: channelID, text: text})
	return f.postErr
}

func (f *fakeSlack) FetchHistory(_ context.Context, _ string, _ int) ([]sevitoutslack.Message, error) {
	return f.history, f.historyErr
}

func (f *fakeSlack) LookupUserIDByEmail(_ context.Context, email string) (string, error) {
	if f.lookupErr != nil {
		return "", f.lookupErr
	}
	return f.emailToUserID[email], nil
}

// fakeSevAPI is a sevAPI that returns scripted responses.
type fakeSevAPI struct {
	createResp *pb.SEVResponse
	createErr  error
	getResp    *pb.SEVResponse
	getErr     error
	transResp  *pb.SEVResponse
	transErr   error

	lastCreateReq *pb.CreateSEVRequest
	lastGetReq    *pb.GetSEVRequest
	lastTransReq  *pb.TransitionStatusRequest
}

func (f *fakeSevAPI) CreateSEV(_ context.Context, in *pb.CreateSEVRequest, _ ...grpc.CallOption) (*pb.SEVResponse, error) {
	f.lastCreateReq = in
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResp, nil
}

func (f *fakeSevAPI) GetSEV(_ context.Context, in *pb.GetSEVRequest, _ ...grpc.CallOption) (*pb.SEVResponse, error) {
	f.lastGetReq = in
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResp, nil
}

func (f *fakeSevAPI) TransitionStatus(_ context.Context, in *pb.TransitionStatusRequest, _ ...grpc.CallOption) (*pb.SEVResponse, error) {
	f.lastTransReq = in
	if f.transErr != nil {
		return nil, f.transErr
	}
	return f.transResp, nil
}

// fakeRoleAPI is a roleAPI that returns a scripted role list.
type fakeRoleAPI struct {
	resp    *pb.ListRolesResponse
	err     error
	lastReq *pb.ListRolesRequest
}

func (f *fakeRoleAPI) ListRoles(_ context.Context, in *pb.ListRolesRequest, _ ...grpc.CallOption) (*pb.ListRolesResponse, error) {
	f.lastReq = in
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// fakeAnnouncementAPI is an announcementAPI that returns scripted responses.
type fakeAnnouncementAPI struct {
	createResp *pb.AnnouncementResponse
	createErr  error
	listResp   *pb.ListAnnouncementsResponse
	listErr    error

	lastCreateReq *pb.CreateAnnouncementRequest
}

func (f *fakeAnnouncementAPI) CreateAnnouncement(_ context.Context, in *pb.CreateAnnouncementRequest, _ ...grpc.CallOption) (*pb.AnnouncementResponse, error) {
	f.lastCreateReq = in
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResp, nil
}

func (f *fakeAnnouncementAPI) ListAnnouncements(_ context.Context, _ *pb.ListAnnouncementsRequest, _ ...grpc.CallOption) (*pb.ListAnnouncementsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResp, nil
}

// fakeChatAPI is a chatAPI that records every entry added.
type fakeChatAPI struct {
	mu      sync.Mutex
	entries []*pb.AddChatEntryRequest
	err     error
}

func (f *fakeChatAPI) AddChatEntry(_ context.Context, in *pb.AddChatEntryRequest, _ ...grpc.CallOption) (*pb.ChatEntryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.entries = append(f.entries, in)
	return &pb.ChatEntryResponse{}, nil
}

// fakeConfigAPI is a configAPI that returns a scripted response or error.
type fakeConfigAPI struct {
	resp *pb.IntegrationConfigResponse
	err  error
}

func (f *fakeConfigAPI) GetIntegrationConfig(_ context.Context, _ *pb.GetIntegrationConfigRequest, _ ...grpc.CallOption) (*pb.IntegrationConfigResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// errAlways is a stand-in error for tests that don't care about the message.
var errAlways = errors.New("boom")

// newTestBot wires a bot from the given fakes, defaulting any nil fake to an
// empty one so tests only need to specify what they use.
func newTestBot(slackC *fakeSlack, sevs *fakeSevAPI, roles *fakeRoleAPI, ann *fakeAnnouncementAPI, chats *fakeChatAPI, defaultChannel, naming string) *bot {
	if slackC == nil {
		slackC = &fakeSlack{}
	}
	if sevs == nil {
		sevs = &fakeSevAPI{}
	}
	if roles == nil {
		roles = &fakeRoleAPI{}
	}
	if ann == nil {
		ann = &fakeAnnouncementAPI{}
	}
	if chats == nil {
		chats = &fakeChatAPI{}
	}
	return newBot(slackC, apiClients{sevs: sevs, roles: roles, announcements: ann, chats: chats}, nil, defaultChannel, naming)
}
