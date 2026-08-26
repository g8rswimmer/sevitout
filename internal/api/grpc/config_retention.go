package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── Data retention ────────────────────────────────────────────────────────────

func (s *ConfigServer) GetRetentionConfig(ctx context.Context, req *pb.GetRetentionConfigRequest) (*pb.RetentionConfigResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	cfg, err := s.retention.Get(ctx, int16(req.GetSeverityLevel()))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "retention config not found for this severity level")
		}
		return nil, status.Error(codes.Internal, "failed to get retention config")
	}
	return retentionConfigToProto(cfg), nil
}

func (s *ConfigServer) UpdateRetentionConfig(ctx context.Context, req *pb.UpdateRetentionConfigRequest) (*pb.RetentionConfigResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	if req.GetRetentionDays() < 0 {
		return nil, status.Error(codes.InvalidArgument, "retention_days must be >= 0 (0 means retain forever)")
	}

	now := time.Now()
	cfg := &store.RetentionConfig{
		SeverityLevel: int16(req.GetSeverityLevel()),
		RetentionDays: int(req.GetRetentionDays()),
		HardDelete:    req.GetHardDelete(),
		// CreatedAt is only used when no row exists yet for this severity
		// level; the store overwrites it from the existing row otherwise.
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.retention.Upsert(ctx, cfg); err != nil {
		return nil, status.Error(codes.Internal, "failed to update retention config")
	}

	slog.InfoContext(ctx, "retention config updated",
		"actor", callerID(ctx), "severity_level", cfg.SeverityLevel,
		"retention_days", cfg.RetentionDays, "hard_delete", cfg.HardDelete)

	return retentionConfigToProto(cfg), nil
}

func (s *ConfigServer) ListRetentionConfig(ctx context.Context, _ *pb.ListRetentionConfigRequest) (*pb.ListRetentionConfigResponse, error) {
	cfgs, err := s.retention.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list retention config")
	}
	resp := &pb.ListRetentionConfigResponse{}
	for _, cfg := range cfgs {
		resp.Configs = append(resp.Configs, retentionConfigToProto(cfg))
	}
	return resp, nil
}

func validateSeverityLevel(level int32) error {
	if level < 1 || level > 4 {
		return status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
	}
	return nil
}

func retentionConfigToProto(cfg *store.RetentionConfig) *pb.RetentionConfigResponse {
	return &pb.RetentionConfigResponse{
		SeverityLevel: int32(cfg.SeverityLevel),
		RetentionDays: int32(cfg.RetentionDays),
		HardDelete:    cfg.HardDelete,
		CreatedAt:     timestamppb.New(cfg.CreatedAt),
		UpdatedAt:     timestamppb.New(cfg.UpdatedAt),
	}
}
