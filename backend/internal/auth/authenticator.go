package auth

// httpAuthenticator adapts the auth token service to httpapi.Authenticator,
// the Phase-1 injection seam RequireAuth uses. Installing it (SetAuthenticator,
// done in Configure) replaces the Phase-1 signature-only JWT stub, so routes
// still guarded by httpapi.RequireAuth (the Phase-1 subscribers/profiles list
// endpoints) validate real access tokens. Permission/scoping-aware routes use
// auth.Require instead.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type httpAuthenticator struct {
	tokens *tokenService
}

func (a httpAuthenticator) Authenticate(r *http.Request) (httpapi.Identity, error) {
	raw, ok := bearerToken(r)
	if !ok {
		return httpapi.Identity{}, errBadToken
	}
	claims, err := a.tokens.parseAccess(raw)
	if err != nil {
		return httpapi.Identity{}, err
	}
	return httpapi.Identity{ManagerID: claims.ManagerID, Role: claims.Role}, nil
}
