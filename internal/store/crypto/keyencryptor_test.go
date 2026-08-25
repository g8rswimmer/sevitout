package crypto_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store/crypto"
)

// TestKeyEncryptor_RoundTrip covers the KeyEncryptor wrapper itself — the
// underlying Encrypt/Decrypt free functions are well covered above, but the
// wrapper internal/api/grpc.ConfigServer actually depends on for credential
// encryption (per its own doc comment) had no direct coverage.
func TestKeyEncryptor_RoundTrip(t *testing.T) {
	key := mustKey(t)
	enc := crypto.NewKeyEncryptor(key)
	plaintext := []byte(`{"api_key":"pd_super_secret_123"}`)

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext must not contain the plaintext")
	}

	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestKeyEncryptor_DecryptWrongKeyRejected(t *testing.T) {
	encA := crypto.NewKeyEncryptor(mustKey(t))
	encB := crypto.NewKeyEncryptor(mustKey(t))

	ciphertext, err := encA.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := encB.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt with the wrong key's KeyEncryptor should fail")
	}
}

func TestKeyEncryptor_InvalidKeySize(t *testing.T) {
	enc := crypto.NewKeyEncryptor([]byte("too-short"))

	if _, err := enc.Encrypt([]byte("secret")); !errors.Is(err, crypto.ErrInvalidKeySize) {
		t.Errorf("Encrypt error = %v, want ErrInvalidKeySize", err)
	}
	if _, err := enc.Decrypt([]byte("anything")); !errors.Is(err, crypto.ErrInvalidKeySize) {
		t.Errorf("Decrypt error = %v, want ErrInvalidKeySize", err)
	}
}
