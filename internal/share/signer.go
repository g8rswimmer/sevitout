// Package share signs and validates the public shareable-link tokens used by
// docs/requirements.md §14.1: a token that grants read-only access to a
// curated view of one SEV, without login, until it's revoked or expires.
package share

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by Signer.Validate.
var (
	ErrTokenExpired = errors.New("share: token expired")
	ErrTokenInvalid = errors.New("share: token invalid")
)

type claims struct {
	jwt.RegisteredClaims
	SEVID string `json:"sev_id"`
	Scope string `json:"scope"`
}

// Signer creates and validates signed shareable-link tokens. Each token is
// self-contained proof of "SEV ID + expiry, HMAC-SHA256 signed" (§13's
// requirement), matching this repo's existing pattern for scoped, signed
// tokens (postmortem.UnlockSigner) — reusing JWT_SECRET rather than
// introducing a second secret. The token itself never grants access on its
// own: ShareStore's Revoked flag is the authority for early revocation,
// since a signed token can't be un-signed — see ShareServer/the public share
// view handler, which check both.
type Signer struct {
	secret []byte
}

// NewSigner returns a Signer using the given HMAC secret (JWT_SECRET).
func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

// Sign creates a token scoped to sevID that expires at expiresAt.
func (s *Signer) Sign(sevID string, expiresAt time.Time) (string, error) {
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		SEVID: sevID,
		Scope: "share",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString(s.secret)
}

// Validate checks tokenStr's signature and expiry and returns the SEV ID it
// is scoped to. It does not consult ShareStore — callers that need to honor
// revocation (the public share view) must check the stored record's Revoked
// flag separately.
func (s *Signer) Validate(tokenStr string) (string, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", ErrTokenExpired
		}
		return "", ErrTokenInvalid
	}
	c, ok := t.Claims.(*claims)
	if !ok || !t.Valid || c.Scope != "share" {
		return "", ErrTokenInvalid
	}
	return c.SEVID, nil
}
