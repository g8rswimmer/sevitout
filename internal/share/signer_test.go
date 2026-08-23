package share_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/share"
)

func TestSigner_SignAndValidate(t *testing.T) {
	s := share.NewSigner("test-secret")

	tok, err := s.Sign("SEV-2026-0001", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sevID, err := s.Validate(tok)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if sevID != "SEV-2026-0001" {
		t.Errorf("sevID = %q, want SEV-2026-0001", sevID)
	}
}

func TestSigner_Validate_Expired(t *testing.T) {
	s := share.NewSigner("test-secret")

	tok, err := s.Sign("SEV-2026-0001", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if _, err := s.Validate(tok); err != share.ErrTokenExpired {
		t.Errorf("want ErrTokenExpired, got %v", err)
	}
}

func TestSigner_Validate_Tampered(t *testing.T) {
	s := share.NewSigner("test-secret")

	tok, err := s.Sign("SEV-2026-0001", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	tampered := tok[:len(tok)-1] + "x"
	if tampered == tok {
		t.Fatal("tamper produced identical token")
	}

	if _, err := s.Validate(tampered); err != share.ErrTokenInvalid {
		t.Errorf("want ErrTokenInvalid, got %v", err)
	}
}

func TestSigner_Validate_WrongSecret(t *testing.T) {
	signed := share.NewSigner("secret-a")
	other := share.NewSigner("secret-b")

	tok, err := signed.Sign("SEV-2026-0001", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if _, err := other.Validate(tok); err != share.ErrTokenInvalid {
		t.Errorf("want ErrTokenInvalid, got %v", err)
	}
}

func TestSigner_Validate_Garbage(t *testing.T) {
	s := share.NewSigner("test-secret")
	if _, err := s.Validate("not-a-token"); err != share.ErrTokenInvalid {
		t.Errorf("want ErrTokenInvalid, got %v", err)
	}
}
