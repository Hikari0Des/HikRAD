package setupapi

// DB-backed FR-122/FR-123 suite: the licence-v2 file exchange. Reuses
// setupWizardEnv/wizCall/mustJSON (wizard_db_test.go) for the throwaway
// DB + admin token, and issueTestLicenseV2 mirrors issueTestLicense's
// production-key-swap trick (license.ProductionPublicKey is a var, not a
// const, exactly so tests can substitute a throwaway keypair) extended
// with the v2 fields.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform/license"
)

// issueTestLicenseV2 mirrors scripts/license-tool's v2 signing exactly
// (Payload with Version/Entitlements/Serial set, plain ed25519.Sign) against
// a throwaway keypair substituted for the production one via t.Cleanup
// restore, same trick as issueTestLicense.
func issueTestLicenseV2(t *testing.T, p license.Payload) (blob []byte, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	origPub := license.ProductionPublicKey
	license.ProductionPublicKey = pub
	t.Cleanup(func() { license.ProductionPublicKey = origPub })

	b, err := license.Sign(priv, p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return raw, pub
}

// TestActivationFileExchange_LandsValidNeverGrace is FR-123's core proof
// (gate item 3): export the activation request through the real endpoint,
// sign a v2 license against EXACTLY the fingerprint it carries (never a
// hand-typed value), upload it, and confirm the result is "valid"
// immediately, with no grace detour.
func TestActivationFileExchange_LandsValidNeverGrace(t *testing.T) {
	e := setupWizardEnv(t)

	r := wizCall(t, e, "POST", "/api/v1/setup/admin", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusCreated {
		t.Fatalf("create admin: %d: %s", r.status, r.body)
	}
	r = wizCall(t, e, "POST", "/api/v1/auth/login", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	var login struct {
		AccessToken string `json:"access_token"`
	}
	r.json(t, &login)

	// FR-123: export the activation request through the real endpoint —
	// never construct/guess a fingerprint locally.
	r = wizCall(t, e, "POST", "/api/v1/license/request-blob", login.AccessToken, nil)
	if r.status != http.StatusOK {
		t.Fatalf("request-blob: %d: %s", r.status, r.body)
	}
	var actReq struct {
		Fingerprint   string `json:"fingerprint"`
		MachineIDHash string `json:"machine_id_hash"`
		MACHash       string `json:"mac_hash"`
		Checksum      string `json:"checksum"`
	}
	r.json(t, &actReq)
	if actReq.Fingerprint == "" || actReq.Checksum == "" {
		t.Fatalf("activation request missing fingerprint/checksum: %+v", actReq)
	}
	if actReq.Fingerprint != actReq.MachineIDHash+":"+actReq.MACHash {
		t.Fatalf("fingerprint %q != Compose(machine_id_hash, mac_hash) %q:%q",
			actReq.Fingerprint, actReq.MachineIDHash, actReq.MACHash)
	}

	blob, _ := issueTestLicenseV2(t, license.Payload{
		KeyID: "K-ACTIVATE-1", Licensee: "Activated ISP", IssuedAt: time.Now().UTC(),
		Tier: "pro", MaxSubscribers: 1000, EntitledVersion: "1",
		Fingerprint:  actReq.Fingerprint, // exactly the server's own — the whole point of FR-123
		Version:      2,
		Entitlements: []string{license.EntitlementWhiteLabel, license.EntitlementSelfService},
		Serial:       "SN-ACTIVATE-1",
	})
	r = wizCall(t, e, "POST", "/api/v1/license", login.AccessToken, blob)
	if r.status != http.StatusOK {
		t.Fatalf("upload activated license: %d: %s", r.status, r.body)
	}
	var lic licenseResponse
	r.json(t, &lic)
	if lic.State != string(license.StateValid) {
		t.Fatalf("state = %q, want %q — activation against the server's own fingerprint must never land in grace", lic.State, license.StateValid)
	}
	if lic.Licensee != "Activated ISP" {
		t.Fatalf("licensee = %q, want Activated ISP", lic.Licensee)
	}
}

// TestActivationFileExchange_StaleFingerprintStillLandsGrace is the control
// case proving the previous test isn't vacuous: a blob signed against a
// fingerprint that is NOT the server's current one still correctly lands in
// grace — FR-123 makes producing a stale-fingerprint blob by mistake
// structurally hard, it does not remove grace as a concept.
func TestActivationFileExchange_StaleFingerprintStillLandsGrace(t *testing.T) {
	e := setupWizardEnv(t)

	r := wizCall(t, e, "POST", "/api/v1/setup/admin", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusCreated {
		t.Fatalf("create admin: %d: %s", r.status, r.body)
	}
	r = wizCall(t, e, "POST", "/api/v1/auth/login", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	var login struct {
		AccessToken string `json:"access_token"`
	}
	r.json(t, &login)

	stale := license.Compose("stale-machine", "de:ad:be:ef:00:99")
	blob, _ := issueTestLicenseV2(t, license.Payload{
		KeyID: "K-STALE-1", Licensee: "Stale FP ISP", IssuedAt: time.Now().UTC(),
		Tier: "pro", MaxSubscribers: 1000, EntitledVersion: "1",
		Fingerprint: stale, Version: 2,
	})
	r = wizCall(t, e, "POST", "/api/v1/license", login.AccessToken, blob)
	if r.status != http.StatusOK {
		t.Fatalf("upload stale license: %d: %s", r.status, r.body)
	}
	var lic licenseResponse
	r.json(t, &lic)
	if lic.State != string(license.StateGrace) {
		t.Fatalf("state = %q, want %q for a deliberately stale fingerprint", lic.State, license.StateGrace)
	}
}
