package auth_test

import (
	"testing"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

func TestHasPermission(t *testing.T) {
	tests := []struct {
		role   store.OrgRole
		method string
		want   bool
	}{
		// Viewer can read
		{store.OrgRoleViewer, "/sevitout.v1.SEVService/GetSEV", true},
		{store.OrgRoleViewer, "/sevitout.v1.SEVService/ListSEVs", true},
		{store.OrgRoleViewer, "/sevitout.v1.AuditService/ListAuditEntries", true},
		{store.OrgRoleViewer, "/sevitout.v1.AuthService/WhoAmI", true},
		{store.OrgRoleViewer, "/sevitout.v1.AuthService/UpdateMyIntegrationIdentities", true},
		{store.OrgRoleViewer, "/sevitout.v1.AuthService/ListUserDirectory", true},
		{store.OrgRoleViewer, "/sevitout.v1.RoleService/ListRoles", true},
		// Viewer cannot invite to Slack (Responder+) or manage roles (IC+)
		{store.OrgRoleViewer, "/sevitout.v1.RoleService/InviteRoleToSlack", false},
		{store.OrgRoleViewer, "/sevitout.v1.RoleService/AssignRole", false},
		{store.OrgRoleResponder, "/sevitout.v1.RoleService/InviteRoleToSlack", true},
		{store.OrgRoleResponder, "/sevitout.v1.RoleService/AssignRole", false},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.RoleService/AssignRole", true},
		// JoinSlackChannel is a self-service action, open to any Viewer.
		{store.OrgRoleViewer, "/sevitout.v1.RoleService/JoinSlackChannel", true},
		// Viewer cannot write
		{store.OrgRoleViewer, "/sevitout.v1.SEVService/CreateSEV", false},
		{store.OrgRoleViewer, "/sevitout.v1.SEVService/UpdateSEV", false},
		{store.OrgRoleViewer, "/sevitout.v1.SEVService/TransitionStatus", false},

		// Responder can read and write SEVs
		{store.OrgRoleResponder, "/sevitout.v1.SEVService/GetSEV", true},
		{store.OrgRoleResponder, "/sevitout.v1.SEVService/ListSEVs", true},
		{store.OrgRoleResponder, "/sevitout.v1.SEVService/CreateSEV", true},
		{store.OrgRoleResponder, "/sevitout.v1.SEVService/UpdateSEV", true},
		// Responder cannot transition status
		{store.OrgRoleResponder, "/sevitout.v1.SEVService/TransitionStatus", false},

		// Incident Commander can do everything a responder can, plus transition
		{store.OrgRoleIncidentCommander, "/sevitout.v1.SEVService/CreateSEV", true},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.SEVService/UpdateSEV", true},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.SEVService/TransitionStatus", true},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.SEVService/GetSEV", true},

		// Admin can do everything
		{store.OrgRoleAdmin, "/sevitout.v1.SEVService/CreateSEV", true},
		{store.OrgRoleAdmin, "/sevitout.v1.SEVService/TransitionStatus", true},
		{store.OrgRoleAdmin, "/sevitout.v1.AuditService/ListAuditEntries", true},
		{store.OrgRoleAdmin, "/sevitout.v1.AuthService/WhoAmI", true},

		// Announcement service
		{store.OrgRoleViewer, "/sevitout.v1.AnnouncementService/ListAnnouncements", true},
		{store.OrgRoleViewer, "/sevitout.v1.AnnouncementService/CreateAnnouncement", false},
		{store.OrgRoleResponder, "/sevitout.v1.AnnouncementService/CreateAnnouncement", true},
		{store.OrgRoleResponder, "/sevitout.v1.AnnouncementService/ListAnnouncements", true},

		// Chat service
		{store.OrgRoleViewer, "/sevitout.v1.ChatService/ListChatEntries", true},
		{store.OrgRoleViewer, "/sevitout.v1.ChatService/AddChatEntry", false},
		{store.OrgRoleResponder, "/sevitout.v1.ChatService/AddChatEntry", true},
		{store.OrgRoleResponder, "/sevitout.v1.ChatService/ListChatEntries", true},

		// SEV link service
		{store.OrgRoleViewer, "/sevitout.v1.SEVLinkService/ListLinkedSEVs", true},
		{store.OrgRoleViewer, "/sevitout.v1.SEVLinkService/LinkSEVs", false},
		{store.OrgRoleViewer, "/sevitout.v1.SEVLinkService/UnlinkSEVs", false},
		{store.OrgRoleResponder, "/sevitout.v1.SEVLinkService/LinkSEVs", true},
		{store.OrgRoleResponder, "/sevitout.v1.SEVLinkService/UnlinkSEVs", true},
		{store.OrgRoleResponder, "/sevitout.v1.SEVLinkService/ListLinkedSEVs", true},

		// Task service
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/ListTasks", true},
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/LinkTask", false},
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/UnlinkTask", false},
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/UpdateTaskDueDate", false},
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/CreateGitHubIssue", false},
		{store.OrgRoleViewer, "/sevitout.v1.TaskService/CreateJiraIssue", false},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/LinkTask", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/UnlinkTask", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/ListTasks", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/UpdateTaskDueDate", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/CreateGitHubIssue", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/CreateJiraIssue", true},

		// Search service
		{store.OrgRoleViewer, "/sevitout.v1.SearchService/SearchSEVs", true},
		{store.OrgRoleResponder, "/sevitout.v1.SearchService/SearchSEVs", true},

		// Report service — read-only, same floor as Search
		{store.OrgRoleViewer, "/sevitout.v1.ReportService/GetDashboardMetrics", true},
		{store.OrgRoleViewer, "/sevitout.v1.ReportService/GetSEVTrends", true},
		{store.OrgRoleViewer, "/sevitout.v1.ReportService/ExportSEVs", true},

		// Share service — creating/revoking a link needs Incident Commander
		{store.OrgRoleViewer, "/sevitout.v1.ShareService/CreateShareLink", false},
		{store.OrgRoleResponder, "/sevitout.v1.ShareService/CreateShareLink", false},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.ShareService/CreateShareLink", true},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.ShareService/RevokeShareLink", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ShareService/CreateShareLink", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ShareService/RevokeShareLink", true},

		// Config service — Viewer can read the service registry and on-call
		// rotations, but not user/integration/retention config or write anything
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListServices", true},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/GetService", true},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListOnCallRotations", true},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/GetOnCallRotation", true},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/CreateService", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/UpdateService", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/DeleteService", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListUsers", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/UpdateUserRole", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListIntegrationConfigs", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/UpsertIntegrationConfig", false},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListRetentionConfig", false},
		// Incident Commander gets no more config access than Responder/Viewer —
		// configuration is Admin territory even for write-capable roles.
		{store.OrgRoleIncidentCommander, "/sevitout.v1.ConfigService/CreateService", false},
		{store.OrgRoleIncidentCommander, "/sevitout.v1.ConfigService/ListUsers", false},
		// Admin has full config access
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/CreateService", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/UpdateService", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/DeleteService", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/ListUsers", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/UpdateUserRole", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/DeactivateUser", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/ReactivateUser", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/CreateOnCallRotation", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/UpdateOnCallRotation", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/DeleteOnCallRotation", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/UpsertIntegrationConfig", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/GetIntegrationConfig", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/ListIntegrationConfigs", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/GetRetentionConfig", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/UpdateRetentionConfig", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/ListRetentionConfig", true},
		{store.OrgRoleAdmin, "/sevitout.v1.ConfigService/IntegrationsHealth", true},
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/IntegrationsHealth", false},
		// ListEnabledIntegrations is the one integration-config RPC open to
		// any Viewer — it returns only a list of type strings.
		{store.OrgRoleViewer, "/sevitout.v1.ConfigService/ListEnabledIntegrations", true},

		// WebSocket subscription (pseudo-method checked manually by internal/api/ws)
		{store.OrgRoleViewer, "/sevitout.v1.WebSocket/Subscribe", true},
		{store.OrgRoleResponder, "/sevitout.v1.WebSocket/Subscribe", true},
		{store.OrgRoleAdmin, "/sevitout.v1.WebSocket/Subscribe", true},

		// Unknown RPC is denied to everyone
		{store.OrgRoleAdmin, "/sevitout.v1.UnknownService/DoSomething", false},
		{store.OrgRoleViewer, "/sevitout.v1.UnknownService/DoSomething", false},
	}

	for _, tt := range tests {
		got := auth.HasPermission(tt.role, tt.method)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.method, got, tt.want)
		}
	}
}
