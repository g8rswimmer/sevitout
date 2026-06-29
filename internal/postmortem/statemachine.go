package postmortem

import (
	"errors"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ErrInvalidTransition is returned when a requested status change is not
// permitted by the postmortem state machine.
var ErrInvalidTransition = errors.New("postmortem: invalid status transition")

// validTransitions maps each postmortem status to the set it may transition to.
var validTransitions = map[store.PostmortemStatus][]store.PostmortemStatus{
	store.PostmortemStatusDraft: {
		store.PostmortemStatusInReview,
	},
	store.PostmortemStatusInReview: {
		store.PostmortemStatusApproved,
		store.PostmortemStatusDraft, // send back for revision
	},
	store.PostmortemStatusApproved: {},
}

// ValidateTransition returns ErrInvalidTransition when moving from → to is not
// permitted by the state machine.
func ValidateTransition(from, to store.PostmortemStatus) error {
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
