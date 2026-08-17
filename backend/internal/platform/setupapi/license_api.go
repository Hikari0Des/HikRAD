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

	// FR-122/FR-125: payload schema version actually installed (1 or 2), the
	// resolved entitlement set (v2 explicit, or the v1-tier-derived fallback
	// — never the raw payload field verbatim for a v1 blob, so the panel
	// never has to know the difference), calendar expiry/support window, and
	// serial. Entitlements is built the same way platform.LicenseHasEntitlement
	// resolves it — the display never has a second, potentially-diverging
	// notion of what's granted.
	Version      int        `json:"version,omitempty"`
	Entitlements []string   `json:"entitlements,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	SupportUntil *time.Time `json:"support_until,omitempty"`
	Serial       string     `json:"serial,omitempty"`

	// FR-104: which fingerprint components match between the issued key and
	// this server right now, so the UI can name what drifted for a re-issue.
	// Nil until a license is installed and this server's fingerprint is known.
	FingerprintMatch *fingerprintMatch `json:"fingerprint_match,omitempty"`

	// FR-105: live subscriber count vs the licensed cap, so the panel can show
	// a persistent renew/upgrade prompt in the warn band. Present only when the
	// license carries a finite cap.
	SubscriberCount *int `json:"subscriber_count,omitempty"`
	OverCap         bool `json:"over_cap,omitempty"`
	CapBlocked      bool `json:"cap_blocked,omitempty"`
}

// fingerprintMatch is the per-component drift view behind WithinTolerance.
type fingerprintMatch struct {
	MachineIDMatch  bool `json:"machine_id_match"`
	MACMatch        bool `json:"mac_match"`
	WithinTolerance bool `json:"within_tolerance"`
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
	resp.Version = rec.Payload.EffectiveVersion()
	for _, name := range license.KnownEntitlements {
		if rec.Payload.HasEntitlement(name) {
			resp.Entitlements = append(resp.Entitlements, name)
		}
	}
	resp.ExpiresAt = rec.Payload.ExpiresAt
	resp.SupportUntil = rec.Payload.SupportUntil
	resp.Serial = rec.Payload.Serial
	if currentFP != "" && rec.IssuedFingerprint != "" {
		mid, mac := license.CompareComponents(rec.IssuedFingerprint, currentFP)
		resp.FingerprintMatch = &fingerprintMatch{
			MachineIDMatch:  mid,
			MACMatch:        mac,
			WithinTolerance: license.WithinTolerance(rec.IssuedFingerprint, currentFP),
		}
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
	resp := buildLicenseResponse(rec, ok, fp)
	// FR-105: attach the live count vs cap so the panel can show a persistent
	// renew/upgrade prompt. Only when the installed license has a finite cap.
	if ok && platform.CachedLicenseMaxSubscribers() > 0 {
		var n int
		if err := svc.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscribers`).Scan(&n); err != nil {
			svc.log.Warn("license: subscriber count for cap status failed", "error", err)
		} else {
			st := platform.EvalSubscriberCap(n)
			resp.SubscriberCount = &n
			resp.OverCap = st.Over
			resp.CapBlocked = st.Blocked
		}
	}
	httpapi.JSON(w, http.StatusOK, resp)
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

// requestBlobResponse is the FR-123 activation-request file: this server's
// live fingerprint (both hashed halves, plus the composed string kept for
// wire-compatibility with the field name the FR-50.4 flow already used),
// the currently installed license's key id/serial if any, this build's
// version, and a checksum license-tool verifies before trusting the file.
// Field names/shapes are additive over the pre-v4-3 response — a re-issue
// request built against an old server build still round-trips.
type requestBlobResponse struct {
	CurrentKeyID   string `json:"current_key_id,omitempty"`
	CurrentSerial  string `json:"current_serial,omitempty"`
	Fingerprint    string `json:"fingerprint"`
	MachineIDHash  string `json:"machine_id_hash"`
	MACHash        string `json:"mac_hash"`
	ProductVersion string `json:"product_version"`
	RequestedAt    string `json:"requested_at"`
	Checksum       string `json:"checksum"`
}

// requestBlobHandler produces the FR-123 activation request: this server's
// OWN current fingerprint (never a value the operator typed), so
// license-tool's `activate` command signs the resulting license against
// exactly this file's Fingerprint — the file exchange that makes "signed
// against a stale fingerprint" structurally impossible, replacing the old
// hand-transcription trap. No phone-home: this only builds and returns a
// local file, no outbound network call is made.
func requestBlobHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, ok, err := platform.LoadLicenseRecord(ctx, svc.db)
	if err != nil {
		svc.log.Error("load license for request-blob failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	machineID, macAddr, fpErr := license.CurrentComponents()
	if fpErr != nil {
		svc.log.Warn("license: fingerprint unavailable for activation request", "error", fpErr)
	}
	var currentKeyID, currentSerial string
	if ok {
		currentKeyID = rec.KeyID
		currentSerial = rec.Payload.Serial
	}
	req := license.NewActivationRequest(machineID, macAddr, currentKeyID, currentSerial, appVersion, time.Now().UTC())
	resp := requestBlobResponse{
		CurrentKeyID:   req.CurrentKeyID,
		CurrentSerial:  req.CurrentSerial,
		Fingerprint:    req.Fingerprint,
		MachineIDHash:  req.MachineIDHash,
		MACHash:        req.MACHash,
		ProductVersion: req.ProductVersion,
		RequestedAt:    req.GeneratedAt.Format(time.RFC3339),
		Checksum:       req.Checksum,
	}
	_ = auth.Audit(ctx, "license.request_blob", "license", resp.CurrentKeyID, nil, resp)
	httpapi.JSON(w, http.StatusOK, resp)
}
