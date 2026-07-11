package auth

// Token model (FR-52.2, FR-29):
//   - Access token: short-lived HS256 JWT carrying sub/role/scoped/sid. It is
//     verified statelessly on every request (no DB hit — keeps the auth
//     overhead inside B's 100 ms budget). Revocation therefore takes effect
//     within one access-token lifetime (≤ accessTTL), which is the FR-29 SLA.
//   - Refresh token: opaque `<sessionID>.<secret>`. Only sha256(secret) is
//     stored (panel_sessions.refresh_hash) and it rotates on every use.
//     Presenting a rotated-away secret for a still-live session is treated as
//     token theft and revokes the whole session (login.go).

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
	// tokenTypeAccess matches httpapi's stub token type value so any consumer
	// still keyed on "access" keeps working.
	tokenTypeAccess = "access"
	// tokenTypeEnroll is a limited grant issued when login succeeds but 2FA
	// enrolment is required (FR-28.1): it authorizes only the TOTP enroll/verify
	// endpoints, nothing else.
	tokenTypeEnroll = "enroll"

	accessTTL        = 5 * time.Minute  // FR-29: revocation SLA ≤ one access lifetime
	enrollTTL        = 10 * time.Minute // window to complete forced enrolment
	refreshSecretLen = 32
)

var errBadToken = errors.New("auth: invalid token")

// accessClaims is what an access token carries beyond the registered claims.
// Perms is the manager's fully-resolved effective permission set (FR-27) and
// AllowedIPs the resolved CIDR allowlist (FR-30); both are embedded at
// login/refresh so authorization and allowlist enforcement need no DB round-trip.
type accessClaims struct {
	ManagerID  string
	Role       string
	Scoped     bool
	SessionID  string
	Perms      []string
	AllowedIPs []string
}

type tokenService struct {
	secret []byte
}

func newTokenService(secret []byte) *tokenService { return &tokenService{secret: secret} }

// issueAccess signs a short-lived access JWT for the session.
func (t *tokenService) issueAccess(c accessClaims, now time.Time) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    c.ManagerID,
		"role":   c.Role,
		"scoped": c.Scoped,
		"sid":    c.SessionID,
		"perms":  c.Perms,
		"ips":    c.AllowedIPs,
		"typ":    tokenTypeAccess,
		"iat":    now.Unix(),
		"exp":    now.Add(accessTTL).Unix(),
	})
	return tok.SignedString(t.secret)
}

// issueEnroll signs a limited enrolment-grant JWT (FR-28.1). It carries only
// the manager id and is accepted solely by the TOTP enroll/verify endpoints.
func (t *tokenService) issueEnroll(managerID string, now time.Time) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": managerID,
		"typ": tokenTypeEnroll,
		"iat": now.Unix(),
		"exp": now.Add(enrollTTL).Unix(),
	})
	return tok.SignedString(t.secret)
}

// parseEnroll validates an enrolment-grant token and returns the manager id.
func (t *tokenService) parseEnroll(raw string) (string, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}), jwt.WithExpirationRequired())
	if err != nil {
		return "", err
	}
	if typ, _ := claims["typ"].(string); typ != tokenTypeEnroll {
		return "", errBadToken
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", errBadToken
	}
	return sub, nil
}

// parseAccess validates signature, method, expiry and type, returning the
// claims. It never touches the database.
func (t *tokenService) parseAccess(raw string) (accessClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}), jwt.WithExpirationRequired())
	if err != nil {
		return accessClaims{}, err
	}
	if typ, _ := claims["typ"].(string); typ != tokenTypeAccess {
		return accessClaims{}, errBadToken
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return accessClaims{}, errBadToken
	}
	role, _ := claims["role"].(string)
	scoped, _ := claims["scoped"].(bool)
	sid, _ := claims["sid"].(string)
	return accessClaims{
		ManagerID:  sub,
		Role:       role,
		Scoped:     scoped,
		SessionID:  sid,
		Perms:      stringSlice(claims["perms"]),
		AllowedIPs: stringSlice(claims["ips"]),
	}, nil
}

// stringSlice coerces a JWT array claim (decoded as []any) to []string.
func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// newRefreshSecret generates a fresh refresh secret and its stored hash.
func newRefreshSecret() (secret string, hash []byte, err error) {
	buf := make([]byte, refreshSecretLen)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, hashRefresh(secret), nil
}

// composeRefreshToken builds the opaque token handed to the client.
func composeRefreshToken(sessionID, secret string) string {
	return sessionID + "." + secret
}

// parseRefreshToken splits a refresh token into its session id and secret.
func parseRefreshToken(tok string) (sessionID, secret string, ok bool) {
	sessionID, secret, ok = strings.Cut(tok, ".")
	if !ok || sessionID == "" || secret == "" {
		return "", "", false
	}
	return sessionID, secret, true
}

// hashRefresh is the sha256 of a refresh secret stored in panel_sessions.
func hashRefresh(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
