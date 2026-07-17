package auth

// Login / refresh / logout (FR-28.2, FR-29). Response shapes are byte-for-byte
// the Phase-1 C7 stub (access_token, refresh_token, manager{id,username,role})
// so the panel client (Agent E) needs no change.

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5"
)

type loginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
	// TOTPCode carries the 2FA code (or a backup code) on the second login
	// submission when the account has TOTP enabled (FR-28.1).
	TOTPCode string `json:"totp_code"`
}

type managerBrief struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type tokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	Manager      managerBrief `json:"manager"`
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	ip, ua := clientIP(r), r.UserAgent()

	if locked, retry := svc.limiter.lockState(ctx, req.Username, ip); locked {
		_ = AuditActor(ctx, "", ip, ua, "auth.lockout", "manager_username", req.Username, nil, nil)
		w.Header().Set("Retry-After", retrySeconds(retry))
		httpapi.Error(w, http.StatusTooManyRequests, "too_many_attempts",
			"too many failed attempts; try again later")
		return
	}

	m, err := lookupManagerByUsername(ctx, svc.db, req.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		svc.log.Error("login lookup failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if m == nil {
		// Unknown user: count the failure to blunt user enumeration + brute force.
		svc.limiter.recordFailure(ctx, req.Username, ip)
		_ = AuditActor(ctx, "", ip, ua, "auth.login_failed", "manager_username", req.Username, nil, nil)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}

	matched, needsUpgrade, verr := verifyPassword(m.PasswordHash, req.Password)
	if verr != nil {
		svc.log.Error("password verify failed", "error", verr, "manager", m.ID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !matched {
		svc.limiter.recordFailure(ctx, req.Username, ip)
		_ = AuditActor(ctx, "", ip, ua, "auth.login_failed", "manager", m.ID, nil, nil)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}

	// Password OK. A disabled account is refused before anything is issued;
	// saying so openly is fine — the caller already proved the password.
	if m.Disabled {
		_ = AuditActor(ctx, m.ID, ip, ua, "auth.login_disabled", "manager", m.ID, nil, nil)
		httpapi.Error(w, http.StatusForbidden, "account_disabled", "this account is disabled")
		return
	}

	// IP allowlist (FR-30, AC-30a) is enforced before anything is
	// issued: a correct password from a disallowed network is still refused.
	allow, err := allowlistCIDRs(ctx, svc.db, m.ID)
	if err != nil {
		svc.log.Error("allowlist lookup failed", "error", err, "manager", m.ID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !ipAllowed(ip, allow) {
		_ = AuditActor(ctx, m.ID, ip, ua, "auth.ip_denied", "manager", m.ID, nil, ipDeniedDetail{IP: ip})
		httpapi.Error(w, http.StatusForbidden, "ip_not_allowed", "your network is not permitted for this account")
		return
	}

	// 2FA (FR-28.1). An enrolled account must present a valid TOTP/backup code;
	// a required-but-not-enrolled account gets a limited enrolment grant instead
	// of a session.
	required := twoFactorRequired(ctx, m)
	switch {
	case m.TOTPEnabled:
		if req.TOTPCode == "" {
			_ = AuditActor(ctx, m.ID, ip, ua, "auth.totp_required", "manager", m.ID, nil, nil)
			httpapi.Error(w, http.StatusUnauthorized, "totp_required", "a two-factor code is required")
			return
		}
		ok, terr := verifyManagerSecondFactor(ctx, m, req.TOTPCode)
		if terr != nil {
			svc.log.Error("totp verify failed", "error", terr, "manager", m.ID)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		if !ok {
			svc.limiter.recordFailure(ctx, req.Username, ip)
			_ = AuditActor(ctx, m.ID, ip, ua, "auth.totp_failed", "manager", m.ID, nil, nil)
			httpapi.Error(w, http.StatusUnauthorized, "invalid_totp", "invalid two-factor code")
			return
		}
	case required:
		// Enrolment mandated but not yet done: hand back an enrolment grant.
		enroll, eerr := svc.tokens.issueEnroll(m.ID, time.Now())
		if eerr != nil {
			svc.log.Error("issue enroll token failed", "error", eerr, "manager", m.ID)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		svc.limiter.reset(ctx, req.Username)
		_ = AuditActor(ctx, m.ID, ip, ua, "auth.totp_enroll_required", "manager", m.ID, nil, nil)
		httpapi.JSON(w, http.StatusOK, map[string]any{
			"totp_enrollment_required": true,
			"enrollment_token":         enroll,
		})
		return
	}

	// Transparent hash upgrade (bcrypt seed → argon2id, or a params bump) —
	// best effort; a failure here must not block the login.
	if needsUpgrade {
		if newHash, herr := hashPassword(req.Password); herr == nil {
			if uerr := updatePasswordHash(ctx, svc.db, m.ID, newHash); uerr != nil {
				svc.log.Warn("password hash upgrade failed", "error", uerr, "manager", m.ID)
			}
		}
	}
	svc.limiter.reset(ctx, req.Username)

	resp, err := issueSession(ctx, m, ip, ua)
	if err != nil {
		svc.log.Error("issue session failed", "error", err, "manager", m.ID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = AuditActor(ctx, m.ID, ip, ua, "auth.login", "manager", m.ID, nil, nil)
	httpapi.JSON(w, http.StatusOK, resp)
}

// twoFactorRequired reports whether this manager must have 2FA (the global
// security.require_2fa setting or their role's require_2fa flag).
func twoFactorRequired(ctx context.Context, m *managerAuthRow) bool {
	if m.RoleRequire2FA {
		return true
	}
	if svc.settings == nil {
		return false
	}
	req, err := platform.Get[bool](ctx, svc.settings, settingRequire2FA)
	if err != nil {
		return false
	}
	return req
}

// issueSession resolves the manager's effective permissions + allowlist,
// creates the panel_sessions row and mints the token pair with both embedded.
func issueSession(ctx context.Context, m *managerAuthRow, ip, ua string) (tokenResponse, error) {
	perms, err := resolvePermissions(ctx, svc.db, m.ID)
	if err != nil {
		return tokenResponse{}, err
	}
	allow, err := allowlistCIDRs(ctx, svc.db, m.ID)
	if err != nil {
		return tokenResponse{}, err
	}
	secret, hash, err := newRefreshSecret()
	if err != nil {
		return tokenResponse{}, err
	}
	sessionID, err := createSession(ctx, svc.db, m.ID, hash, ua, ip)
	if err != nil {
		return tokenResponse{}, err
	}
	access, err := svc.tokens.issueAccess(accessClaims{
		ManagerID: m.ID, Role: m.Role, Scoped: m.Scoped, SessionID: sessionID,
		Perms: perms, AllowedIPs: allow,
	}, time.Now())
	if err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{
		AccessToken:  access,
		RefreshToken: composeRefreshToken(sessionID, secret),
		Manager:      managerBrief{ID: m.ID, Username: m.Username, Role: m.Role},
	}, nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	ip, ua := clientIP(r), r.UserAgent()

	sessionID, secret, ok := parseRefreshToken(req.RefreshToken)
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "invalid refresh token")
		return
	}
	managerID, storedHash, revoked, err := getSessionForRefresh(ctx, svc.db, sessionID)
	if err != nil {
		// Not found (or bad uuid) → invalid; never reveal which.
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "invalid refresh token")
		return
	}
	if revoked {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	if subtle.ConstantTimeCompare(hashRefresh(secret), storedHash) != 1 {
		// A live session presented a rotated-away secret → token theft.
		// Revoke the whole session chain and audit it (FR-29).
		_, _ = revokeSession(ctx, svc.db, sessionID, "")
		_ = AuditActor(ctx, managerID, ip, ua, "auth.refresh_reuse", "panel_session", sessionID, nil, nil)
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}

	// Rotate: new secret, update the row (also verifies still-not-revoked).
	newSecret, newHash, err := newRefreshSecret()
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	rotated, err := rotateSession(ctx, svc.db, sessionID, newHash, ip, ua)
	if err != nil {
		svc.log.Error("rotate session failed", "error", err, "session", sessionID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !rotated {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}

	mgr, err := lookupManagerByID(ctx, svc.db, managerID)
	if err != nil {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	// Disable revokes sessions, but a refresh racing the revocation must not
	// resurrect one.
	if mgr.Disabled {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_token", "session is no longer valid")
		return
	}
	// Re-resolve perms + allowlist on every refresh so a role/override/allowlist
	// edit takes effect within one access-token lifetime (FR-27, FR-30).
	perms, err := resolvePermissions(ctx, svc.db, mgr.ID)
	if err != nil {
		svc.log.Error("resolve permissions failed", "error", err, "manager", mgr.ID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	allow, err := allowlistCIDRs(ctx, svc.db, mgr.ID)
	if err != nil {
		svc.log.Error("allowlist lookup failed", "error", err, "manager", mgr.ID)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	access, err := svc.tokens.issueAccess(accessClaims{
		ManagerID: mgr.ID, Role: mgr.Role, Scoped: mgr.Scoped, SessionID: sessionID,
		Perms: perms, AllowedIPs: allow,
	}, time.Now())
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: composeRefreshToken(sessionID, newSecret),
		Manager:      managerBrief{ID: mgr.ID, Username: mgr.Username, Role: mgr.Role},
	})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	m, ok := ManagerFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	if m.SessionID != "" {
		if _, err := revokeSession(r.Context(), svc.db, m.SessionID, m.ID); err != nil {
			svc.log.Error("logout revoke failed", "error", err, "session", m.SessionID)
		}
	}
	_ = Audit(r.Context(), "auth.logout", "panel_session", m.SessionID, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func retrySeconds(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
