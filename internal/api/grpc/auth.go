package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// AuthServer implements pb.AuthServiceServer.
type AuthServer struct {
	pb.UnimplementedAuthServiceServer
	users store.UserStore
}

// NewAuthServer returns an AuthServer backed by users.
func NewAuthServer(users store.UserStore) *AuthServer {
	return &AuthServer{users: users}
}

// WhoAmI returns the identity of the currently authenticated caller.
func (s *AuthServer) WhoAmI(ctx context.Context, _ *pb.WhoAmIRequest) (*pb.WhoAmIResponse, error) {
	uc, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	user, err := s.users.Get(ctx, uc.UserID)
	if err != nil {
		return nil, internalError(ctx, "failed to get user", err)
	}
	return whoAmIToProto(user), nil
}

// UpdateMyIntegrationIdentities lets the caller set their own self-service
// integration identities (Slack user ID, GitHub username, Jira account ID —
// docs/roadmap.md Phase 10a). Full-replace: all three fields are applied on
// every call, and an empty string clears that field.
func (s *AuthServer) UpdateMyIntegrationIdentities(ctx context.Context, req *pb.UpdateMyIntegrationIdentitiesRequest) (*pb.WhoAmIResponse, error) {
	uc, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}

	user, err := s.users.UpdateIntegrationIdentities(ctx, uc.UserID,
		strPtrOrNil(req.GetSlackUserId()),
		strPtrOrNil(req.GetGithubUsername()),
		strPtrOrNil(req.GetJiraAccountId()),
	)
	if err != nil {
		return nil, internalError(ctx, "failed to update integration identities", err)
	}
	return whoAmIToProto(user), nil
}

// ListUserDirectory returns a minimal, Viewer-safe user directory — id,
// name, email, and stored Slack user ID only — for the role-assignment user
// picker (§10c) and Slack auto-invite resolution (§10d), both of which need
// "who is this person" without the Admin-only fields ConfigService.ListUsers
// exposes (org_role, active status, timestamps).
func (s *AuthServer) ListUserDirectory(ctx context.Context, req *pb.ListUserDirectoryRequest) (*pb.ListUserDirectoryResponse, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list users", err)
	}

	var ids map[string]bool
	if len(req.GetIds()) > 0 {
		ids = make(map[string]bool, len(req.GetIds()))
		for _, id := range req.GetIds() {
			ids[id] = true
		}
	}
	q := strings.ToLower(req.GetQuery())

	resp := &pb.ListUserDirectoryResponse{}
	for _, u := range users {
		if ids != nil && !ids[u.ID] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(u.Name), q) && !strings.Contains(strings.ToLower(u.Email), q) {
			continue
		}
		du := &pb.DirectoryUser{Id: u.ID, Name: u.Name, Email: u.Email}
		if u.SlackUserID != nil {
			du.SlackUserId = *u.SlackUserID
		}
		if u.GitHubUsername != nil {
			du.GithubUsername = *u.GitHubUsername
		}
		if u.JiraAccountID != nil {
			du.JiraAccountId = *u.JiraAccountID
		}
		resp.Users = append(resp.Users, du)
	}
	return resp, nil
}

func whoAmIToProto(user *store.User) *pb.WhoAmIResponse {
	resp := &pb.WhoAmIResponse{
		Id:      user.ID,
		Email:   user.Email,
		Name:    user.Name,
		OrgRole: string(user.OrgRole),
	}
	if user.AvatarURL != nil {
		resp.AvatarUrl = *user.AvatarURL
	}
	if user.SlackUserID != nil {
		resp.SlackUserId = *user.SlackUserID
	}
	if user.GitHubUsername != nil {
		resp.GithubUsername = *user.GitHubUsername
	}
	if user.JiraAccountID != nil {
		resp.JiraAccountId = *user.JiraAccountID
	}
	return resp
}

// strPtrOrNil returns nil for an empty string, else a pointer to v — the
// full-replace/clear semantics UpdateMyIntegrationIdentities' proto doc
// comment describes: an empty field clears the stored value.
func strPtrOrNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
