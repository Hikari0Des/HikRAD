package setupapi

// GET/POST /api/v1/license, POST /api/v1/license/request-blob (contract C4,
// FR-50). The signature-verify/fingerprint/grace-machine logic all lives in
// internal/platform/license (pure) and internal/platform (DB persistence +
// cache); this file is only the HTTP shape + auth.Audit calls.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
)

type licenseResponse struct {
	Installed         bool       `json:"installed"`
	State             string     `json:"state"`
	KeyID             string     `json:"key_id,omitempty"`
	Licensee          string     `json:"licensee,omitempty"`
	Tier              string     `json:"tier,omitempty"`
	MaxSubscribers    int        `json:"max_subscribers,omitempty"`
	EntitledVersion   string     `json:"entitled_version,omitempty"`
	IssuedFingerprint string     `json:"issued_fingerprint,omitempty"`
	Fingerprint       string     `json:"fingerprint"` // this server's current fingerprint
	GraceStartedAt    *time.Time `json:"grace_started_at,omitempty"`
	GraceExpiresAt    *time.Time `json:"grace_expires_at,omitempty"`
}

func currentFingerprintOrEmpty(log func(msg string, args ...any)) string {
	fp, err := license.Current()
	if err != nil {
		log("license: fingerprint unavailable", "error", err)
		return ""
	}
	return fp
}

func buildLicenseResponse(rec license.Record, installed bool, currentFP string) licenseResponse {
	resp := licenseResponse{
		Installed:   installed,
		State:       string(license.StateValid),
		Fingerprint: currentFP,
	}
	if !installed {
		return resp
	}
	resp.State = string(rec.State)
	resp.KeyID = rec.KeyID
	resp.Licensee = rec.Payload.Licensee
	resp.Tier = rec.Payload.Tier
	resp.MaxSubscribers = rec.Payload.MaxSubscribers
	resp.EntitledVersion = rec.Payload.EntitledVersion
	resp.IssuedFingerprint = rec.IssuedFingerprint
	resp.GraceStartedAt = rec.GraceStartedAt
	if exp := rec.GraceExpiresAt(); !exp.IsZero() {
		resp.GraceExpiresAt = &exp
	}
	return resp
}

func getLicenseHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, ok, err := platform.LoadLicenseRecord(ctx, svc.db)
	if err != nil {
		svc.log.Error("get license failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	fp := currentFingerprintOrEmpty(svc.log.Warn)
	httpapi.JSON(w, http.StatusOK, buildLicenseResponse(rec, ok, fp))
}

// uploadLicenseHandler validates and installs a new license blob (FR-50.1,
// AC-50a: purely offline — Verify never makes a network call). It never
// force-marks the upload valid: license.Evaluate runs against the live
// fingerprint immediately, so uploading a key issued for a different machine
// still correctly lands in grace (re-issue-for-this-machine is what clears
// it, matching AC-50b).
func uploadLicenseHandler(w http.ResponseWriter, r *http.Request) {
	installLicense(w, r, "license.upload")
}

func installLicense(w http.ResponseWriter, r *http.Request, auditAction string) {
	blob, ok := decodeLicenseBlob(w, r)
	if !ok {
		return
	}
	payload, err := license.Verify(license.ProductionPublicKey, blob)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "invalid_license", "license signature verification failed")
		return
	}
	ctx := r.Context()

	// Save with a neutral State (Valid, no grace timer) and let
	// RefreshLicenseCache do the actual Evaluate-against-live-fingerprint
	// pass immediately after. This is deliberate, not just convenient: it
	// means "just uploaded a key for a different machine" and "the nightly
	// re-evaluation noticed a mismatch" go through the exact same
	// before-vs-after comparison that raises the grace alert exactly once —
	// pre-computing the state here and saving it directly would make this
	// upload path silently skip the alert (found by testing the full upload
	// flow against a live server, not by inspection).
	rec := license.Record{
		KeyID:             payload.KeyID,
		Payload:           payload,
		IssuedFingerprint: payload.Fingerprint,
		State:             license.StateValid,
	}
	if err := platform.SaveLicenseRecord(ctx, svc.db, rec, blob.Signature); err != nil {
		svc.log.Error("save license failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	platform.RefreshLicenseCache(ctx, svc.db, svc.log)

	updated, _, err := platform.LoadLicenseRecord(ctx, svc.db)
	if err != nil {
		svc.log.Error("reload license after save failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	fp := currentFingerprintOrEmpty(svc.log.Warn)
	_ = auth.Audit(ctx, auditAction, "license", updated.KeyID, nil, map[string]any{
		"licensee": payload.Licensee, "key_id": payload.KeyID, "state": updated.State,
	})
	httpapi.JSON(w, http.StatusOK, buildLicenseResponse(updated, true, fp))
}

// decodeLicenseBlob preserves the payload's exact wire bytes as
// json.RawMessage — Ed25519 signs those exact bytes, so decoding into a
// generic map and re-marshaling (which alphabetizes keys) would break every
// signature. Bind's json.Decoder feeds json.RawMessage its raw slice
// verbatim via UnmarshalJSON, which is exactly what license.Verify needs.
func decodeLicenseBlob(w http.ResponseWriter, r *http.Request) (license.Blob, bool) {
	var req struct {
		Payload   json.RawMessage `json:"payload" validate:"required"`
		Signature string          `json:"signature" validate:"required"`
	}
	if !httpapi.Bind(w, r, &req) {
		return license.Blob{}, false
	}
	return license.Blob{Payload: req.Payload, Signature: req.Signature}, true
}

type requestBlobResponse struct {
	CurrentKeyID string `json:"current_key_id,omitempty"`
	Fingerprint  string `json:"fingerprint"`
	RequestedAt  string `json:"requested_at"`
}

// requestBlobHandler produces the FR-50.4 re-issue request: old key id (if
// any) + the new fingerprint, for the admin to send the vendor by whatever
// offline channel they have (email, support ticket, USB). No phone-home.
func requestBlobHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, ok, err := platform.LoadLicenseRecord(ctx, svc.db)
	if err != nil {
		svc.log.Error("load license for request-blob failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	resp := requestBlobResponse{
		Fingerprint: currentFingerprintOrEmpty(svc.log.Warn),
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if ok {
		resp.CurrentKeyID = rec.KeyID
	}
	_ = auth.Audit(ctx, "license.request_blob", "license", resp.CurrentKeyID, nil, resp)
	httpapi.JSON(w, http.StatusOK, resp)
}
