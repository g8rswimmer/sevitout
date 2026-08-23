package main

import (
	"context"

	"google.golang.org/grpc"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// The interfaces below are the narrow subsets of each generated gRPC client
// this bot actually calls, declared here (the consumer) per this repo's
// interface-ownership convention (see CLAUDE.md). pb.NewSEVServiceClient and
// friends return the full generated interfaces, which satisfy these
// implicitly — tests substitute small hand-written fakes instead of a real
// gRPC connection.

// sevAPI is the subset of pb.SEVServiceClient the bot's slash commands use.
type sevAPI interface {
	CreateSEV(ctx context.Context, in *pb.CreateSEVRequest, opts ...grpc.CallOption) (*pb.SEVResponse, error)
	GetSEV(ctx context.Context, in *pb.GetSEVRequest, opts ...grpc.CallOption) (*pb.SEVResponse, error)
	TransitionStatus(ctx context.Context, in *pb.TransitionStatusRequest, opts ...grpc.CallOption) (*pb.SEVResponse, error)
}

// roleAPI is the subset of pb.RoleServiceClient the bot uses to find who to
// invite into an auto-created incident channel.
type roleAPI interface {
	ListRoles(ctx context.Context, in *pb.ListRolesRequest, opts ...grpc.CallOption) (*pb.ListRolesResponse, error)
}

// announcementAPI is the subset of pb.AnnouncementServiceClient the bot uses
// for `/sev update` and `@sevbot timeline`.
type announcementAPI interface {
	CreateAnnouncement(ctx context.Context, in *pb.CreateAnnouncementRequest, opts ...grpc.CallOption) (*pb.AnnouncementResponse, error)
	ListAnnouncements(ctx context.Context, in *pb.ListAnnouncementsRequest, opts ...grpc.CallOption) (*pb.ListAnnouncementsResponse, error)
}

// chatAPI is the subset of pb.ChatServiceClient `/sev capture` uses to write
// captured Slack messages into a SEV's chat log.
type chatAPI interface {
	AddChatEntry(ctx context.Context, in *pb.AddChatEntryRequest, opts ...grpc.CallOption) (*pb.ChatEntryResponse, error)
}

// configAPI is the subset of pb.ConfigServiceClient the bot uses to load its
// non-secret Slack settings (default notification channel, incident channel
// naming convention) — see docs/requirements.md §18.4. The bot's own
// credentials (SLACK_APP_TOKEN, SLACK_BOT_TOKEN) come directly from its
// process environment, matching docs/architecture.md §10's docker-compose
// layout; ConfigService never returns decrypted credentials anyway.
type configAPI interface {
	GetIntegrationConfig(ctx context.Context, in *pb.GetIntegrationConfigRequest, opts ...grpc.CallOption) (*pb.IntegrationConfigResponse, error)
}

// apiClients groups every backend dependency the bot calls, so bot
// construction takes one argument instead of five.
type apiClients struct {
	sevs          sevAPI
	roles         roleAPI
	announcements announcementAPI
	chats         chatAPI
	config        configAPI
}
