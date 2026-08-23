// Package ai implements the pluggable AI provider system (docs/requirements.md
// §11, docs/architecture.md §8): a Provider interface any AI backend
// implements, a Dispatcher that routes lifecycle events and on-demand
// requests to the configured provider, and two built-in providers (a real
// Anthropic HTTP client and a generic HTTP provider for externally hosted
// handlers).
package ai

import (
	"context"
	"time"
)

// Action identifies one capability a Provider exposes. It is also the value
// stored in AIOutput.Action and mirrors pb.AIAction (proto/sevitout/v1/ai.proto)
// one for one; internal/api/grpc converts between the two so this package
// never imports generated protobuf code.
type Action string

const (
	ActionSummarize         Action = "summarize"
	ActionSuggestRootCause  Action = "suggest_root_cause"
	ActionDraftPostmortem   Action = "draft_postmortem"
	ActionSuggestTasks      Action = "suggest_tasks"
	ActionFindSimilar       Action = "find_similar"
	ActionSuggestResponders Action = "suggest_responders"
	ActionDraftAnnouncement Action = "draft_announcement"
)

// TriggerEvent identifies the lifecycle moment that caused a proactive
// dispatch (§11.1), or ManualTrigger for a user-initiated action (§11.2).
type TriggerEvent string

const (
	TriggerSEVOpened          TriggerEvent = "sev.opened"
	TriggerSEVMitigated       TriggerEvent = "sev.mitigated"
	TriggerSEVResolved        TriggerEvent = "sev.resolved"
	TriggerPostmortemInReview TriggerEvent = "postmortem.in_review"
	ManualTrigger             TriggerEvent = "manual"
)

// SEVContext is the read-only view of a SEV (and its related records) handed
// to every Provider method. Providers must not mutate it. Built by
// Dispatcher.buildContext from the store layer so providers stay decoupled
// from internal/store entirely.
type SEVContext struct {
	ID                   string
	Title                string
	Description          string
	SeverityLevel        int16
	Status               string
	RootCauseCategory    string
	RootCauseDescription string
	Mitigation           string
	Prevention           string
	BusinessImpact       string
	AffectedServices     []string
	DetectionMethod      string
	StartedAt            *time.Time
	DetectedAt           *time.Time
	MitigatedAt          *time.Time
	ResolvedAt           *time.Time
	// Timeline merges status transitions and announcements into a single
	// chronological narrative, the same raw material the postmortem's
	// auto-populated Timeline section uses (§10).
	Timeline []TimelineEntry
	// Similar holds other SEVs sharing an affected service, for FindSimilar
	// and SuggestRootCause to reference without querying the store
	// themselves — Dispatcher resolves this via SEVStore before dispatch.
	Similar []SimilarSEVSummary
}

// TimelineEntry is one chronological event surfaced to a provider: either a
// status transition or an announcement.
type TimelineEntry struct {
	At      time.Time
	Kind    string // "status_change" or "announcement"
	Summary string
}

// SimilarSEVSummary is the minimal candidate information passed into
// FindSimilar/SuggestRootCause — the provider decides relevance, Dispatcher
// only narrows the candidate pool (currently: shares an affected service).
type SimilarSEVSummary struct {
	ID                string
	Title             string
	RootCauseCategory string
}

// RootCauseSuggestion is one candidate root cause category with rationale.
type RootCauseSuggestion struct {
	Category  string `json:"category"`
	Rationale string `json:"rationale"`
}

// PostmortemDraft is a skeleton postmortem body, one string per standard
// section (§10), ready to seed Postmortem.Content as Markdown.
type PostmortemDraft struct {
	Summary             string `json:"summary"`
	CustomerImpact      string `json:"customer_impact"`
	Timeline            string `json:"timeline"`
	RootCause           string `json:"root_cause"`
	ContributingFactors string `json:"contributing_factors"`
	LessonsLearned      string `json:"lessons_learned"`
	ActionItems         string `json:"action_items"`
}

// TaskSuggestion is one candidate follow-up/action-item task.
type TaskSuggestion struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	RelationshipType string `json:"relationship_type"`
	Priority         string `json:"priority"`
}

// SimilarSEV is one historical SEV judged similar, with the provider's own
// explanation of why.
type SimilarSEV struct {
	SEVID  string `json:"sev_id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// ResponderSuggestion is one candidate person/role for a newly opened SEV.
type ResponderSuggestion struct {
	Role      string `json:"role"`
	Rationale string `json:"rationale"`
}

// Chunk is one piece of a streamed action's output. Done marks the final
// chunk; Content on that final chunk is what gets persisted, matching what
// the synchronous equivalent (e.g. DraftPostmortem, marshaled to its final
// string form) would have produced.
type Chunk struct {
	Content string
	Done    bool
}

// Provider is implemented by every AI backend (built-in or HTTP-configured).
// All methods take a fully-populated SEVContext and must not block
// indefinitely — callers apply their own timeout via ctx.
//
// This extends the six methods sketched in docs/architecture.md §8 with
// SuggestResponders and DraftAnnouncement so every proactive trigger and
// user-triggered action in docs/requirements.md §11 has a concrete method to
// call; docs/architecture.md has been updated to match.
type Provider interface {
	Summarize(ctx context.Context, sev *SEVContext) (string, error)
	SuggestRootCause(ctx context.Context, sev *SEVContext) ([]RootCauseSuggestion, error)
	DraftPostmortem(ctx context.Context, sev *SEVContext) (*PostmortemDraft, error)
	SuggestTasks(ctx context.Context, sev *SEVContext) ([]TaskSuggestion, error)
	FindSimilar(ctx context.Context, sev *SEVContext) ([]SimilarSEV, error)
	SuggestResponders(ctx context.Context, sev *SEVContext) ([]ResponderSuggestion, error)
	DraftAnnouncement(ctx context.Context, sev *SEVContext) (string, error)
	// StreamAction runs action and streams its output incrementally. The
	// channel is closed after the final (Done==true) chunk or an error.
	StreamAction(ctx context.Context, action Action, sev *SEVContext) (<-chan Chunk, error)
}
