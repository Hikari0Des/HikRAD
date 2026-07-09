package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Identity is the authenticated caller attached to the request context.
type Identity struct {
	ManagerID string
	Role      string
}

// Authenticator is the injectable authentication seam. Phase 1 installs the
// signature-check-only JWTAuthenticator below; in Phase 2 Agent 1 (Platform &
// Security) swaps in the real implementation (permissions, revocation) via
// SetAuthenticator without touching this package's files.
type Authenticator interface {
	Authenticate(r *http.Request) (Identity, error)
}

var authn Authenticator

// SetAuthenticator installs the process-wide Authenticator used by
// RequireAuth. Call before serving requests.
func SetAuthenticator(a Authenticator) { authn = a }

type identityCtxKey struct{}

// IdentityFrom returns the Identity RequireAuth stored on the context.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(Identity)
	return id, ok
}

// RequireAuth guards a route with the installed Authenticator, writing a C2
// 401 envelope on failure.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authn == nil {
			Error(w, http.StatusUnauthorized, "unauthorized", "authentication is not configured")
			return
		}
		id, err := authn.Authenticate(r)
		if err != nil {
			Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityCtxKey{}, id)))
	})
}

// JWTAuthenticator is the Phase-1 auth stub: it verifies only the HMAC
// signature, expiry and token type of `Authorization: Bearer <token>` —
// no permission checks (those arrive in Phase 2 from Agent 1).
type JWTAuthenticator struct {
	Secret []byte
}

func (a JWTAuthenticator) Authenticate(r *http.Request) (Identity, error) {
	header := r.Header.Get("Authorization")
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || raw == "" {
		return Identity{}, errors.New("missing bearer token")
	}
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return a.Secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}), jwt.WithExpirationRequired())
	if err != nil {
		return Identity{}, err
	}
	if typ, _ := claims["typ"].(string); typ != TokenTypeAccess {
		return Identity{}, errors.New("not an access token")
	}
	sub, _ := claims["sub"].(string)
	role, _ := claims["role"].(string)
	if sub == "" {
		return Identity{}, errors.New("token has no subject")
	}
	return Identity{ManagerID: sub, Role: role}, nil
}
