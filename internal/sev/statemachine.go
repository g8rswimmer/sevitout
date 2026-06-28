package sev

import (
	"errors"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ErrInvalidTransition is returned when a requested status change is not
// permitted by the state machine.
var ErrInvalidTransition = errors.New("sev: invalid status transition")

// validTransitions maps each status to the set of statuses it may transition to.
var validTransitions = map[store.SEVStatus][]store.SEVStatus{
	store.SEVStatusOpen: {
		store.SEVStatusInvestigating,
		store.SEVStatusMitigated,
	},
	store.SEVStatusInvestigating: {
		store.SEVStatusMitigated,
		store.SEVStatusOpen, // re-open
	},
	store.SEVStatusMitigated: {
		store.SEVStatusResolved,
		store.SEVStatusInvestigating, // step back
	},
	store.SEVStatusResolved: {
		store.SEVStatusPostmortemInProgress,
	},
	store.SEVStatusPostmortemInProgress: {
		store.SEVStatusPostmortemComplete,
	},
	store.SEVStatusPostmortemComplete: {
		store.SEVStatusOpen, // re-open after postmortem
	},
}

// ValidateTransition returns ErrInvalidTransition if moving from → to is not
// permitted. A nil error means the transition is allowed.
func ValidateTransition(from, to store.SEVStatus) error {
	allowed, ok := validTransitions[from]
	if !ok {
		return ErrInvalidTransition
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return ErrInvalidTransition
}
