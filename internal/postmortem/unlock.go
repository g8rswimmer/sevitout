package postmortem

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by UnlockSigner.Validate.
var (
	ErrUnlockTokenExpired     = errors.New("postmortem: unlock token expired")
	ErrUnlockTokenInvalid     = errors.New("postmortem: unlock token invalid")
	ErrUnlockTokenSEVMismatch = errors.New("postmortem: unlock token SEV ID mismatch")
)

type unlockClaims struct {
	jwt.RegisteredClaims
	SEVID string `json:"sev_id"`
	Scope string `json:"scope"`
}

// UnlockSigner creates and validates short-lived JWTs that authorize a single
// write session on a locked SEV. Tokens are scoped to a specific SEV ID and
// expire after 15 minutes.
type UnlockSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewUnlockSigner returns an UnlockSigner using the given HMAC secret.
func NewUnlockSigner(secret string) *UnlockSigner {
	return &UnlockSigner{
		secret: []byte(secret),
		ttl:    15 * time.Minute,
	}
}

// Sign creates a short-lived unlock token scoped to sevID.
func (s *UnlockSigner) Sign(sevID string) (string, error) {
	now := time.Now()
	claims := unlockClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
		SEVID: sevID,
		Scope: "unlock",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

// Validate checks that tokenStr is a valid, non-expired unlock token for sevID.
func (s *UnlockSigner) Validate(tokenStr, sevID string) error {
	t, err := jwt.ParseWithClaims(tokenStr, &unlockClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return ErrUnlockTokenExpired
		}
		return ErrUnlockTokenInvalid
	}
	claims, ok := t.Claims.(*unlockClaims)
	if !ok || !t.Valid {
		return ErrUnlockTokenInvalid
	}
	if claims.Scope != "unlock" {
		return ErrUnlockTokenInvalid
	}
	if claims.SEVID != sevID {
		return ErrUnlockTokenSEVMismatch
	}
	return nil
}
