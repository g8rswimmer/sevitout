package grpc

import (
	"context"

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
		return nil, status.Error(codes.Internal, "failed to get user")
	}
	resp := &pb.WhoAmIResponse{
		Id:      user.ID,
		Email:   user.Email,
		Name:    user.Name,
		OrgRole: string(user.OrgRole),
	}
	if user.AvatarURL != nil {
		resp.AvatarUrl = *user.AvatarURL
	}
	return resp, nil
}
