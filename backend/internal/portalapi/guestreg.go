package portalapi

// Self-service guest registration (FR-169, migration 0632). Visitor connects
// on the hotspot → taps Register → enters name + mobile → receives a
// passcode → enters it → gets a real, named account. Every abuse control
// (one live passcode per number, cooldown, send-time validity, per-MAC
// recording, entitlement-gated daily cap) lives in passcodes.go, shared with
// FR-172's OTP login — this file is just the two HTTP handlers and the
// account-creation step that follows a verified code.
//
// Both endpoints are unauthenticated by construction: a visitor registering
// has no token yet. mac is a hint from the hotspot redirect — recorded for
// abuse tracking, never used to authenticate anything by itself (that is
// exactly FR-167/FR-168's job, a completely separate path).

import (
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/subscribers"
)

type registerRequest struct {
	Name     string `json:"name" validate:"required"`
	Phone    string `json:"phone" validate:"required"`
	MAC      string `json:"mac,omitempty"`
	Language string `json:"language,omitempty"`
}

func (m *Module) registerHandler(w http.ResponseWriter, r *http.Request) {
	var in registerRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	phone := normalizePhone(in.Phone)
	locale := normalizeLocale(in.Language)

	err := issuePasscode(r.Context(), m, passcodePurposeRegister, phone, "", in.MAC, strings.TrimSpace(in.Name), locale)
	writePasscodeSendResult(w, err)
}

type registerVerifyRequest struct {
	Phone string `json:"phone" validate:"required"`
	Code  string `json:"code" validate:"required"`
	MAC   string `json:"mac,omitempty"`
}

func (m *Module) registerVerifyHandler(w http.ResponseWriter, r *http.Request) {
	var in registerVerifyRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	phone := normalizePhone(in.Phone)

	name, ok, err := verifyPasscode(ctx, m, passcodePurposeRegister, phone, in.MAC, strings.TrimSpace(in.Code))
	if err != nil {
		m.internalError(w, "register verify", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_code", "invalid or expired code")
		return
	}

	cfg := loadGuestRegistrationConfig(ctx, m.settings)
	if cfg.DefaultProfileID == "" {
		// Setup-order prerequisite (phase brief §3.4): a messaging channel,
		// the feature toggle, AND a default plan must all be configured
		// before this can hand out a real account — never guess a plan.
		httpapi.Error(w, http.StatusServiceUnavailable, "not_configured",
			"guest registration has no default plan configured")
		return
	}

	subID, err := subscribers.CreateGuestAccount(ctx, m.db, phone, name, cfg.DefaultProfileID)
	if err != nil {
		m.internalError(w, "create guest account", err)
		return
	}
	_ = auth.AuditActor(ctx, subID, clientIP(r), r.UserAgent(), "portal.guest_register", "subscriber", subID, nil,
		map[string]any{"phone": phone, "name": name})
	id, err := subscribers.LoadIdentity(ctx, m.db, subID)
	if err != nil {
		m.internalError(w, "load guest identity", err)
		return
	}
	resp, err := m.issueSession(ctx, id, clientIP(r), r.UserAgent())
	if err != nil {
		m.internalError(w, "issue guest session", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}

// normalizePhone strips everything but leading '+' and digits — the same
// identifier is used as both the passcode key and the new account's
// username, so "+964 750 123 4567" and "0750-123-4567" typed differently by
// the same visitor twice must resolve to the identical row.
func normalizePhone(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for i, r := range s {
		if r == '+' && i == 0 {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeLocale(s string) string {
	switch s {
	case "ar", "ku":
		return s
	default:
		return "en"
	}
}

func writePasscodeSendResult(w http.ResponseWriter, err error) {
	switch err {
	case nil:
		httpapi.JSON(w, http.StatusOK, map[string]any{"sent": true})
	case errPasscodeAlreadyLive:
		// Not a failure — the frontend advances to the "enter your code"
		// screen exactly as if a fresh code had just gone out (FR-169: "says
		// 'check your messages' rather than sending another").
		httpapi.JSON(w, http.StatusOK, map[string]any{"sent": true, "already_sent": true})
	case errPasscodeCooldown:
		httpapi.Error(w, http.StatusTooManyRequests, "cooldown", "please wait before requesting another code")
	case errPasscodeDailyCapReached:
		httpapi.Error(w, http.StatusTooManyRequests, "daily_cap_reached", "daily passcode volume limit reached")
	case errPasscodeFeatureDisabled:
		httpapi.Error(w, http.StatusNotFound, "not_enabled", "this feature is not enabled")
	case errPasscodeNotLicensed:
		httpapi.Error(w, http.StatusForbidden, "not_licensed", "this feature is not included in the current license")
	case errPasscodeNotConfigured:
		httpapi.Error(w, http.StatusServiceUnavailable, "channel_not_configured", "no messaging channel is configured")
	default:
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
