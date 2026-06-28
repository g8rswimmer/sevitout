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
	// Auth service
	"/sevitout.v1.AuthService/WhoAmI": store.OrgRoleViewer,
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
