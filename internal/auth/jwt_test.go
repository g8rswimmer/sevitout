package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/g8rswimmer/sevitout/internal/auth"
)

func TestJWTSigner_Sign_Validate(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-32-chars-long-enough", 24)

	tokenStr, err := signer.Sign("user-1", "alice@example.com", "responder")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	claims, err := signer.Validate(tokenStr)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q, want %q", claims.Subject, "user-1")
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("email = %q, want %q", claims.Email, "alice@example.com")
	}
	if claims.OrgRole != "responder" {
		t.Errorf("role = %q, want %q", claims.OrgRole, "responder")
	}
}

func TestJWTSigner_Validate_Expired(t *testing.T) {
	// Sign with a past expiry by manually crafting the claims.
	secret := "test-secret-32-chars-long-enough"
	signer := auth.NewJWTSigner(secret, 24)

	// Build an already-expired token.
	past := time.Now().Add(-time.Hour)
	raw := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(past.Add(-time.Hour)),
			ExpiresAt: jwt.NewNumericDate(past),
		},
		Email:   "alice@example.com",
		OrgRole: "viewer",
	})
	tokenStr, err := raw.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	_, err = signer.Validate(tokenStr)
	if err != auth.ErrTokenExpired {
		t.Errorf("Validate expired token = %v, want ErrTokenExpired", err)
	}
}

func TestJWTSigner_Validate_Tampered(t *testing.T) {
	signer := auth.NewJWTSigner("test-secret-32-chars-long-enough", 24)

	tokenStr, err := signer.Sign("user-1", "alice@example.com", "viewer")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Flip the token's first character rather than its last: the JWT's
	// three segments (header/payload/signature) are each base64url-encoded
	// independently, and the *last* character of a segment can have unused
	// low-order bits when the segment's byte length isn't a multiple of 3
	// — replacing only that character has a real (~1-in-16, confirmed
	// empirically) chance of decoding to the exact same bytes, making the
	// "tampered" token spuriously valid and this test flaky. The first
	// character of a segment has no such ambiguity: it always encodes the
	// top bits of the first byte, so changing it always changes the
	// decoded content.
	runes := []rune(tokenStr)
	if runes[0] == 'X' {
		runes[0] = 'Y'
	} else {
		runes[0] = 'X'
	}
	tampered := string(runes)

	_, err = signer.Validate(tampered)
	if err != auth.ErrTokenInvalid {
		t.Errorf("Validate tampered token = %v, want ErrTokenInvalid", err)
	}
}

func TestJWTSigner_Validate_WrongSecret(t *testing.T) {
	signer1 := auth.NewJWTSigner("secret-one-32-chars-long-enough!", 24)
	signer2 := auth.NewJWTSigner("secret-two-32-chars-long-enough!", 24)

	tokenStr, _ := signer1.Sign("user-1", "alice@example.com", "viewer")

	_, err := signer2.Validate(tokenStr)
	if err != auth.ErrTokenInvalid {
		t.Errorf("Validate wrong-secret token = %v, want ErrTokenInvalid", err)
	}
}
