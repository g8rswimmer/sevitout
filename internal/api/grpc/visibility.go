package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// loadVisibleSEV fetches the SEV identified by sevID and enforces its §14
// visibility via sensitiveSEVVisible. Any handler that accepts a bare sev_id
// and returns sub-resource data (announcements, chat, roles, tasks,
// postmortem, linked SEVs, ...) should call this instead of calling
// sevs.Get directly — otherwise a Sensitive SEV's sub-resources can be read
// by a caller who couldn't see the SEV itself.
//
// Both "the SEV doesn't exist" and "the SEV exists but isn't visible to this
// caller" map to codes.NotFound — never codes.PermissionDenied — matching
// sensitiveSEVVisible's existence-masking contract.
func loadVisibleSEV(ctx context.Context, sevs store.SEVStore, access store.SEVAccessStore, sevID string) (*store.SEV, error) {
	record, err := sevs.Get(ctx, sevID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}
	visible, err := sensitiveSEVVisible(ctx, access, record)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to check SEV visibility")
	}
	if !visible {
		return nil, status.Error(codes.NotFound, "SEV not found")
	}
	return record, nil
}
