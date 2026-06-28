package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by JWTSigner.Validate.
var (
	ErrTokenExpired = errors.New("auth: token expired")
	ErrTokenInvalid = errors.New("auth: token invalid")
)

// Claims are the payload fields issued in every JWT.
type Claims struct {
	jwt.RegisteredClaims
	Email   string `json:"email"`
	OrgRole string `json:"role"`
}

// JWTSigner signs and validates HS256 JWTs.
type JWTSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTSigner constructs a signer using the given HMAC secret and token TTL.
// If ttlHours is zero, 24 hours is used.
func NewJWTSigner(secret string, ttlHours int) *JWTSigner {
	ttl := time.Duration(ttlHours) * time.Hour
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	return &JWTSigner{secret: []byte(secret), ttl: ttl}
}

// Sign creates and signs a JWT for the given user.
func (s *JWTSigner) Sign(userID, email, orgRole string) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
		Email:   email,
		OrgRole: orgRole,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

// Validate parses and validates a JWT, returning its claims on success.
func (s *JWTSigner) Validate(tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}
