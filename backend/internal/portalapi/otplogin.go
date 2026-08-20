package portalapi

// OTP login for passwordless hotspot accounts (FR-172), closing the
// known-issues row dated 2026-07-16: a subscriber created with
// has_password=false (v1.1's no_password flag, or FR-169's own guest
// registration) cannot use VerifyPassword's username/password path at all
// — "" can never match "" there by design, so without this file such an
// account had NO portal login path whatsoever. Reuses FR-169's passcode
// machinery (passcodes.go) exactly as the phase brief instructs; the only
// new logic here is resolving username -> the eligible phone number.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/subscribers"
	"github.com/jackc/pgx/v5"
)

var errOTPNotEligible = errors.New("portalapi: account is not eligible for OTP login")

// otpEligiblePhone returns the subscriber id + phone for a username that IS
// a passwordless account with a phone on file, or errOTPNotEligible for
// everything else (unknown username, has a password, or no phone) — one
// generic outcome so this endpoint cannot be used to enumerate which
// usernames exist or which have passwords set.
func otpEligiblePhone(ctx context.Context, m *Module, username string) (subscriberID, phone string, err error) {
	var hasPassword bool
	err = m.db.QueryRow(ctx,
		`SELECT id::text, COALESCE(phone,''), has_password FROM subscribers WHERE username = $1`,
		username).Scan(&subscriberID, &phone, &hasPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errOTPNotEligible
	}
	if err != nil {
		return "", "", err
	}
	if hasPassword || phone == "" {
		return "", "", errOTPNotEligible
	}
	return subscriberID, phone, nil
}

type otpRequestRequest struct {
	Username string `json:"username" validate:"required"`
	MAC      string `json:"mac,omitempty"`
}

func (m *Module) otpRequestHandler(w http.ResponseWriter, r *http.Request) {
	var in otpRequestRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	subID, phone, err := otpEligiblePhone(ctx, m, strings.TrimSpace(in.Username))
	if err != nil {
		if errors.Is(err, errOTPNotEligible) {
			// Same shape as any other refusal here — never confirms or
			// denies the username exists.
			httpapi.Error(w, http.StatusUnauthorized, "otp_not_available", "otp login is not available for this account")
			return
		}
		m.internalError(w, "otp eligibility", err)
		return
	}
	id, err := subscribers.LoadIdentity(ctx, m.db, subID)
	if err != nil {
		m.internalError(w, "otp load identity", err)
		return
	}
	perr := issuePasscode(ctx, m, passcodePurposeOTPLogin, phone, subID, in.MAC, "", id.Language)
	writePasscodeSendResult(w, perr)
}

type otpVerifyRequest struct {
	Username string `json:"username" validate:"required"`
	Code     string `json:"code" validate:"required"`
	MAC      string `json:"mac,omitempty"`
}

func (m *Module) otpVerifyHandler(w http.ResponseWriter, r *http.Request) {
	var in otpVerifyRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	ctx := r.Context()
	subID, phone, err := otpEligiblePhone(ctx, m, strings.TrimSpace(in.Username))
	if err != nil {
		if errors.Is(err, errOTPNotEligible) {
			httpapi.Error(w, http.StatusUnauthorized, "otp_not_available", "otp login is not available for this account")
			return
		}
		m.internalError(w, "otp eligibility", err)
		return
	}
	_, ok, err := verifyPasscode(ctx, m, passcodePurposeOTPLogin, phone, in.MAC, strings.TrimSpace(in.Code))
	if err != nil {
		m.internalError(w, "otp verify", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "invalid_code", "invalid or expired code")
		return
	}
	id, err := subscribers.LoadIdentity(ctx, m.db, subID)
	if err != nil {
		m.internalError(w, "otp load identity", err)
		return
	}
	resp, err := m.issueSession(ctx, id, clientIP(r), r.UserAgent())
	if err != nil {
		m.internalError(w, "otp issue session", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
