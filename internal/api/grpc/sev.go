package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// OnCaller retrieves the current on-call person for a service.
// Implementations must return ("", nil) when no one is on-call.
type OnCaller interface {
	OnCallLookup(ctx context.Context, serviceID string) (string, error)
}

type SEVServer struct {
	pb.UnimplementedSEVServiceServer
	sevs        store.SEVStore
	audit       store.AuditStore
	history     store.StatusHistoryStore
	roles       store.RoleStore
	services    store.ServiceStore
	postmortems store.PostmortemStore
	onCaller    OnCaller // nil when PagerDuty is not configured
	unlock      Unlocker
	publisher   Publisher // nil when WebSocket support is not wired up
}

func NewSEVServer(
	sevs store.SEVStore,
	audit store.AuditStore,
	history store.StatusHistoryStore,
	roles store.RoleStore,
	services store.ServiceStore,
	postmortems store.PostmortemStore,
	onCaller OnCaller,
	unlock Unlocker,
	publisher Publisher,
) *SEVServer {
	return &SEVServer{
		sevs:        sevs,
		audit:       audit,
		history:     history,
		roles:       roles,
		services:    services,
		postmortems: postmortems,
		onCaller:    onCaller,
		unlock:      unlock,
		publisher:   publisher,
	}
}

func (s *SEVServer) CreateSEV(ctx context.Context, req *pb.CreateSEVRequest) (*pb.SEVResponse, error) {
	if req.GetTitle() == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if req.GetSeverityLevel() < 1 || req.GetSeverityLevel() > 4 {
		return nil, status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	record := &store.SEV{
		Title:            req.GetTitle(),
		Description:      req.GetDescription(),
		SeverityLevel:    int16(req.GetSeverityLevel()),
		Status:           store.SEVStatusOpen,
		AffectedServices: req.GetAffectedServices(),
		Tags:             req.GetTags(),
		Sensitive:        req.GetSensitive(),
		CreatedBy:        callerID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if v := req.GetDetectionMethod(); v != "" {
		record.DetectionMethod = &v
	}
	if v := req.GetAlertName(); v != "" {
		record.AlertName = &v
	}
	if v := req.GetMonitoringTool(); v != "" {
		record.MonitoringTool = &v
	}
	if req.GetStartedAt() != nil {
		t := req.GetStartedAt().AsTime()
		record.StartedAt = &t
	} else {
		// Default to creation time when the caller doesn't provide started_at;
		// the requirements say this timestamp "may be estimated".
		record.StartedAt = &now
	}
	if req.GetDetectedAt() != nil {
		t := req.GetDetectedAt().AsTime()
		record.DetectedAt = &t
	}

	sev.ComputeMetrics(record)

	if err := s.sevs.Create(ctx, record); err != nil {
		return nil, status.Error(codes.Internal, "failed to create SEV")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    callerID,
		Action:    "sev.created",
		CreatedAt: now,
	})

	if err := s.postmortems.Create(ctx, &store.Postmortem{
		SEVID:     record.ID,
		Status:    store.PostmortemStatusDraft,
		Content:   "",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, status.Error(codes.Internal, "failed to create postmortem for SEV")
	}

	// Best-effort on-call auto-population. Never blocks or fails the response.
	if s.onCaller != nil && len(req.GetAffectedServices()) > 0 {
		if svc, err := s.services.Get(ctx, req.GetAffectedServices()[0]); err == nil && svc.PagerDutyServiceID != nil {
			if displayName, err := s.onCaller.OnCallLookup(ctx, *svc.PagerDutyServiceID); err == nil && displayName != "" {
				if err := s.roles.Assign(ctx, &store.SEVRole{
					SEVID:       record.ID,
					RoleType:    store.SEVRoleOnCall,
					DisplayName: displayName,
					CreatedAt:   now,
					CreatedBy:   callerID,
				}); err != nil {
					slog.ErrorContext(ctx, "on-call role auto-assign failed", "sev_id", record.ID, "err", err)
				}
			}
		}
	}

	resp := sevToProto(record)
	if !record.Sensitive {
		// Published after on-call auto-population above so a subscriber
		// reacting to sev.created (the Slack bot's auto incident-channel
		// creation, M11) can immediately look up the on-call role via
		// RoleService without racing this handler's own writes.
		publishProto(s.publisher, record.ID, "sev.created", resp)
	}

	return resp, nil
}

func (s *SEVServer) GetSEV(ctx context.Context, req *pb.GetSEVRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}
	return sevToProto(record), nil
}

func (s *SEVServer) UpdateSEV(ctx context.Context, req *pb.UpdateSEVRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	if record.Locked {
		if err := validateUnlock(s.unlock, req.GetUnlockToken(), req.GetId()); err != nil {
			return nil, err
		}
	}

	if v := req.GetTitle(); v != "" {
		record.Title = v
	}
	if v := req.GetDescription(); v != "" {
		record.Description = v
	}
	if v := req.GetSeverityLevel(); v != 0 {
		if v < 1 || v > 4 {
			return nil, status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
		}
		record.SeverityLevel = int16(v)
	}
	if v := req.GetRootCauseCategory(); v != "" {
		record.RootCauseCategory = &v
	}
	if v := req.GetRootCauseDescription(); v != "" {
		record.RootCauseDescription = &v
	}
	if v := req.GetMitigation(); v != "" {
		record.Mitigation = &v
	}
	if v := req.GetPrevention(); v != "" {
		record.Prevention = &v
	}
	if v := req.GetBusinessImpact(); v != "" {
		record.BusinessImpact = &v
	}
	if v := req.GetAffectedServices(); len(v) > 0 {
		record.AffectedServices = v
	}
	if v := req.GetDetectionMethod(); v != "" {
		record.DetectionMethod = &v
	}
	if v := req.GetAlertName(); v != "" {
		record.AlertName = &v
	}
	if v := req.GetMonitoringTool(); v != "" {
		record.MonitoringTool = &v
	}
	if req.GetRightPeoplePresent() != nil {
		b := req.GetRightPeoplePresent().GetValue()
		record.RightPeoplePresent = &b
	}
	if v := req.GetRightPeopleNotes(); v != "" {
		record.RightPeopleNotes = &v
	}
	if v := req.GetTags(); len(v) > 0 {
		record.Tags = v
	}
	if req.GetStartedAt() != nil {
		t := req.GetStartedAt().AsTime()
		record.StartedAt = &t
	}
	if req.GetDetectedAt() != nil {
		t := req.GetDetectedAt().AsTime()
		record.DetectedAt = &t
	}
	if req.GetSensitive() != nil {
		record.Sensitive = req.GetSensitive().GetValue()
	}

	record.UpdatedAt = time.Now()
	sev.ComputeMetrics(record)

	if err := s.sevs.Update(ctx, record); err != nil {
		return nil, status.Error(codes.Internal, "failed to update SEV")
	}

	updaterID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		updaterID = uc.UserID
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    updaterID,
		Action:    "sev.updated",
		CreatedAt: record.UpdatedAt,
	})

	resp := sevToProto(record)
	if !record.Sensitive {
		publishProto(s.publisher, record.ID, "sev.updated", resp)
	}

	return resp, nil
}

func (s *SEVServer) ListSEVs(ctx context.Context, req *pb.ListSEVsRequest) (*pb.ListSEVsResponse, error) {
	filter := store.SEVFilter{
		OnCallUser: req.GetOnCallUser(),
		Search:     req.GetSearch(),
		Limit:      int(req.GetLimit()),
		Offset:     int(req.GetOffset()),
	}
	for _, l := range req.GetSeverityLevels() {
		filter.SeverityLevels = append(filter.SeverityLevels, int16(l))
	}
	for _, st := range req.GetStatuses() {
		filter.Statuses = append(filter.Statuses, store.SEVStatus(st))
	}

	total, err := s.sevs.Count(ctx, filter)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to count SEVs")
	}

	records, err := s.sevs.List(ctx, filter)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list SEVs")
	}

	resp := &pb.ListSEVsResponse{
		Total: int32(total),
	}
	for _, r := range records {
		resp.Sevs = append(resp.Sevs, sevToProto(r))
	}
	return resp, nil
}

func (s *SEVServer) TransitionStatus(ctx context.Context, req *pb.TransitionStatusRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.GetToStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "to_status is required")
	}

	toStatus := store.SEVStatus(req.GetToStatus())
	switch toStatus {
	case store.SEVStatusOpen,
		store.SEVStatusInvestigating,
		store.SEVStatusMitigated,
		store.SEVStatusResolved,
		store.SEVStatusPostmortemInProgress,
		store.SEVStatusPostmortemComplete:
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown to_status value")
	}

	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	// A locked SEV requires a valid unlock token for any transition.
	if record.Locked {
		if err := validateUnlock(s.unlock, req.GetUnlockToken(), req.GetId()); err != nil {
			return nil, err
		}
	}

	if err := sev.ValidateTransition(record.Status, toStatus); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	fromStatus := record.Status
	record.Status = toStatus

	if req.GetMitigatedAt() != nil {
		t := req.GetMitigatedAt().AsTime()
		record.MitigatedAt = &t
	}
	if req.GetResolvedAt() != nil {
		t := req.GetResolvedAt().AsTime()
		record.ResolvedAt = &t
	}
	if req.GetPostmortemCompletedAt() != nil {
		t := req.GetPostmortemCompletedAt().AsTime()
		record.PostmortemCompletedAt = &t
	}

	now := time.Now()

	// Lock the SEV on entering PostmortemComplete; clear the lock when leaving
	// (e.g. re-open after postmortem review). The unlock token was already
	// validated above when record.Locked was true.
	if toStatus == store.SEVStatusPostmortemComplete {
		if record.PostmortemCompletedAt == nil {
			record.PostmortemCompletedAt = &now
		}
		record.Locked = true
	} else {
		record.Locked = false
	}

	sev.ComputeMetrics(record)
	record.UpdatedAt = now

	transitionerID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		transitionerID = uc.UserID
	}

	// Write history before updating the SEV so that a history failure leaves
	// the SEV in its previous state and the caller can safely retry.
	if err := s.history.Create(ctx, &store.SEVStatusHistory{
		SEVID:          record.ID,
		FromStatus:     &fromStatus,
		ToStatus:       toStatus,
		UserID:         transitionerID,
		TransitionedAt: now,
	}); err != nil {
		return nil, status.Error(codes.Internal, "failed to record status history")
	}

	if err := s.sevs.Update(ctx, record); err != nil {
		return nil, status.Error(codes.Internal, "failed to update SEV")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    transitionerID,
		Action:    "sev.status_transitioned",
		CreatedAt: now,
	})

	resp := sevToProto(record)
	if !record.Sensitive {
		publishProto(s.publisher, record.ID, "sev.status_changed", resp)
	}

	return resp, nil
}

// validateUnlock returns a gRPC status error when the token is missing or invalid for sevID.
func validateUnlock(u Unlocker, token, sevID string) error {
	if u == nil {
		return status.Error(codes.Internal, "lock enforcement not configured")
	}
	if token == "" {
		return status.Error(codes.PermissionDenied, "SEV is locked; provide an unlock_token")
	}
	if err := u.Validate(token, sevID); err != nil {
		if errors.Is(err, postmortem.ErrUnlockTokenExpired) {
			return status.Error(codes.PermissionDenied, "unlock token has expired")
		}
		if errors.Is(err, postmortem.ErrUnlockTokenSEVMismatch) {
			return status.Error(codes.PermissionDenied, "unlock token is not valid for this SEV")
		}
		return status.Error(codes.PermissionDenied, "invalid unlock token")
	}
	return nil
}

func sevToProto(s *store.SEV) *pb.SEVResponse {
	resp := &pb.SEVResponse{
		Id:               s.ID,
		Title:            s.Title,
		Description:      s.Description,
		SeverityLevel:    int32(s.SeverityLevel),
		Status:           string(s.Status),
		AffectedServices: s.AffectedServices,
		Tags:             s.Tags,
		Locked:           s.Locked,
		Sensitive:        s.Sensitive,
		CreatedBy:        s.CreatedBy,
		CreatedAt:        timestamppb.New(s.CreatedAt),
		UpdatedAt:        timestamppb.New(s.UpdatedAt),
	}

	if s.RootCauseCategory != nil {
		resp.RootCauseCategory = *s.RootCauseCategory
	}
	if s.RootCauseDescription != nil {
		resp.RootCauseDescription = *s.RootCauseDescription
	}
	if s.Mitigation != nil {
		resp.Mitigation = *s.Mitigation
	}
	if s.Prevention != nil {
		resp.Prevention = *s.Prevention
	}
	if s.BusinessImpact != nil {
		resp.BusinessImpact = *s.BusinessImpact
	}
	if s.DetectionMethod != nil {
		resp.DetectionMethod = *s.DetectionMethod
	}
	if s.AlertName != nil {
		resp.AlertName = *s.AlertName
	}
	if s.MonitoringTool != nil {
		resp.MonitoringTool = *s.MonitoringTool
	}
	if s.RightPeoplePresent != nil {
		resp.RightPeoplePresent = wrapperspb.Bool(*s.RightPeoplePresent)
	}
	if s.RightPeopleNotes != nil {
		resp.RightPeopleNotes = *s.RightPeopleNotes
	}
	if s.StartedAt != nil {
		resp.StartedAt = timestamppb.New(*s.StartedAt)
	}
	if s.DetectedAt != nil {
		resp.DetectedAt = timestamppb.New(*s.DetectedAt)
	}
	if s.MitigatedAt != nil {
		resp.MitigatedAt = timestamppb.New(*s.MitigatedAt)
	}
	if s.ResolvedAt != nil {
		resp.ResolvedAt = timestamppb.New(*s.ResolvedAt)
	}
	if s.PostmortemCompletedAt != nil {
		resp.PostmortemCompletedAt = timestamppb.New(*s.PostmortemCompletedAt)
	}
	if s.MTTDSeconds != nil {
		resp.MttdSeconds = *s.MTTDSeconds
	}
	if s.MTTMSeconds != nil {
		resp.MttmSeconds = *s.MTTMSeconds
	}
	if s.MTTRSeconds != nil {
		resp.MttrSeconds = *s.MTTRSeconds
	}
	if s.DTTMSeconds != nil {
		resp.DttmSeconds = *s.DTTMSeconds
	}

	return resp
}
