package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/ai"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// AIRunner executes AI actions on demand — synchronously (TriggerAction) and
// streamed (StreamAction) — for AIServer (§11.2). Declared here (the
// consumer) per this repo's interface-ownership convention — ai.Dispatcher
// satisfies it implicitly.
type AIRunner interface {
	Run(ctx context.Context, sevID string, action ai.Action, pluginID int64) (*store.AIOutput, error)
	StreamOne(ctx context.Context, sevID string, action ai.Action, pluginID int64) (<-chan ai.Chunk, error)
}

// AIServer implements pb.AIServiceServer: the functional, non-admin AI
// surface (§11.2). Admin CRUD for plugin registration is on ConfigServer.
type AIServer struct {
	pb.UnimplementedAIServiceServer
	runner  AIRunner // nil when no AI plugin is configured
	outputs store.AIOutputStore
	plugins store.AIPluginStore
}

// NewAIServer returns an AIServer. runner may be nil — TriggerAction and
// StreamAction then fail with FailedPrecondition rather than panicking.
func NewAIServer(runner AIRunner, outputs store.AIOutputStore, plugins store.AIPluginStore) *AIServer {
	return &AIServer{runner: runner, outputs: outputs, plugins: plugins}
}

func (s *AIServer) TriggerAction(ctx context.Context, req *pb.TriggerActionRequest) (*pb.AIOutputResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	action, err := actionFromProto(req.GetAction())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if s.runner == nil {
		return nil, status.Error(codes.FailedPrecondition, "no AI plugin is configured")
	}

	out, err := s.runner.Run(ctx, req.GetSevId(), action, req.GetPluginId())
	if err != nil {
		return nil, aiErrorToStatus(err)
	}
	return aiOutputToProto(out), nil
}

func (s *AIServer) StreamAction(req *pb.TriggerActionRequest, stream pb.AIService_StreamActionServer) error {
	if req.GetSevId() == "" {
		return status.Error(codes.InvalidArgument, "sev_id is required")
	}
	action, err := actionFromProto(req.GetAction())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if s.runner == nil {
		return status.Error(codes.FailedPrecondition, "no AI plugin is configured")
	}

	chunks, err := s.runner.StreamOne(stream.Context(), req.GetSevId(), action, req.GetPluginId())
	if err != nil {
		return aiErrorToStatus(err)
	}
	for chunk := range chunks {
		if err := stream.Send(&pb.AIActionChunk{Content: chunk.Content, Done: chunk.Done}); err != nil {
			return err
		}
	}
	return nil
}

func (s *AIServer) ListOutputs(ctx context.Context, req *pb.ListAIOutputsRequest) (*pb.ListAIOutputsResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	outs, err := s.outputs.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list AI outputs")
	}
	resp := &pb.ListAIOutputsResponse{}
	for _, o := range outs {
		resp.Outputs = append(resp.Outputs, aiOutputToProto(o))
	}
	return resp, nil
}

func (s *AIServer) ListPlugins(ctx context.Context, _ *pb.ListPluginsRequest) (*pb.ListPluginsResponse, error) {
	plugins, err := s.plugins.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list AI plugins")
	}
	resp := &pb.ListPluginsResponse{}
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		provider := ""
		if p.Provider != nil {
			provider = *p.Provider
		}
		resp.Plugins = append(resp.Plugins, &pb.AvailablePlugin{Id: p.ID, Name: p.Name, Provider: provider})
	}
	return resp, nil
}

// aiErrorToStatus maps internal/ai's sentinel errors to specific gRPC status
// codes; anything else is an opaque Internal error.
func aiErrorToStatus(err error) error {
	switch {
	case errors.Is(err, ai.ErrAIDisabledForSEV):
		return status.Error(codes.FailedPrecondition, "AI is disabled for this SEV")
	case errors.Is(err, ai.ErrSensitiveSEV):
		return status.Error(codes.FailedPrecondition, "AI actions are not available for sensitive SEVs")
	case errors.Is(err, ai.ErrPluginDisabled):
		return status.Error(codes.FailedPrecondition, "the requested plugin is disabled")
	case errors.Is(err, ai.ErrNoEnabledPlugin):
		return status.Error(codes.FailedPrecondition, "no enabled AI plugin is configured")
	case errors.Is(err, ai.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "AI plugin rate limit exceeded, try again shortly")
	case errors.Is(err, ai.ErrEncryptionNotConfigured):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, "SEV or plugin not found")
	default:
		return status.Error(codes.Internal, "AI action failed: "+err.Error())
	}
}

// actionFromProto and protoFromAction convert between pb.AIAction and
// ai.Action so internal/ai never imports generated protobuf code.
func actionFromProto(a pb.AIAction) (ai.Action, error) {
	switch a {
	case pb.AIAction_AI_ACTION_SUMMARIZE:
		return ai.ActionSummarize, nil
	case pb.AIAction_AI_ACTION_SUGGEST_ROOT_CAUSE:
		return ai.ActionSuggestRootCause, nil
	case pb.AIAction_AI_ACTION_DRAFT_POSTMORTEM:
		return ai.ActionDraftPostmortem, nil
	case pb.AIAction_AI_ACTION_SUGGEST_TASKS:
		return ai.ActionSuggestTasks, nil
	case pb.AIAction_AI_ACTION_FIND_SIMILAR:
		return ai.ActionFindSimilar, nil
	case pb.AIAction_AI_ACTION_SUGGEST_RESPONDERS:
		return ai.ActionSuggestResponders, nil
	case pb.AIAction_AI_ACTION_DRAFT_ANNOUNCEMENT:
		return ai.ActionDraftAnnouncement, nil
	default:
		return "", errUnspecifiedAction
	}
}

var errUnspecifiedAction = errors.New("action is required")

func protoFromAction(a string) pb.AIAction {
	switch ai.Action(a) {
	case ai.ActionSummarize:
		return pb.AIAction_AI_ACTION_SUMMARIZE
	case ai.ActionSuggestRootCause:
		return pb.AIAction_AI_ACTION_SUGGEST_ROOT_CAUSE
	case ai.ActionDraftPostmortem:
		return pb.AIAction_AI_ACTION_DRAFT_POSTMORTEM
	case ai.ActionSuggestTasks:
		return pb.AIAction_AI_ACTION_SUGGEST_TASKS
	case ai.ActionFindSimilar:
		return pb.AIAction_AI_ACTION_FIND_SIMILAR
	case ai.ActionSuggestResponders:
		return pb.AIAction_AI_ACTION_SUGGEST_RESPONDERS
	case ai.ActionDraftAnnouncement:
		return pb.AIAction_AI_ACTION_DRAFT_ANNOUNCEMENT
	default:
		return pb.AIAction_AI_ACTION_UNSPECIFIED
	}
}

func aiOutputToProto(o *store.AIOutput) *pb.AIOutputResponse {
	return &pb.AIOutputResponse{
		Id:           o.ID,
		SevId:        o.SEVID,
		PluginId:     o.PluginID,
		TriggerEvent: o.TriggerEvent,
		Action:       protoFromAction(o.Action),
		Content:      o.Content,
		CreatedAt:    timestamppb.New(o.CreatedAt),
	}
}
