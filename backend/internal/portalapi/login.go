package portalapi

// Portal login/refresh/logout (contract C2 FR-41.1). Response shape is
// frozen exactly by F's already-written client (frontend/portal/src/api/auth.ts):
// {access_token, refresh_token, subscriber:{id,username,name,language}}.
//
// Edge case (task brief): a disabled subscriber may still log in — read-only
// access so they can see their status and pay to reactivate; only the
// renewal endpoints reflect what policy actually allows. Login therefore
// never gates on subscriber status.

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/subscribers"
)

type loginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type subscriberBrief struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Language string `json:"language"`
}

type tokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	Subscriber   subscriberBrief `json:"subscriber"`
}

func (m *Module) loginHandler(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	ip, ua := clientIP(r), r.UserAgent()

	if locked, retry := m.limiter.lockState(ctx, in.Username, ip); locked {
		w.Header().Set("Retry-After", retrySeconds(retry))
		httpapi.Error(w, http.StatusTooManyRequests, "rate_limited",
			"too many failed attempts; try again in "+retrySeconds(retry)+" seconds")
		return
	}

	id, ok, err := subscribers.VerifyPassword(ctx, m.db, in.Username, in.Password)
	if err != nil {
		m.internalError(w, "login verify", err)
		return
	}
	if !ok {
		m.limiter.recordFailure(ctx, in.Username, ip)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	m.limiter.reset(ctx, in.Username)

	resp, err := m.issueSession(ctx, id, ip, ua)
	if err != nil {
		m.internalError(w, "issue session", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

func (m *Module) issueSession(ctx context.Context, id subscribers.PortalIdentity, ip, ua string) (tokenResponse, error) {
	secret, hash, err := newRefreshSecret()
	if err != nil {
		return tokenResponse{}, err
	}
	sessionID, err := createSession(ctx, m.db, id.ID, hash, ip, ua)
	if err != nil {
		return tokenResponse{}, err
	}
	access, err := issueAccess(m.jwtSecret, id.ID, sessionID, time.Now())
	if err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{
		AccessToken:  access,
		RefreshToken: composeRefreshToken(sessionID, secret),
		Subscriber:   subscriberBrief{ID: id.ID, Username: id.Username, Name: id.Name, Language: id.Language},
	}, nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (m *Module) refreshHandler(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	ip, ua := clientIP(r), r.UserAgent()

	sessionID, secret, ok := parseRefreshToken(in.RefreshToken)
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "invalid refresh token")
		return
	}
	subscriberID, storedHash, revoked, err := getSessionForRefresh(ctx, m.db, sessionID)
	if err != nil {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "invalid refresh token")
		return
	}
	if revoked {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	if subtle.ConstantTimeCompare(hashRefresh(secret), storedHash) != 1 {
		// A live session presenting a rotated-away secret is treated as token
		// theft: revoke the whole session immediately (same discipline as auth).
		_ = revokeSession(ctx, m.db, sessionID)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}

	newSecret, newHash, err := newRefreshSecret()
	if err != nil {
		m.internalError(w, "refresh", err)
		return
	}
	rotated, err := rotateSession(ctx, m.db, sessionID, newHash, ip, ua)
	if err != nil {
		m.internalError(w, "refresh rotate", err)
		return
	}
	if !rotated {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}

	id, err := subscribers.LoadIdentity(ctx, m.db, subscriberID)
	if err != nil {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	access, err := issueAccess(m.jwtSecret, subscriberID, sessionID, time.Now())
	if err != nil {
		m.internalError(w, "refresh issue", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: composeRefreshToken(sessionID, newSecret),
		Subscriber:   subscriberBrief{ID: id.ID, Username: id.Username, Name: id.Name, Language: id.Language},
	})
}

func (m *Module) logoutHandler(w http.ResponseWriter, r *http.Request) {
	sub, ok := SubscriberFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	if sub.SessionID != "" {
		if err := revokeSession(r.Context(), m.db, sub.SessionID); err != nil {
			m.log.Error("portalapi: logout revoke failed", "error", err, "session", sub.SessionID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func retrySeconds(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
