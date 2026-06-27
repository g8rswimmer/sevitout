package sev_test

import (
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

func TestValidateTransition_Valid(t *testing.T) {
	cases := []struct {
		name string
		from store.SEVStatus
		to   store.SEVStatus
	}{
		// Open edges
		{"Open→Investigating", store.SEVStatusOpen, store.SEVStatusInvestigating},
		{"Open→Mitigated", store.SEVStatusOpen, store.SEVStatusMitigated},
		// Investigating edges
		{"Investigating→Mitigated", store.SEVStatusInvestigating, store.SEVStatusMitigated},
		{"Investigating→Open(reopen)", store.SEVStatusInvestigating, store.SEVStatusOpen},
		// Mitigated edges
		{"Mitigated→Resolved", store.SEVStatusMitigated, store.SEVStatusResolved},
		{"Mitigated→Investigating(stepback)", store.SEVStatusMitigated, store.SEVStatusInvestigating},
		// Resolved edges
		{"Resolved→PostmortemInProgress", store.SEVStatusResolved, store.SEVStatusPostmortemInProgress},
		// PostmortemInProgress edge
		{"PostmortemInProgress→PostmortemComplete", store.SEVStatusPostmortemInProgress, store.SEVStatusPostmortemComplete},
		// PostmortemComplete edge
		{"PostmortemComplete→Open(reopen)", store.SEVStatusPostmortemComplete, store.SEVStatusOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := sev.ValidateTransition(tc.from, tc.to); err != nil {
				t.Errorf("ValidateTransition(%q → %q) = %v, want nil", tc.from, tc.to, err)
			}
		})
	}
}

func TestValidateTransition_Invalid(t *testing.T) {
	cases := []struct {
		name string
		from store.SEVStatus
		to   store.SEVStatus
	}{
		// From Open: jumps and self-transition
		{"Open→Open(self)", store.SEVStatusOpen, store.SEVStatusOpen},
		{"Open→Resolved", store.SEVStatusOpen, store.SEVStatusResolved},
		{"Open→PostmortemInProgress", store.SEVStatusOpen, store.SEVStatusPostmortemInProgress},
		{"Open→PostmortemComplete", store.SEVStatusOpen, store.SEVStatusPostmortemComplete},
		// From Investigating
		{"Investigating→Resolved", store.SEVStatusInvestigating, store.SEVStatusResolved},
		{"Investigating→PostmortemInProgress", store.SEVStatusInvestigating, store.SEVStatusPostmortemInProgress},
		{"Investigating→Investigating(self)", store.SEVStatusInvestigating, store.SEVStatusInvestigating},
		// From Mitigated: Open is not in the allowed set
		{"Mitigated→Open", store.SEVStatusMitigated, store.SEVStatusOpen},
		{"Mitigated→PostmortemInProgress", store.SEVStatusMitigated, store.SEVStatusPostmortemInProgress},
		{"Mitigated→Mitigated(self)", store.SEVStatusMitigated, store.SEVStatusMitigated},
		// From Resolved: re-open is only valid via PostmortemComplete
		{"Resolved→Open", store.SEVStatusResolved, store.SEVStatusOpen},
		{"Resolved→Investigating", store.SEVStatusResolved, store.SEVStatusInvestigating},
		{"Resolved→Mitigated", store.SEVStatusResolved, store.SEVStatusMitigated},
		{"Resolved→PostmortemComplete", store.SEVStatusResolved, store.SEVStatusPostmortemComplete},
		// From PostmortemInProgress
		{"PostmortemInProgress→Open", store.SEVStatusPostmortemInProgress, store.SEVStatusOpen},
		{"PostmortemInProgress→Resolved", store.SEVStatusPostmortemInProgress, store.SEVStatusResolved},
		// From PostmortemComplete
		{"PostmortemComplete→Resolved", store.SEVStatusPostmortemComplete, store.SEVStatusResolved},
		{"PostmortemComplete→PostmortemInProgress", store.SEVStatusPostmortemComplete, store.SEVStatusPostmortemInProgress},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sev.ValidateTransition(tc.from, tc.to)
			if err == nil {
				t.Errorf("ValidateTransition(%q → %q) = nil, want ErrInvalidTransition", tc.from, tc.to)
				return
			}
			if !errors.Is(err, sev.ErrInvalidTransition) {
				t.Errorf("ValidateTransition(%q → %q) = %v, want ErrInvalidTransition", tc.from, tc.to, err)
			}
		})
	}
}

func TestValidateTransition_UnknownFromStatus(t *testing.T) {
	cases := []struct {
		name string
		from store.SEVStatus
		to   store.SEVStatus
	}{
		{"empty string", store.SEVStatus(""), store.SEVStatusOpen},
		{"bogus value", store.SEVStatus("bogus"), store.SEVStatusInvestigating},
		{"closed (not a real status)", store.SEVStatus("closed"), store.SEVStatusOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sev.ValidateTransition(tc.from, tc.to)
			if err == nil {
				t.Errorf("ValidateTransition(%q → %q) = nil, want ErrInvalidTransition", tc.from, tc.to)
				return
			}
			if !errors.Is(err, sev.ErrInvalidTransition) {
				t.Errorf("ValidateTransition(%q → %q) = %v, want ErrInvalidTransition", tc.from, tc.to, err)
			}
		})
	}
}
