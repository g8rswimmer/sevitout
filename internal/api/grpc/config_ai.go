package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// CreateAIPlugin registers a new AI plugin (§18.6). Like
// UpsertIntegrationConfig, a supplied api_key is encrypted with AES-256-GCM
// before being handed to the store — it is never returned by any RPC.
func (s *ConfigServer) CreateAIPlugin(ctx context.Context, req *pb.CreateAIPluginRequest) (*pb.AIPluginResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	handlerType, err := parseAIHandlerType(req.GetHandlerType())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	now := time.Now()
	plugin := &store.AIPlugin{
		Name:                      req.GetName(),
		Version:                   req.GetVersion(),
		HandlerType:               handlerType,
		Enabled:                   req.GetEnabled(),
		TriggerOnOpen:             req.GetTriggerOnOpen(),
		TriggerOnMitigated:        req.GetTriggerOnMitigated(),
		TriggerOnResolved:         req.GetTriggerOnResolved(),
		TriggerOnPostmortemReview: req.GetTriggerOnPostmortemReview(),
		RateLimitPerMinute:        req.GetRateLimitPerMinute(),
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if v := req.GetDescription(); v != "" {
		plugin.Description = &v
	}
	if v := req.GetHttpEndpoint(); v != "" {
		plugin.HTTPEndpoint = &v
	}
	if v := req.GetProvider(); v != "" {
		plugin.Provider = &v
	}
	if v := req.GetModel(); v != "" {
		plugin.Model = &v
	}
	if req.GetApiKey() != "" {
		sealed, err := s.encryptAPIKey(req.GetApiKey())
		if err != nil {
			return nil, err
		}
		plugin.EncryptedAPIKey = sealed
	}

	if err := s.aiPlugins.Create(ctx, plugin); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Error(codes.AlreadyExists, "an AI plugin with this name already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create AI plugin")
	}
	return aiPluginToProto(plugin), nil
}

func (s *ConfigServer) GetAIPlugin(ctx context.Context, req *pb.GetAIPluginRequest) (*pb.AIPluginResponse, error) {
	plugin, err := s.aiPlugins.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "AI plugin not found")
		}
		return nil, status.Error(codes.Internal, "failed to get AI plugin")
	}
	return aiPluginToProto(plugin), nil
}

func (s *ConfigServer) UpdateAIPlugin(ctx context.Context, req *pb.UpdateAIPluginRequest) (*pb.AIPluginResponse, error) {
	plugin, err := s.aiPlugins.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "AI plugin not found")
		}
		return nil, status.Error(codes.Internal, "failed to get AI plugin")
	}

	if v := req.GetName(); v != "" {
		plugin.Name = v
	}
	if v := req.GetVersion(); v != "" {
		plugin.Version = v
	}
	if v := req.GetDescription(); v != "" {
		plugin.Description = &v
	}
	if v := req.GetHandlerType(); v != "" {
		handlerType, err := parseAIHandlerType(v)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		plugin.HandlerType = handlerType
	}
	if v := req.GetHttpEndpoint(); v != "" {
		plugin.HTTPEndpoint = &v
	}
	if v := req.GetProvider(); v != "" {
		plugin.Provider = &v
	}
	if v := req.GetModel(); v != "" {
		plugin.Model = &v
	}
	if req.GetApiKey() != "" {
		sealed, err := s.encryptAPIKey(req.GetApiKey())
		if err != nil {
			return nil, err
		}
		plugin.EncryptedAPIKey = sealed
	}
	if req.GetEnabled() != nil {
		plugin.Enabled = req.GetEnabled().GetValue()
	}
	if req.GetTriggerOnOpen() != nil {
		plugin.TriggerOnOpen = req.GetTriggerOnOpen().GetValue()
	}
	if req.GetTriggerOnMitigated() != nil {
		plugin.TriggerOnMitigated = req.GetTriggerOnMitigated().GetValue()
	}
	if req.GetTriggerOnResolved() != nil {
		plugin.TriggerOnResolved = req.GetTriggerOnResolved().GetValue()
	}
	if req.GetTriggerOnPostmortemReview() != nil {
		plugin.TriggerOnPostmortemReview = req.GetTriggerOnPostmortemReview().GetValue()
	}
	if req.GetRateLimitPerMinute() != nil {
		plugin.RateLimitPerMinute = req.GetRateLimitPerMinute().GetValue()
	}
	plugin.UpdatedAt = time.Now()

	if err := s.aiPlugins.Update(ctx, plugin); err != nil {
		return nil, status.Error(codes.Internal, "failed to update AI plugin")
	}
	return aiPluginToProto(plugin), nil
}

func (s *ConfigServer) DeleteAIPlugin(ctx context.Context, req *pb.DeleteAIPluginRequest) (*emptypb.Empty, error) {
	if err := s.aiPlugins.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "AI plugin not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete AI plugin")
	}
	if s.rateLimits != nil {
		s.rateLimits.EvictRateLimit(req.GetId())
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListAIPlugins(ctx context.Context, _ *pb.ListAIPluginsRequest) (*pb.ListAIPluginsResponse, error) {
	plugins, err := s.aiPlugins.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list AI plugins")
	}
	resp := &pb.ListAIPluginsResponse{}
	for _, p := range plugins {
		resp.Plugins = append(resp.Plugins, aiPluginToProto(p))
	}
	return resp, nil
}

func (s *ConfigServer) encryptAPIKey(apiKey string) ([]byte, error) {
	if s.crypto == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"credential encryption is not configured (ENCRYPTION_KEY not set)")
	}
	sealed, err := s.crypto.Encrypt([]byte(apiKey))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to encrypt api_key")
	}
	return sealed, nil
}

func parseAIHandlerType(v string) (store.AIHandlerType, error) {
	switch store.AIHandlerType(v) {
	case store.AIHandlerBuiltin, store.AIHandlerHTTP:
		return store.AIHandlerType(v), nil
	default:
		return "", errInvalidHandlerType
	}
}

var errInvalidHandlerType = errors.New(`handler_type must be "builtin" or "http"`)

// aiPluginToProto never includes the decrypted API key — only whether one is
// currently configured, same policy as integrationConfigToProto.
func aiPluginToProto(p *store.AIPlugin) *pb.AIPluginResponse {
	resp := &pb.AIPluginResponse{
		Id:                        p.ID,
		Name:                      p.Name,
		Version:                   p.Version,
		HandlerType:               string(p.HandlerType),
		ApiKeyConfigured:          len(p.EncryptedAPIKey) > 0,
		Enabled:                   p.Enabled,
		TriggerOnOpen:             p.TriggerOnOpen,
		TriggerOnMitigated:        p.TriggerOnMitigated,
		TriggerOnResolved:         p.TriggerOnResolved,
		TriggerOnPostmortemReview: p.TriggerOnPostmortemReview,
		RateLimitPerMinute:        p.RateLimitPerMinute,
		CreatedAt:                 timestamppb.New(p.CreatedAt),
		UpdatedAt:                 timestamppb.New(p.UpdatedAt),
	}
	if p.Description != nil {
		resp.Description = *p.Description
	}
	if p.HTTPEndpoint != nil {
		resp.HttpEndpoint = *p.HTTPEndpoint
	}
	if p.Provider != nil {
		resp.Provider = *p.Provider
	}
	if p.Model != nil {
		resp.Model = *p.Model
	}
	return resp
}
