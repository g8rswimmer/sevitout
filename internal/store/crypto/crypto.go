// Package crypto provides application-layer AES-256-GCM encryption for
// secrets that must never reach PostgreSQL as plaintext (integration
// credentials, AI provider API keys, etc.). See docs/architecture.md §11
// ("Credential encryption") and §12.2.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// KeySize is the required key length for AES-256: 32 bytes.
const KeySize = 32

// ErrInvalidKeySize is returned when a key is not exactly KeySize bytes.
var ErrInvalidKeySize = fmt.Errorf("crypto: key must be %d bytes", KeySize)

// Encrypt seals plaintext with AES-256-GCM under key, returning
// nonce||ciphertext||tag. A fresh random nonce is generated for every call,
// so encrypting the same plaintext twice yields different output.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}
	// Seal appends the ciphertext to the (empty) destination slice we pass,
	// so prefixing nonce here is the standard idiom for a single self
	// contained blob: nonce || ciphertext || tag.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a blob produced by Encrypt. It fails if key is wrong or the
// ciphertext (including the nonce or authentication tag) has been altered.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}

// DecodeKey base64-decodes a 32-byte AES-256 key (the format ENCRYPTION_KEY
// is provided in) and validates its length.
func DecodeKey(base64Key string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode key: %w", err)
	}
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	return key, nil
}

// KeyEncryptor binds Encrypt/Decrypt to a fixed key, so callers that need an
// object (rather than two free functions plus a key) can pass one around —
// e.g. internal/api/grpc.ConfigServer's Encryptor dependency.
type KeyEncryptor struct {
	key []byte
}

// NewKeyEncryptor returns a KeyEncryptor using key, which must be KeySize bytes.
func NewKeyEncryptor(key []byte) *KeyEncryptor {
	return &KeyEncryptor{key: key}
}

func (k *KeyEncryptor) Encrypt(plaintext []byte) ([]byte, error) { return Encrypt(plaintext, k.key) }
func (k *KeyEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	return Decrypt(ciphertext, k.key)
}
