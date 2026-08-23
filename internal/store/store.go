package store

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by all store implementations.
var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// SEVStore manages SEV records.
type SEVStore interface {
	Create(ctx context.Context, sev *SEV) error
	Get(ctx context.Context, id string) (*SEV, error)
	Update(ctx context.Context, sev *SEV) error
	List(ctx context.Context, filter SEVFilter) ([]*SEV, error)
	Count(ctx context.Context, filter SEVFilter) (int, error)
	UpdateLocked(ctx context.Context, id string, locked bool) error
}

// PostmortemStore manages the postmortem document attached to each SEV.
type PostmortemStore interface {
	Create(ctx context.Context, pm *Postmortem) error
	GetBySEVID(ctx context.Context, sevID string) (*Postmortem, error)
	Update(ctx context.Context, pm *Postmortem) error
}

// AuditStore is an append-only log of all SEV mutations.
// No update or delete operations exist by design.
type AuditStore interface {
	Append(ctx context.Context, entry *AuditEntry) error
	ListBySEVID(ctx context.Context, sevID string) ([]*AuditEntry, error)
}

// AnnouncementStore manages ordered status updates on a SEV.
type AnnouncementStore interface {
	Create(ctx context.Context, a *Announcement) error
	ListBySEVID(ctx context.Context, sevID string) ([]*Announcement, error)
	// SearchSEVIDs returns the distinct SEV IDs with at least one announcement
	// whose message matches query.
	SearchSEVIDs(ctx context.Context, query string) ([]string, error)
}

// ChatStore manages chat log entries captured during an incident.
type ChatStore interface {
	Create(ctx context.Context, entry *ChatEntry) error
	ListBySEVID(ctx context.Context, sevID string) ([]*ChatEntry, error)
}

// TaskStore manages external task references linked to a SEV.
type TaskStore interface {
	Create(ctx context.Context, task *LinkedTask) error
	Get(ctx context.Context, id int64) (*LinkedTask, error)
	Update(ctx context.Context, task *LinkedTask) error
	// SetDueDateIfUnset atomically sets a task's due date only if it doesn't
	// already have one, reporting whether the write was applied. Used to
	// back-fill a due date once without racing concurrent callers doing the
	// same back-fill for the same task.
	SetDueDateIfUnset(ctx context.Context, id int64, dueDate time.Time) (applied bool, err error)
	Delete(ctx context.Context, id int64) error
	ListBySEVID(ctx context.Context, sevID string) ([]*LinkedTask, error)
}

// SEVLinkStore manages typed bidirectional relationships between SEVs.
type SEVLinkStore interface {
	Create(ctx context.Context, link *SEVLink) error
	Delete(ctx context.Context, sourceSEVID, targetSEVID string, relType SEVRelationshipType) error
	ListBySEVID(ctx context.Context, sevID string) ([]*SEVLink, error)
}

// SLIStore manages SLI violation records on a SEV.
type SLIStore interface {
	Create(ctx context.Context, sli *SLI) error
	Delete(ctx context.Context, id int64) error
	ListBySEVID(ctx context.Context, sevID string) ([]*SLI, error)
}

// UserStore manages registered users.
type UserStore interface {
	Create(ctx context.Context, user *User) error
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	List(ctx context.Context) ([]*User, error)
	Count(ctx context.Context) (int64, error)
}

// ServiceStore manages the lightweight internal service registry.
type ServiceStore interface {
	Create(ctx context.Context, svc *Service) error
	Get(ctx context.Context, id string) (*Service, error)
	Update(ctx context.Context, svc *Service) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, activeOnly bool) ([]*Service, error)
}

// OnCallStore manages on-call rotation definitions and manual overrides.
type OnCallStore interface {
	Create(ctx context.Context, rotation *OnCallRotation) error
	Get(ctx context.Context, id int64) (*OnCallRotation, error)
	Update(ctx context.Context, rotation *OnCallRotation) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*OnCallRotation, error)
	// GetCurrentOnCall returns the active on-call entry for the given service.
	// It prefers a manual override with a matching time window over a PagerDuty rotation.
	GetCurrentOnCall(ctx context.Context, serviceID string) (*OnCallRotation, error)
}

// AIPluginStore manages registered AI provider plugin configurations.
type AIPluginStore interface {
	Create(ctx context.Context, plugin *AIPlugin) error
	Get(ctx context.Context, id int64) (*AIPlugin, error)
	Update(ctx context.Context, plugin *AIPlugin) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*AIPlugin, error)
}

// AIOutputStore manages stored AI-generated content, keyed by the SEV it was
// generated for. Outputs are append-only — there is no Update or Delete,
// matching the audit log's immutability posture (§11, §15).
type AIOutputStore interface {
	Create(ctx context.Context, output *AIOutput) error
	ListBySEVID(ctx context.Context, sevID string) ([]*AIOutput, error)
}

// IntegrationConfigStore manages per-integration credentials and settings.
// Callers are responsible for encrypting credentials before storing and
// decrypting after retrieval.
type IntegrationConfigStore interface {
	Get(ctx context.Context, integrationType string) (*IntegrationConfig, error)
	Upsert(ctx context.Context, config *IntegrationConfig) error
	List(ctx context.Context) ([]*IntegrationConfig, error)
}

// StatusHistoryStore is an immutable log of SEV status transitions.
type StatusHistoryStore interface {
	Create(ctx context.Context, h *SEVStatusHistory) error
	ListBySEVID(ctx context.Context, sevID string) ([]*SEVStatusHistory, error)
}

// RetentionConfigStore manages the per-severity-level retention policy.
type RetentionConfigStore interface {
	Get(ctx context.Context, severityLevel int16) (*RetentionConfig, error)
	Upsert(ctx context.Context, cfg *RetentionConfig) error
	List(ctx context.Context) ([]*RetentionConfig, error)
}

// ShareStore manages public shareable link tokens for SEVs.
type ShareStore interface {
	Create(ctx context.Context, link *ShareableLink) error
	GetByToken(ctx context.Context, token string) (*ShareableLink, error)
	Revoke(ctx context.Context, token string, revokedBy string) error
	ListBySEVID(ctx context.Context, sevID string) ([]*ShareableLink, error)
}

// RoleStore manages people and role assignments on SEVs.
type RoleStore interface {
	Assign(ctx context.Context, role *SEVRole) error
	// Remove deletes the role assignment with the given id from the given SEV.
	// Returns ErrNotFound when no matching assignment exists.
	Remove(ctx context.Context, sevID string, id int64) error
	ListBySEVID(ctx context.Context, sevID string) ([]*SEVRole, error)
	// ListSEVIDsByUser returns the distinct SEV IDs where user (matched
	// against either UserID or DisplayName) holds a role assignment. When
	// roleType is non-nil, only that role type is considered.
	ListSEVIDsByUser(ctx context.Context, user string, roleType *SEVRoleType) ([]string, error)
}
