package grpc

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// ShareTokenValidator checks a share token's signature and expiry, returning
// the SEV ID it's scoped to. Declared here (the consumer) per this repo's
// interface-ownership convention — share.Signer satisfies it implicitly.
type ShareTokenValidator interface {
	Validate(tokenStr string) (sevID string, err error)
}

// sharedAnnouncement is the curated shape of one announcement in the public
// view — author identity is deliberately omitted (not listed among the
// exposed fields in docs/requirements.md §14.1).
type sharedAnnouncement struct {
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// sharedSEVResponse is the curated, public-safe view of a SEV served at
// GET /s/{token}. Only the fields docs/requirements.md §14.1 lists are
// present: "title, severity, status, lifecycle timestamps, announcements
// marked external, and business impact" — everything else (root cause,
// mitigation, chat log, audit log, tags, internal announcements, etc.) is
// deliberately left out, not just hidden by zero-valuing.
type sharedSEVResponse struct {
	ID                    string               `json:"id"`
	Title                 string               `json:"title"`
	SeverityLevel         int16                `json:"severity_level"`
	Status                string               `json:"status"`
	StartedAt             *time.Time           `json:"started_at,omitempty"`
	DetectedAt            *time.Time           `json:"detected_at,omitempty"`
	MitigatedAt           *time.Time           `json:"mitigated_at,omitempty"`
	ResolvedAt            *time.Time           `json:"resolved_at,omitempty"`
	PostmortemCompletedAt *time.Time           `json:"postmortem_completed_at,omitempty"`
	BusinessImpact        string               `json:"business_impact,omitempty"`
	Announcements         []sharedAnnouncement `json:"announcements"`
}

// ShareViewHandler serves GET /s/{token}: the public, unauthenticated
// shareable-link view (docs/requirements.md §14.1). It intentionally does
// NOT check any bearer token or RBAC — see share.proto's doc comment for why
// this can't be a normal gRPC/grpc-gateway route. Revocation and expiry are
// enforced from ShareStore (the authority — a signed token can't be
// un-signed); Validate is still called as a second, independent check on the
// token's own signature and embedded expiry.
type ShareViewHandler struct {
	shares        store.ShareStore
	sevs          store.SEVStore
	announcements store.AnnouncementStore
	validator     ShareTokenValidator
}

// ShareViewHandlerParams groups NewShareViewHandler's dependencies.
type ShareViewHandlerParams struct {
	Shares        store.ShareStore
	SEVs          store.SEVStore
	Announcements store.AnnouncementStore
	Validator     ShareTokenValidator
}

// NewShareViewHandler returns a ShareViewHandler backed by p.
func NewShareViewHandler(p ShareViewHandlerParams) *ShareViewHandler {
	return &ShareViewHandler{shares: p.Shares, sevs: p.SEVs, announcements: p.Announcements, validator: p.Validator}
}

func (h *ShareViewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := telemetry.LoggerFromContext(r.Context())

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	link, err := h.shares.GetByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
		log.ErrorContext(r.Context(), "share view: failed to look up link", "err", err)
		http.Error(w, "failed to look up link", http.StatusInternalServerError)
		return
	}
	if link.Revoked {
		http.Error(w, "link has been revoked", http.StatusGone)
		return
	}
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		http.Error(w, "link has expired", http.StatusGone)
		return
	}

	sevID, err := h.validator.Validate(token)
	if err != nil || sevID != link.SEVID {
		// The DB checks above are the real authority; this only catches a
		// corrupted/forged token string that happened to collide with a
		// stored row, which should never occur in practice.
		http.Error(w, "invalid link", http.StatusNotFound)
		return
	}

	sev, err := h.sevs.Get(r.Context(), link.SEVID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "SEV not found", http.StatusNotFound)
			return
		}
		log.ErrorContext(r.Context(), "share view: failed to get SEV", "sev_id", link.SEVID, "err", err)
		http.Error(w, "failed to get SEV", http.StatusInternalServerError)
		return
	}
	// Defense-in-depth: CreateShareLink already refuses to mint a link for a
	// Sensitive SEV, but re-check here too in case the SEV was flagged
	// Sensitive after the link was created — mirrors how ai.Dispatcher
	// re-checks Sensitive against a freshly-fetched record rather than
	// trusting a check made earlier in the request's lifetime.
	if sev.Sensitive {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	all, err := h.announcements.ListBySEVID(r.Context(), sev.ID)
	if err != nil {
		log.ErrorContext(r.Context(), "share view: failed to list announcements", "sev_id", sev.ID, "err", err)
		http.Error(w, "failed to list announcements", http.StatusInternalServerError)
		return
	}
	external := []sharedAnnouncement{}
	for _, a := range all {
		if a.Audience != store.AudienceExternal {
			continue
		}
		external = append(external, sharedAnnouncement{Message: a.Message, CreatedAt: a.CreatedAt})
	}

	resp := sharedSEVResponse{
		ID:                    sev.ID,
		Title:                 sev.Title,
		SeverityLevel:         sev.SeverityLevel,
		Status:                string(sev.Status),
		StartedAt:             sev.StartedAt,
		DetectedAt:            sev.DetectedAt,
		MitigatedAt:           sev.MitigatedAt,
		ResolvedAt:            sev.ResolvedAt,
		PostmortemCompletedAt: sev.PostmortemCompletedAt,
		Announcements:         external,
	}
	if sev.BusinessImpact != nil {
		resp.BusinessImpact = *sev.BusinessImpact
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
