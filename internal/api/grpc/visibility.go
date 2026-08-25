package grpc

import (
	"context"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// sensitiveSEVVisible reports whether record is visible to ctx's caller
// (§14: "only explicitly added users can view" a Sensitive SEV). A
// non-Sensitive record is always visible.
//
// A Sensitive record is visible to an Admin or Incident Commander
// unconditionally — the same "manages sensitive things" trust boundary
// already used for PostmortemService.UnlockSEV and
// ShareService.CreateShareLink/RevokeShareLink (both gated at
// store.OrgRoleIncidentCommander in internal/auth/rbac.go). An Incident
// Commander needs this bypass to be able to find a Sensitive SEV at all in
// order to grant someone else access to it via SEVAccessService, which is
// itself gated at the same IC floor.
//
// Anyone else is visible only via an explicit grant in access.
//
// Callers MUST translate a false result into codes.NotFound, never
// codes.PermissionDenied — mirrors share_view.go's ShareViewHandler, which
// returns "link not found" rather than confirming a Sensitive SEV exists to
// a caller who can't see it.
func sensitiveSEVVisible(ctx context.Context, access store.SEVAccessStore, record *store.SEV) (bool, error) {
	if !record.Sensitive {
		return true, nil
	}
	uc, ok := auth.UserFromContext(ctx)
	if !ok {
		return false, nil
	}
	if uc.OrgRole == store.OrgRoleAdmin || uc.OrgRole == store.OrgRoleIncidentCommander {
		return true, nil
	}
	return access.HasAccess(ctx, record.ID, uc.UserID)
}
