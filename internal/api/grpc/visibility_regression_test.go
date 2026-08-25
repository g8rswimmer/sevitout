package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// TestSensitiveSEVSubResourcesHiddenFromCallerWithoutAccess is the §14
// regression backstop referenced by loadVisibleSEV's doc comment
// (visibility.go): every RPC that accepts a bare sev_id and returns
// sub-resource data for that SEV must enforce sensitiveSEVVisible, or a
// Sensitive SEV's sub-resources leak to a caller who couldn't see the SEV
// itself. Add a new case here whenever a new "list/get sub-resource by
// sev_id" RPC is introduced, so a handler that forgets the check fails this
// test instead of only looking wrong to a reviewer.
//
// Each case seeds real sub-resource data for the sensitive SEV so that a
// handler which merely returns "not found" for an empty result set (rather
// than because it checked visibility) cannot pass by accident.
func TestSensitiveSEVSubResourcesHiddenFromCallerWithoutAccess(t *testing.T) {
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()

	now := time.Now()
	sv := &store.SEV{
		Title: "Sensitive incident", SeverityLevel: 1, Status: store.SEVStatusOpen,
		Sensitive: true, CreatedBy: "user-admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}
	sevID := sv.ID

	announcements := memory.NewAnnouncementStore()
	if err := announcements.Create(context.Background(), &store.Announcement{
		SEVID: sevID, AuthorID: "user-admin", Message: "internal update",
		Audience: store.AudienceInternal, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed announcement: %v", err)
	}

	chat := memory.NewChatStore()
	if err := chat.Create(context.Background(), &store.ChatEntry{
		SEVID: sevID, OccurredAt: now, Source: "slack", Author: "user-admin",
		Content: "sensitive chat content", AddedAt: now, AddedBy: "user-admin",
	}); err != nil {
		t.Fatalf("seed chat entry: %v", err)
	}

	roles := memory.NewRoleStore()
	if err := roles.Assign(context.Background(), &store.SEVRole{
		SEVID: sevID, RoleType: store.SEVRoleIncidentCommander, DisplayName: "IC",
		CreatedAt: now, CreatedBy: "user-admin",
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	tasks := memory.NewTaskStore()
	if err := tasks.Create(context.Background(), &store.LinkedTask{
		SEVID: sevID, ExternalSystem: "github", TaskID: "1", URL: "https://example.test/1",
		Title: "follow up", RelationshipType: store.TaskRelationshipActionItem,
		Priority: store.TaskPriorityNonCritical, CreatedAt: now, CreatedBy: "user-admin",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	postmortems := memory.NewPostmortemStore()
	if err := postmortems.Create(context.Background(), &store.Postmortem{
		SEVID: sevID, Status: store.PostmortemStatusDraft, Content: "sensitive root cause detail",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed postmortem: %v", err)
	}

	links := memory.NewSEVLinkStore()
	otherSEV := &store.SEV{
		Title: "Other incident", SeverityLevel: 3, Status: store.SEVStatusOpen,
		CreatedBy: "user-admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := sevs.Create(context.Background(), otherSEV); err != nil {
		t.Fatalf("seed other SEV: %v", err)
	}
	if err := links.Create(context.Background(), &store.SEVLink{
		SourceSEVID: sevID, TargetSEVID: otherSEV.ID, RelationshipType: store.SEVRelationshipRelated,
		CreatedAt: now, CreatedBy: "user-admin",
	}); err != nil {
		t.Fatalf("seed sev link: %v", err)
	}

	audit := memory.NewAuditStore()
	if err := audit.Append(context.Background(), &store.AuditEntry{
		SEVID: sevID, UserID: "user-admin", Action: "sev.created", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed audit entry: %v", err)
	}

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})

	cases := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{
			name: "AnnouncementService.ListAnnouncements",
			call: func(ctx context.Context) error {
				s := grpchandler.NewAnnouncementServer(announcements, sevs, access, nil)
				_, err := s.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "ChatService.ListChatEntries",
			call: func(ctx context.Context) error {
				s := grpchandler.NewChatServer(chat, sevs, access, nil)
				_, err := s.ListChatEntries(ctx, &pb.ListChatEntriesRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "RoleService.ListRoles",
			call: func(ctx context.Context) error {
				s := grpchandler.NewRoleServer(grpchandler.RoleServerParams{
					Roles: roles, SEVs: sevs, Access: access, Audit: memory.NewAuditStore(),
				})
				_, err := s.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "TaskService.ListTasks",
			call: func(ctx context.Context) error {
				s := grpchandler.NewTaskServer(grpchandler.TaskServerParams{
					Tasks: tasks, SEVs: sevs, Access: access, Audit: memory.NewAuditStore(),
				})
				_, err := s.ListTasks(ctx, &pb.ListTasksRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "PostmortemService.GetPostmortem",
			call: func(ctx context.Context) error {
				s := grpchandler.NewPostmortemServer(grpchandler.PostmortemServerParams{
					Postmortems: postmortems, SEVs: sevs, Access: access, Audit: memory.NewAuditStore(),
					Unlock: postmortem.NewUnlockSigner("test-secret-at-least-32-chars-long"),
				})
				_, err := s.GetPostmortem(ctx, &pb.GetPostmortemRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "SEVLinkService.ListLinkedSEVs",
			call: func(ctx context.Context) error {
				s := grpchandler.NewSEVLinkServer(links, sevs, access, memory.NewAuditStore())
				_, err := s.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevID})
				return err
			},
		},
		{
			name: "AuditService.ListAuditEntries",
			call: func(ctx context.Context) error {
				s := grpchandler.NewAuditServer(audit, sevs, access)
				_, err := s.ListAuditEntries(ctx, &pb.ListAuditEntriesRequest{SevId: sevID})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(viewerCtx)
			if err == nil {
				t.Fatalf("%s: want error for a Sensitive SEV the caller has no grant to, got nil (sub-resource data leaked)", tc.name)
			}
			if code := grpcCode(err); code != codes.NotFound {
				t.Errorf("%s: error code = %v, want NotFound (masking existence, not PermissionDenied)", tc.name, code)
			}
		})
	}
}
