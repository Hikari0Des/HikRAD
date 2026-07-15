package portalapi

// Portal token model (contract C2, FR-41.1). Deliberately separate from the
// panel's (internal/auth/tokens.go) rather than sharing its private
// tokenService: a distinct "typ" claim value means a portal token fails
// auth.Require's parseAccess (typ mismatch) and a panel token fails
// requireSubscriber below — audience separation falls out of the type check,
// no shared blocklist needed. The signing secret IS shared (HIKRAD_JWT_SECRET,
// same as the panel — see module.go) since it's the same trust boundary, just
// a different token shape.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenTypePortalAccess = "portal_access"
	accessTTL             = 15 * time.Minute // short-lived; theft mitigation (task edge case)
	refreshSecretLen      = 32
)

var errBadToken = errors.New("portalapi: invalid token")

func issueAccess(secret []byte, subscriberID, sessionID string, now time.Time) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": subscriberID,
		"sid": sessionID,
		"typ": tokenTypePortalAccess,
		"iat": now.Unix(),
		"exp": now.Add(accessTTL).Unix(),
	})
	return tok.SignedString(secret)
}

func parseAccess(secret []byte, raw string) (subscriberID, sessionID string, err error) {
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", err
	}
	if typ, _ := claims["typ"].(string); typ != tokenTypePortalAccess {
		return "", "", errBadToken
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", "", errBadToken
	}
	sid, _ := claims["sid"].(string)
	return sub, sid, nil
}

// newRefreshSecret generates a fresh opaque refresh secret and its stored hash.
func newRefreshSecret() (secret string, hash []byte, err error) {
	buf := make([]byte, refreshSecretLen)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, hashRefresh(secret), nil
}

func composeRefreshToken(sessionID, secret string) string { return sessionID + "." + secret }

func parseRefreshToken(tok string) (sessionID, secret string, ok bool) {
	sessionID, secret, ok = strings.Cut(tok, ".")
	if !ok || sessionID == "" || secret == "" {
		return "", "", false
	}
	return sessionID, secret, true
}

func hashRefresh(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
