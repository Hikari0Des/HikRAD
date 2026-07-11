package auth

// TOTP 2FA endpoints (C7 / FR-28.1): self-service enroll → verify-activate →
// disable, plus admin reset of a locked-out manager. Enrolment and verification
// accept either a full access token or a limited enrolment grant (issued by
// login when 2FA is required but not yet set up).

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

type enrollOnlyKeyT struct{}

// enrollOrAccess authenticates a request bearing either a normal access token
// or an enrolment grant, storing a Manager in context. Enrolment grants are
// flagged so the (few) endpoints that accept them can behave accordingly.
func enrollOrAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "authentication is not configured")
			return
		}
		if m, err := managerFromRequest(r); err == nil {
			ctx := withManager(r.Context(), m)
			if !ipAllowed(m.IP, m.AllowedIPs) {
				httpapi.Error(w, http.StatusForbidden, "ip_not_allowed", "your network is not permitted for this account")
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		raw, ok := bearerToken(r)
		if !ok {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		managerID, err := svc.tokens.parseEnroll(raw)
		if err != nil {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
			return
		}
		m := &Manager{ID: managerID, IP: clientIP(r), UA: r.UserAgent()}
		ctx := context.WithValue(withManager(r.Context(), m), enrollOnlyKeyT{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type enrollResponse struct {
	OTPAuthURI string `json:"otpauth_uri"`
	Secret     string `json:"secret"`
}

func enrollTOTPHandler(w http.ResponseWriter, r *http.Request) {
	m, _ := ManagerFrom(r.Context())
	ctx := r.Context()

	row, err := lookupManagerByID(ctx, svc.db, m.ID)
	if err != nil {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	if row.TOTPEnabled {
		httpapi.Error(w, http.StatusConflict, "conflict", "two-factor is already enabled; disable it first")
		return
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		svc.log.Error("totp secret gen failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if err := storePendingSecret(ctx, svc.db, m.ID, secret); err != nil {
		svc.log.Error("store pending secret failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(ctx, "auth.totp_enroll_started", "manager", m.ID, nil, nil)
	httpapi.JSON(w, http.StatusOK, enrollResponse{
		OTPAuthURI: otpauthURI(row.Username, secret),
		Secret:     secret,
	})
}

type verifyTOTPRequest struct {
	Code string `json:"code" validate:"required"`
}

func verifyTOTPHandler(w http.ResponseWriter, r *http.Request) {
	m, _ := ManagerFrom(r.Context())
	ctx := r.Context()
	var req verifyTOTPRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}

	_, pending, err := loadTOTPSecrets(ctx, svc.db, m.ID)
	if err != nil {
		svc.log.Error("load totp secrets failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if len(pending) == 0 {
		httpapi.Error(w, http.StatusConflict, "conflict", "no pending enrolment; start enrolment first")
		return
	}
	secret, err := decodeSecretForCheck(pending)
	if err != nil {
		svc.log.Error("decrypt pending secret failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !verifyTOTP(secret, req.Code, time.Now()) {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_totp", "invalid two-factor code")
		return
	}

	codes, err := generateBackupCodes()
	if err != nil {
		svc.log.Error("backup code gen failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	hashes := make([][]byte, len(codes))
	for i, c := range codes {
		hashes[i] = hashBackupCode(c)
	}
	if err := activatePendingSecret(ctx, svc.db, m.ID, hashes, m.SessionID); err != nil {
		if errors.Is(err, errNoPendingEnrolment) {
			httpapi.Error(w, http.StatusConflict, "conflict", "no pending enrolment; start enrolment first")
			return
		}
		svc.log.Error("activate totp failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(ctx, "auth.totp_enabled", "manager", m.ID, nil, nil)
	// Backup codes are shown exactly once, at activation.
	httpapi.JSON(w, http.StatusOK, map[string]any{"backup_codes": codes})
}

type disableTOTPRequest struct {
	Password string `json:"password" validate:"required"`
	Code     string `json:"code" validate:"required"`
}

func disableTOTPHandler(w http.ResponseWriter, r *http.Request) {
	m, _ := ManagerFrom(r.Context())
	ctx := r.Context()
	var req disableTOTPRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	row, err := lookupManagerByID(ctx, svc.db, m.ID)
	if err != nil {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or missing credentials")
		return
	}
	if !row.TOTPEnabled {
		httpapi.Error(w, http.StatusConflict, "conflict", "two-factor is not enabled")
		return
	}
	matched, _, verr := verifyPassword(row.PasswordHash, req.Password)
	if verr != nil || !matched {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid password")
		return
	}
	ok, terr := verifyManagerSecondFactor(ctx, row, req.Code)
	if terr != nil {
		svc.log.Error("second factor check failed", "error", terr)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_totp", "invalid two-factor code")
		return
	}
	if err := disableTOTP(ctx, svc.db, m.ID); err != nil {
		svc.log.Error("disable totp failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	// Disabling 2FA revokes other sessions (FR-29).
	if err := revokeOtherSessions(ctx, svc.db, m.ID, m.SessionID); err != nil {
		svc.log.Error("revoke sessions on totp disable failed", "error", err)
	}
	_ = Audit(ctx, "auth.totp_disabled", "manager", m.ID, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// resetManagerTOTPHandler lets an admin clear a manager's 2FA (FR-28.1: reset a
// locked-out manager). Audited; revokes the target's sessions.
func resetManagerTOTPHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	ctx := r.Context()
	if _, err := lookupManagerByID(ctx, svc.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if err := disableTOTP(ctx, svc.db, id); err != nil {
		svc.log.Error("reset totp failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if err := revokeOtherSessions(ctx, svc.db, id, ""); err != nil {
		svc.log.Error("revoke sessions on totp reset failed", "error", err)
	}
	_ = Audit(ctx, "managers.totp_reset", "manager", id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
