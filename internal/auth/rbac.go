package auth

import "github.com/g8rswimmer/sevitout/internal/store"

// roleLevel assigns a numeric level to each org role so permission checks
// can be expressed as a minimum-level comparison.
var roleLevel = map[store.OrgRole]int{
	store.OrgRoleViewer:            1,
	store.OrgRoleResponder:         2,
	store.OrgRoleIncidentCommander: 3,
	store.OrgRoleAdmin:             4,
}

// rpcMinRole maps each gRPC full method path to the minimum OrgRole required
// to call it. Any method absent from this map is denied to all callers.
var rpcMinRole = map[string]store.OrgRole{
	// SEV service
	"/sevitout.v1.SEVService/CreateSEV":        store.OrgRoleResponder,
	"/sevitout.v1.SEVService/GetSEV":           store.OrgRoleViewer,
	"/sevitout.v1.SEVService/UpdateSEV":        store.OrgRoleResponder,
	"/sevitout.v1.SEVService/ListSEVs":         store.OrgRoleViewer,
	"/sevitout.v1.SEVService/TransitionStatus": store.OrgRoleIncidentCommander,
	// Audit service
	"/sevitout.v1.AuditService/ListAuditEntries": store.OrgRoleViewer,
	// Auth service — UpdateMyIntegrationIdentities/ListUserDirectory sit at
	// the same Viewer floor as WhoAmI: both act purely on the caller's own
	// identity or return a minimal public-safe directory, not a
	// user-management surface (docs/roadmap.md Phase 10a).
	"/sevitout.v1.AuthService/WhoAmI":                        store.OrgRoleViewer,
	"/sevitout.v1.AuthService/UpdateMyIntegrationIdentities": store.OrgRoleViewer,
	"/sevitout.v1.AuthService/ListUserDirectory":             store.OrgRoleViewer,
	// Role service — InviteRoleToSlack is an auxiliary invite action, not
	// role *management* like AssignRole/RemoveRole, so it sits at the lower
	// Responder floor (docs/roadmap.md Phase 10e).
	"/sevitout.v1.RoleService/AssignRole":        store.OrgRoleIncidentCommander,
	"/sevitout.v1.RoleService/RemoveRole":        store.OrgRoleIncidentCommander,
	"/sevitout.v1.RoleService/ListRoles":         store.OrgRoleViewer,
	"/sevitout.v1.RoleService/InviteRoleToSlack": store.OrgRoleResponder,
	// JoinSlackChannel is a self-service "add me" action, not role
	// management or even an auxiliary invite of someone else — any
	// authenticated Viewer with real (non visibility-restricted) access to
	// the SEV may call it; the handler itself enforces that finer-grained
	// access check (docs/roadmap.md Phase 11c).
	"/sevitout.v1.RoleService/JoinSlackChannel": store.OrgRoleViewer,
	// SEV access service — granting/revoking a user's visibility into a
	// Sensitive SEV (§14) is scoped the same as unlocking a completed SEV
	// and creating/revoking a share link: IC or Admin. ListAccess itself
	// stays open at Viewer, since the handler body re-applies
	// sensitiveSEVVisible per-record (see sev_access.go) — otherwise a
	// non-allowed Viewer could still probe who has access to a SEV they
	// can't see.
	"/sevitout.v1.SEVAccessService/GrantAccess":  store.OrgRoleIncidentCommander,
	"/sevitout.v1.SEVAccessService/RevokeAccess": store.OrgRoleIncidentCommander,
	"/sevitout.v1.SEVAccessService/ListAccess":   store.OrgRoleViewer,
	// Postmortem service
	"/sevitout.v1.PostmortemService/GetPostmortem":              store.OrgRoleViewer,
	"/sevitout.v1.PostmortemService/UpdatePostmortem":           store.OrgRoleResponder,
	"/sevitout.v1.PostmortemService/TransitionPostmortemStatus": store.OrgRoleIncidentCommander,
	"/sevitout.v1.PostmortemService/UnlockSEV":                  store.OrgRoleIncidentCommander,
	// Announcement service
	"/sevitout.v1.AnnouncementService/CreateAnnouncement": store.OrgRoleResponder,
	"/sevitout.v1.AnnouncementService/ListAnnouncements":  store.OrgRoleViewer,
	// Chat service
	"/sevitout.v1.ChatService/AddChatEntry":    store.OrgRoleResponder,
	"/sevitout.v1.ChatService/ListChatEntries": store.OrgRoleViewer,
	// SEV link service
	"/sevitout.v1.SEVLinkService/LinkSEVs":       store.OrgRoleResponder,
	"/sevitout.v1.SEVLinkService/UnlinkSEVs":     store.OrgRoleResponder,
	"/sevitout.v1.SEVLinkService/ListLinkedSEVs": store.OrgRoleViewer,
	// Task service
	"/sevitout.v1.TaskService/LinkTask":          store.OrgRoleResponder,
	"/sevitout.v1.TaskService/UnlinkTask":        store.OrgRoleResponder,
	"/sevitout.v1.TaskService/ListTasks":         store.OrgRoleViewer,
	"/sevitout.v1.TaskService/UpdateTaskDueDate": store.OrgRoleResponder,
	"/sevitout.v1.TaskService/CreateGitHubIssue": store.OrgRoleResponder,
	"/sevitout.v1.TaskService/CreateJiraIssue":   store.OrgRoleResponder,
	// Search service
	"/sevitout.v1.SearchService/SearchSEVs": store.OrgRoleViewer,
	// Report service — dashboard/trends/export are all read-only, same Viewer
	// floor as ListSEVs/SearchSEVs.
	"/sevitout.v1.ReportService/GetDashboardMetrics": store.OrgRoleViewer,
	"/sevitout.v1.ReportService/GetSEVTrends":        store.OrgRoleViewer,
	"/sevitout.v1.ReportService/ExportSEVs":          store.OrgRoleViewer,
	"/sevitout.v1.ReportService/GetServiceMetrics":   store.OrgRoleViewer,
	// Share service — creating/revoking a public link is scoped the same as
	// unlocking a completed SEV (§10.1, §14.1): IC or Admin. The public view
	// itself (GET /s/{token}) isn't a gRPC method at all — see share.proto.
	"/sevitout.v1.ShareService/CreateShareLink": store.OrgRoleIncidentCommander,
	"/sevitout.v1.ShareService/RevokeShareLink": store.OrgRoleIncidentCommander,
	// Config service — service registry and on-call rotations are readable
	// by any authenticated user (referenced elsewhere in the UI); everything
	// else is Admin-only per docs/requirements.md §18.
	"/sevitout.v1.ConfigService/CreateService": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/GetService":    store.OrgRoleViewer,
	"/sevitout.v1.ConfigService/UpdateService": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/DeleteService": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListServices":  store.OrgRoleViewer,
	// Per-service SLA targets (docs/roadmap.md Phase 12) — reads sit at the
	// same Viewer floor as GetService/ListServices, since the resolved
	// numbers are already exposed to any Viewer via SEVResponse.sla_status;
	// mutations match UpdateService/DeleteService's Admin floor.
	"/sevitout.v1.ConfigService/GetServiceSLA":        store.OrgRoleViewer,
	"/sevitout.v1.ConfigService/UpsertServiceSLA":     store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/DeleteServiceSLA":     store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListServiceSLAs":      store.OrgRoleViewer,
	"/sevitout.v1.ConfigService/ListUsers":            store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/UpdateUserRole":       store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/DeactivateUser":       store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ReactivateUser":       store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/CreateOnCallRotation": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/GetOnCallRotation":    store.OrgRoleViewer,
	"/sevitout.v1.ConfigService/UpdateOnCallRotation": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/DeleteOnCallRotation": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListOnCallRotations":  store.OrgRoleViewer,
	// GetIntegrationCatalog is a pure static-schema read (no store access,
	// nothing per-integration sensitive) but stays Admin-only for
	// consistency with the rest of this integration-config group.
	"/sevitout.v1.ConfigService/GetIntegrationCatalog":   store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/UpsertIntegrationConfig": store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/GetIntegrationConfig":    store.OrgRoleAdmin,
	// GetSlackBotCredential sits at the same Admin floor as every other
	// integration-config RPC, but ConfigServer.GetSlackBotCredential narrows
	// it further, server-side, to one specific Admin account (the slackbot
	// service user) — see that method's doc comment.
	"/sevitout.v1.ConfigService/GetSlackBotCredential":  store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListIntegrationConfigs": store.OrgRoleAdmin,
	// ListEnabledIntegrations is deliberately at the Viewer floor, not the
	// Admin floor every other integration-config RPC above sits at — it
	// returns only a list of type strings, no settings/credentials state,
	// safe for any authenticated user the same way ChatService.ListChatEntries
	// sits at Viewer alongside ChatService's higher-gated AddChatEntry (RBAC
	// is per-RPC, not per-service). See docs/roadmap.md Phase 11a.
	"/sevitout.v1.ConfigService/ListEnabledIntegrations": store.OrgRoleViewer,
	"/sevitout.v1.ConfigService/GetRetentionConfig":      store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/UpdateRetentionConfig":   store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListRetentionConfig":     store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/CreateAIPlugin":          store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/GetAIPlugin":             store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/UpdateAIPlugin":          store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/DeleteAIPlugin":          store.OrgRoleAdmin,
	"/sevitout.v1.ConfigService/ListAIPlugins":           store.OrgRoleAdmin,
	// AI service — running/streaming an action needs at least Responder
	// (same floor as most SEV-mutating actions); listing outputs and
	// available plugins is read-only and open to any authenticated user.
	"/sevitout.v1.AIService/TriggerAction": store.OrgRoleResponder,
	"/sevitout.v1.AIService/StreamAction":  store.OrgRoleResponder,
	"/sevitout.v1.AIService/ListOutputs":   store.OrgRoleViewer,
	"/sevitout.v1.AIService/ListPlugins":   store.OrgRoleViewer,
	// GET /admin/integrations/health: not a real gRPC method — see
	// internal/api/grpc.IntegrationsHealthHandler, which checks this entry
	// directly since it bypasses the gRPC interceptor entirely.
	"/sevitout.v1.ConfigService/IntegrationsHealth": store.OrgRoleAdmin,
	// WebSocket: not a real gRPC method — internal/api/ws.Handler checks this
	// entry directly (via HasPermission) since WS connections bypass the
	// gRPC interceptor entirely and need their own RBAC floor.
	"/sevitout.v1.WebSocket/Subscribe": store.OrgRoleViewer,
	// gRPC reflection (both v1alpha and v1 registered by grpc-go)
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": store.OrgRoleViewer,
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      store.OrgRoleViewer,
}

// HasPermission reports whether role meets the minimum required for method.
// Unknown methods are denied by default.
func HasPermission(role store.OrgRole, method string) bool {
	minRole, ok := rpcMinRole[method]
	if !ok {
		return false
	}
	return roleLevel[role] >= roleLevel[minRole]
}
