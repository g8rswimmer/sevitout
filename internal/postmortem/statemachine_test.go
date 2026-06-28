package postmortem_test

import (
	"testing"

	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/store"
)

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		from    store.PostmortemStatus
		to      store.PostmortemStatus
		wantErr bool
	}{
		// Valid paths
		{store.PostmortemStatusDraft, store.PostmortemStatusInReview, false},
		{store.PostmortemStatusInReview, store.PostmortemStatusApproved, false},
		{store.PostmortemStatusInReview, store.PostmortemStatusDraft, false},

		// Invalid paths
		{store.PostmortemStatusDraft, store.PostmortemStatusApproved, true},
		{store.PostmortemStatusDraft, store.PostmortemStatusDraft, true},
		{store.PostmortemStatusApproved, store.PostmortemStatusDraft, true},
		{store.PostmortemStatusApproved, store.PostmortemStatusInReview, true},
		{store.PostmortemStatusApproved, store.PostmortemStatusApproved, true},
		{"unknown", store.PostmortemStatusDraft, true},
	}

	for _, tc := range tests {
		err := postmortem.ValidateTransition(tc.from, tc.to)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateTransition(%q, %q): got err=%v, wantErr=%v", tc.from, tc.to, err, tc.wantErr)
		}
	}
}
