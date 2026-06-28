package postmortem_test

import (
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/postmortem"
)

func TestUnlockSigner_RoundTrip(t *testing.T) {
	signer := postmortem.NewUnlockSigner("test-secret-at-least-32-chars-long")
	sevID := "SEV-2026-0001"

	token, err := signer.Sign(sevID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if err := signer.Validate(token, sevID); err != nil {
		t.Errorf("Validate: unexpected error: %v", err)
	}
}

func TestUnlockSigner_WrongSEVID(t *testing.T) {
	signer := postmortem.NewUnlockSigner("test-secret-at-least-32-chars-long")

	token, err := signer.Sign("SEV-2026-0001")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	err = signer.Validate(token, "SEV-2026-9999")
	if !errors.Is(err, postmortem.ErrUnlockTokenSEVMismatch) {
		t.Errorf("expected ErrUnlockTokenSEVMismatch, got %v", err)
	}
}

func TestUnlockSigner_TamperedToken(t *testing.T) {
	signer := postmortem.NewUnlockSigner("test-secret-at-least-32-chars-long")

	err := signer.Validate("not.a.valid.jwt", "SEV-2026-0001")
	if !errors.Is(err, postmortem.ErrUnlockTokenInvalid) {
		t.Errorf("expected ErrUnlockTokenInvalid, got %v", err)
	}
}

func TestUnlockSigner_WrongSecret(t *testing.T) {
	signer1 := postmortem.NewUnlockSigner("secret-one-padded-to-32-chars-xyz")
	signer2 := postmortem.NewUnlockSigner("secret-two-padded-to-32-chars-xyz")

	token, err := signer1.Sign("SEV-2026-0001")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	err = signer2.Validate(token, "SEV-2026-0001")
	if !errors.Is(err, postmortem.ErrUnlockTokenInvalid) {
		t.Errorf("expected ErrUnlockTokenInvalid, got %v", err)
	}
}
