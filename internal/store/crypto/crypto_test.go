package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store/crypto"
)

func mustKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := mustKey(t)
	plaintext := []byte(`{"api_key":"pd_super_secret_123"}`)

	ciphertext, err := crypto.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext must not contain the plaintext")
	}

	got, err := crypto.Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncrypt_NonDeterministic(t *testing.T) {
	key := mustKey(t)
	plaintext := []byte("same input")

	c1, err := crypto.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	c2, err := crypto.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(c1, c2) {
		t.Error("encrypting the same plaintext twice should yield different ciphertext (random nonce)")
	}
}

func TestDecrypt_TamperedCiphertextRejected(t *testing.T) {
	key := mustKey(t)
	ciphertext, err := crypto.Encrypt([]byte("hello world"), key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := bytes.Clone(ciphertext)
	tampered[len(tampered)-1] ^= 0xFF // flip the last byte of the auth tag

	if _, err := crypto.Decrypt(tampered, key); err == nil {
		t.Error("Decrypt should reject tampered ciphertext")
	}
}

func TestDecrypt_WrongKeyRejected(t *testing.T) {
	key := mustKey(t)
	wrongKey := mustKey(t)
	ciphertext, err := crypto.Encrypt([]byte("hello world"), key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := crypto.Decrypt(ciphertext, wrongKey); err == nil {
		t.Error("Decrypt should reject the wrong key")
	}
}

func TestEncrypt_InvalidKeySize(t *testing.T) {
	if _, err := crypto.Encrypt([]byte("x"), []byte("too-short")); err != crypto.ErrInvalidKeySize {
		t.Errorf("want ErrInvalidKeySize, got %v", err)
	}
}

func TestDecrypt_InvalidKeySize(t *testing.T) {
	if _, err := crypto.Decrypt([]byte("x"), []byte("too-short")); err != crypto.ErrInvalidKeySize {
		t.Errorf("want ErrInvalidKeySize, got %v", err)
	}
}

func TestDecrypt_TruncatedCiphertext(t *testing.T) {
	key := mustKey(t)
	if _, err := crypto.Decrypt([]byte("short"), key); err == nil {
		t.Error("Decrypt should reject ciphertext shorter than the nonce")
	}
}

func TestDecodeKey(t *testing.T) {
	raw := mustKey(t)
	encoded := base64.StdEncoding.EncodeToString(raw)

	key, err := crypto.DecodeKey(encoded)
	if err != nil {
		t.Fatalf("DecodeKey: %v", err)
	}
	if !bytes.Equal(key, raw) {
		t.Error("DecodeKey did not round-trip the original key bytes")
	}
}

func TestDecodeKey_WrongLength(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("too-short-key"))
	if _, err := crypto.DecodeKey(encoded); err != crypto.ErrInvalidKeySize {
		t.Errorf("want ErrInvalidKeySize, got %v", err)
	}
}

func TestDecodeKey_NotBase64(t *testing.T) {
	if _, err := crypto.DecodeKey("not valid base64!!"); err == nil {
		t.Error("DecodeKey should reject invalid base64")
	}
}
