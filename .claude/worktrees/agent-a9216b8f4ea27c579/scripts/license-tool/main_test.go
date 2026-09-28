package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTempKey generates a vendor keypair, writes the private key (base64,
// exactly as `license-tool keygen` does) to a temp file, and returns the path
// plus the public key for signature verification.
func writeTempKey(t *testing.T) (keyFile string, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyFile = filepath.Join(t.TempDir(), "priv.b64")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return keyFile, pub
}

func runSign(t *testing.T, in issueInput) []byte {
	t.Helper()
	in.out = filepath.Join(t.TempDir(), "lic.json")
	if err := signAndWrite(in); err != nil {
		t.Fatalf("signAndWrite: %v", err)
	}
	b, err := os.ReadFile(in.out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return b
}

// TestSignAndWrite_VerifiesAndDecodes asserts the emitted blob authenticates
// under the vendor public key and decodes to the given fields — i.e. the
// tool/verifier wire-format contract holds.
func TestSignAndWrite_VerifiesAndDecodes(t *testing.T) {
	keyFile, pub := writeTempKey(t)
	in := issueInput{
		keyFile: keyFile, keyID: "K-2026-0001", licensee: "Acme ISP",
		tier: "pro", maxSubs: 1000, version: "1", fingerprint: "abc123:def456",
		issuedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
	}
	out := runSign(t, in)

	var b blob
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(b.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if !ed25519.Verify(pub, b.Payload, sig) {
		t.Fatal("signature does not verify over the exact payload bytes")
	}
	var p payload
	if err := json.Unmarshal(b.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Tier != "pro" || p.MaxSubscribers != 1000 || p.Fingerprint != "abc123:def456" {
		t.Fatalf("payload fields wrong: %+v", p)
	}
}

// TestSignAndWrite_ByteIdenticalAcrossPaths is the FR-103 hard invariant: the
// flag path and the interactive path both funnel identical fields into
// signAndWrite, so identical inputs (including issuedAt) must produce
// byte-identical output. Two independent issueInput values standing in for the
// two entry points must serialize to the same bytes.
func TestSignAndWrite_ByteIdenticalAcrossPaths(t *testing.T) {
	keyFile, _ := writeTempKey(t)
	issued := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

	fromFlags := issueInput{
		keyFile: keyFile, keyID: "K-1", licensee: "ISP", tier: "standard",
		maxSubs: 500, version: "1", fingerprint: "fp1:fp2", issuedAt: issued,
	}
	fromInteractive := issueInput{
		keyFile: keyFile, keyID: "K-1", licensee: "ISP", tier: "standard",
		maxSubs: 500, version: "1", fingerprint: "fp1:fp2", issuedAt: issued,
	}
	a := runSign(t, fromFlags)
	b := runSign(t, fromInteractive)
	if string(a) != string(b) {
		t.Fatalf("flag vs interactive output differ:\n flags: %s\n inter: %s", a, b)
	}
}

// TestSignAndWrite_UnlimitedTier confirms max-subscribers 0 (the unlimited
// tier) is accepted and round-trips as 0.
func TestSignAndWrite_UnlimitedTier(t *testing.T) {
	keyFile, _ := writeTempKey(t)
	in := issueInput{
		keyFile: keyFile, keyID: "K-U", licensee: "Big ISP", tier: "unlimited",
		maxSubs: 0, version: "1", fingerprint: "x:y",
		issuedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
	}
	out := runSign(t, in)
	var b blob
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var p payload
	if err := json.Unmarshal(b.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.MaxSubscribers != 0 || p.Tier != "unlimited" {
		t.Fatalf("unlimited tier wrong: %+v", p)
	}
}

// TestSignAndWrite_V1UnaffectedByV2Fields confirms an issueInput with no v2
// fields set (the existing CI call-site shape) still emits a payload with no
// "version"/"entitlements" keys at all — the FR-122 wire-compat guarantee.
func TestSignAndWrite_V1UnaffectedByV2Fields(t *testing.T) {
	keyFile, _ := writeTempKey(t)
	in := issueInput{
		keyFile: keyFile, keyID: "K-1", licensee: "ISP", tier: "standard",
		maxSubs: 500, version: "1", fingerprint: "fp1:fp2",
		issuedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
	}
	out := runSign(t, in)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	var payloadRaw map[string]json.RawMessage
	if err := json.Unmarshal(raw["payload"], &payloadRaw); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	for _, key := range []string{"version", "entitlements", "expires_at", "support_until", "serial"} {
		if _, present := payloadRaw[key]; present {
			t.Errorf("v1-shaped issueInput emitted a %q key — must be fully absent, not just zero", key)
		}
	}
}

// TestSignAndWrite_V2EntitlementsRoundTrip covers the new activation path:
// setting entitlements (or expiry/support/serial) switches the payload to v2
// and every field round-trips.
func TestSignAndWrite_V2EntitlementsRoundTrip(t *testing.T) {
	keyFile, pub := writeTempKey(t)
	exp := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	in := issueInput{
		keyFile: keyFile, keyID: "K-2", licensee: "Acme ISP", tier: "pro",
		maxSubs: 1000, version: "1", fingerprint: "fp1:fp2",
		issuedAt:     time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
		entitlements: []string{"white_label", "self_service"},
		expiresAt:    &exp,
		serial:       "SN-777",
	}
	out := runSign(t, in)

	var b blob
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(b.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if !ed25519.Verify(pub, b.Payload, sig) {
		t.Fatal("signature does not verify over v2 payload bytes")
	}
	var p payload
	if err := json.Unmarshal(b.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Version != 2 {
		t.Errorf("Version = %d, want 2", p.Version)
	}
	if len(p.Entitlements) != 2 || p.Serial != "SN-777" {
		t.Errorf("v2 fields wrong: %+v", p)
	}
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt wrong: %+v", p.ExpiresAt)
	}
}

func TestTierCatalog(t *testing.T) {
	want := map[string]int{"starter": 200, "standard": 500, "pro": 1000, "unlimited": 0}
	if len(tierCatalog) != len(want) {
		t.Fatalf("catalog size %d, want %d", len(tierCatalog), len(want))
	}
	for _, tr := range tierCatalog {
		w, ok := want[tr.Key]
		if !ok {
			t.Fatalf("unexpected tier %q", tr.Key)
		}
		if tr.Max != w {
			t.Fatalf("tier %q max=%d, want %d", tr.Key, tr.Max, w)
		}
	}
}
