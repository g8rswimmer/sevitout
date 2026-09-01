package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── User management ──────────────────────────────────────────────────────────

func (s *ConfigServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list users", err)
	}
	q := strings.ToLower(req.GetQuery())
	resp := &pb.ListUsersResponse{}
	for _, u := range users {
		if q != "" && !strings.Contains(strings.ToLower(u.Name), q) && !strings.Contains(strings.ToLower(u.Email), q) {
			continue
		}
		resp.Users = append(resp.Users, userToProto(u))
	}
	return resp, nil
}

func (s *ConfigServer) UpdateUserRole(ctx context.Context, req *pb.UpdateUserRoleRequest) (*pb.UserResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := validateOrgRole(req.GetOrgRole()); err != nil {
		return nil, err
	}

	u, err := s.users.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, internalError(ctx, "failed to get user", err)
	}

	oldRole := u.OrgRole
	u.OrgRole = store.OrgRole(req.GetOrgRole())
	u.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, u); err != nil {
		return nil, internalError(ctx, "failed to update user", err)
	}

	// Permission changes must be logged (docs/requirements.md §14, §18.2).
	slog.InfoContext(ctx, "user role changed",
		"actor", callerID(ctx), "user_id", u.ID, "old_role", oldRole, "new_role", u.OrgRole)

	return userToProto(u), nil
}

func (s *ConfigServer) DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.UserResponse, error) {
	return s.setUserActive(ctx, req.GetId(), false)
}

func (s *ConfigServer) ReactivateUser(ctx context.Context, req *pb.ReactivateUserRequest) (*pb.UserResponse, error) {
	return s.setUserActive(ctx, req.GetId(), true)
}

func (s *ConfigServer) setUserActive(ctx context.Context, id string, active bool) (*pb.UserResponse, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, internalError(ctx, "failed to get user", err)
	}
	u.Active = active
	u.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, u); err != nil {
		return nil, internalError(ctx, "failed to update user", err)
	}

	action := "deactivated"
	if active {
		action = "reactivated"
	}
	slog.InfoContext(ctx, "user "+action, "actor", callerID(ctx), "user_id", u.ID)

	return userToProto(u), nil
}

func validateOrgRole(role string) error {
	switch store.OrgRole(role) {
	case store.OrgRoleViewer, store.OrgRoleResponder, store.OrgRoleIncidentCommander, store.OrgRoleAdmin:
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			"org_role must be one of: viewer, responder, incident-commander, admin")
	}
}

func userToProto(u *store.User) *pb.UserResponse {
	resp := &pb.UserResponse{
		Id:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		OrgRole:   string(u.OrgRole),
		Active:    u.Active,
		CreatedAt: timestamppb.New(u.CreatedAt),
		UpdatedAt: timestamppb.New(u.UpdatedAt),
	}
	if u.AvatarURL != nil {
		resp.AvatarUrl = *u.AvatarURL
	}
	if u.SlackUserID != nil {
		resp.SlackUserId = *u.SlackUserID
	}
	if u.GitHubUsername != nil {
		resp.GithubUsername = *u.GitHubUsername
	}
	if u.JiraAccountID != nil {
		resp.JiraAccountId = *u.JiraAccountID
	}
	return resp
}
