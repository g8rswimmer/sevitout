package grpc

import (
	"context"
	"errors"
	"regexp"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// SlackInviteClient is the narrow subset of internal/integrations/slack.Client
// InviteRoleToSlack needs, declared here (the consumer) per this repo's
// interface-ownership convention — *slack.Client satisfies it implicitly.
type SlackInviteClient interface {
	InviteUsers(ctx context.Context, channelID string, userIDs []string) error
	LookupUserIDByEmail(ctx context.Context, email string) (string, error)
}

// SlackClientFactory builds a SlackInviteClient from a decrypted bot token.
// Injected (rather than this package importing internal/integrations/slack
// directly) so tests can substitute a fake without a real Slack API call —
// cmd/server wires the real factory (slack.NewClient).
type SlackClientFactory func(botToken string) SlackInviteClient

// roleEmailInAngleBrackets pulls the email out of a free-text display name
// of the form "Alice <alice@example.com>" — the same fallback shape
// cmd/slackbot/channel.go's emailInAngleBrackets matches, duplicated here
// per docs/roadmap.md Phase 10e's accepted "~20-line resolver duplicated
// between two binaries" trade-off rather than a shared package for two call
// sites.
var roleEmailInAngleBrackets = regexp.MustCompile(`<([^>@\s]+@[^>]+)>`)

// RoleServer implements pb.RoleServiceServer.
type RoleServer struct {
	pb.UnimplementedRoleServiceServer
	roles        store.RoleStore
	sevs         store.SEVStore
	access       store.SEVAccessStore
	audit        store.AuditStore
	publisher    Publisher // nil when WebSocket support is not wired up
	users        store.UserStore
	integrations store.IntegrationConfigStore
	crypto       Encryptor
	slackFactory SlackClientFactory // nil when Slack invite support is not wired up
}

// RoleServerParams groups NewRoleServer's dependencies. Publisher may be nil
// (WebSocket support is optional at deploy time). Users/Integrations/Crypto/
// SlackFactory may also be nil/zero, in which case InviteRoleToSlack always
// returns Unavailable — see that method's doc comment.
type RoleServerParams struct {
	Roles        store.RoleStore
	SEVs         store.SEVStore
	Access       store.SEVAccessStore
	Audit        store.AuditStore
	Publisher    Publisher
	Users        store.UserStore
	Integrations store.IntegrationConfigStore
	Crypto       Encryptor
	SlackFactory SlackClientFactory
}

// NewRoleServer returns a RoleServer backed by p.
func NewRoleServer(p RoleServerParams) *RoleServer {
	return &RoleServer{
		roles: p.Roles, sevs: p.SEVs, access: p.Access, audit: p.Audit, publisher: p.Publisher,
		users: p.Users, integrations: p.Integrations, crypto: p.Crypto, slackFactory: p.SlackFactory,
	}
}

func (s *RoleServer) AssignRole(ctx context.Context, req *pb.AssignRoleRequest) (*pb.SEVRoleResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetRoleType() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_type is required")
	}
	if req.GetDisplayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "display_name is required")
	}

	switch store.SEVRoleType(req.GetRoleType()) {
	case store.SEVRoleOnCall, store.SEVRoleDetectedBy, store.SEVRoleIncidentCommander,
		store.SEVRoleCommsLead, store.SEVRoleRecorder, store.SEVRoleResponder:
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown role_type")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	role := &store.SEVRole{
		SEVID:       req.GetSevId(),
		RoleType:    store.SEVRoleType(req.GetRoleType()),
		DisplayName: req.GetDisplayName(),
		CreatedAt:   now,
		CreatedBy:   callerID,
	}
	if v := req.GetUserId(); v != "" {
		role.UserID = &v
	}

	if err := s.roles.Assign(ctx, role); err != nil {
		return nil, internalError(ctx, "failed to assign role", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "role.assigned",
		CreatedAt: now,
	})

	resp := roleToProto(role)
	if !sevRecord.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "role.changed", resp)
	}

	return resp, nil
}

func (s *RoleServer) RemoveRole(ctx context.Context, req *pb.RemoveRoleRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	if err := s.roles.Remove(ctx, req.GetSevId(), req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "role assignment not found")
		}
		return nil, internalError(ctx, "failed to remove role", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "role.removed",
		CreatedAt: time.Now(),
	})

	if !sevRecord.Sensitive {
		publishJSON(s.publisher, req.GetSevId(), "role.changed", map[string]any{
			"id":      req.GetId(),
			"removed": true,
		})
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleServer) ListRoles(ctx context.Context, req *pb.ListRolesRequest) (*pb.ListRolesResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := loadVisibleSEV(ctx, s.sevs, s.access, req.GetSevId()); err != nil {
		return nil, err
	}

	roles, err := s.roles.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list roles", err)
	}

	resp := &pb.ListRolesResponse{}
	for _, r := range roles {
		resp.Roles = append(resp.Roles, roleToProto(r))
	}
	return resp, nil
}

// InviteRoleToSlack invites the person holding one role assignment into
// sevID's already-created incident Slack channel (docs/roadmap.md Phase
// 10e) — a manual "add to chat" action for a role assigned after the
// channel was created. codes.FailedPrecondition when the SEV has no Slack
// channel recorded, when Slack invite support isn't wired up at all
// (slackFactory/integrations/crypto nil — matching TaskServer's
// GitHub/Jira-not-configured posture), or when the role holder has no
// resolvable Slack identity. Reuses the same identity-resolution order as
// cmd/slackbot/channel.go's inviteRoleHolders (§10d): stored SlackUserID →
// LookupUserIDByEmail(user's email) → DisplayName regex scrape → no match.
func (s *RoleServer) InviteRoleToSlack(ctx context.Context, req *pb.InviteRoleToSlackRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if s.slackFactory == nil || s.integrations == nil {
		return nil, status.Error(codes.Unavailable, "Slack integration is not configured")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}
	if sevRecord.SlackChannelID == nil || *sevRecord.SlackChannelID == "" {
		return nil, status.Error(codes.FailedPrecondition, "this SEV has no Slack channel to invite into")
	}

	roles, err := s.roles.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list roles", err)
	}
	var role *store.SEVRole
	for _, r := range roles {
		if r.ID == req.GetId() {
			role = r
			break
		}
	}
	if role == nil {
		return nil, status.Error(codes.NotFound, "role assignment not found")
	}

	cfg, err := s.integrations.Get(ctx, "slack")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.Unavailable, "Slack integration is not configured")
		}
		return nil, internalError(ctx, "failed to get slack integration config", err)
	}
	creds, err := DecryptIntegrationCredentials(s.crypto, cfg)
	if err != nil {
		return nil, internalError(ctx, "failed to decrypt slack integration credentials", err)
	}
	botToken := creds["bot_token"]
	if botToken == "" {
		return nil, status.Error(codes.Unavailable, "Slack integration is not configured")
	}
	slackClient := s.slackFactory(botToken)

	slackUserID, err := s.resolveRoleSlackUserID(ctx, slackClient, role)
	if err != nil {
		return nil, internalError(ctx, "failed to resolve Slack identity", err)
	}
	if slackUserID == "" {
		return nil, status.Error(codes.FailedPrecondition, "this role holder has no resolvable Slack identity")
	}

	if err := slackClient.InviteUsers(ctx, *sevRecord.SlackChannelID, []string{slackUserID}); err != nil {
		return nil, internalError(ctx, "failed to invite to Slack channel", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}
	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "role.invited_to_slack",
		CreatedAt: time.Now(),
	})

	return &emptypb.Empty{}, nil
}

// resolveRoleSlackUserID resolves role to a Slack user ID, trying (in
// order): a stored User.SlackUserID, LookupUserIDByEmail against that
// user's email, then a regex scrape of DisplayName for an
// "<email@example.com>" pattern. Returns ("", nil) — not an error — when no
// identity resolves, matching cmd/slackbot's "silently skip, don't fail"
// posture for an unmatched invitee.
func (s *RoleServer) resolveRoleSlackUserID(ctx context.Context, slackClient SlackInviteClient, role *store.SEVRole) (string, error) {
	if role.UserID != nil && *role.UserID != "" && s.users != nil {
		user, err := s.users.Get(ctx, *role.UserID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if user != nil {
			if user.SlackUserID != nil && *user.SlackUserID != "" {
				return *user.SlackUserID, nil
			}
			return slackClient.LookupUserIDByEmail(ctx, user.Email)
		}
	}
	if m := roleEmailInAngleBrackets.FindStringSubmatch(role.DisplayName); len(m) == 2 {
		return slackClient.LookupUserIDByEmail(ctx, m[1])
	}
	return "", nil
}

func roleToProto(r *store.SEVRole) *pb.SEVRoleResponse {
	resp := &pb.SEVRoleResponse{
		Id:          r.ID,
		SevId:       r.SEVID,
		RoleType:    string(r.RoleType),
		DisplayName: r.DisplayName,
		CreatedAt:   timestamppb.New(r.CreatedAt),
		CreatedBy:   r.CreatedBy,
	}
	if r.UserID != nil {
		resp.UserId = *r.UserID
	}
	return resp
}
