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
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/LinkTask", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/UnlinkTask", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/ListTasks", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/UpdateTaskDueDate", true},
		{store.OrgRoleResponder, "/sevitout.v1.TaskService/CreateGitHubIssue", true},

		// Search service
		{store.OrgRoleViewer, "/sevitout.v1.SearchService/SearchSEVs", true},
		{store.OrgRoleResponder, "/sevitout.v1.SearchService/SearchSEVs", true},

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
