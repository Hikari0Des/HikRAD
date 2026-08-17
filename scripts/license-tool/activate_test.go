package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildRequest mirrors license.NewActivationRequest for test fixtures:
// hashing components the same way the server would, then computing the
// checksum over the zeroed-Checksum encoding.
func buildRequest(t *testing.T, machineID, mac string) activationRequest {
	t.Helper()
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:16]
	}
	req := activationRequest{
		SchemaVersion:  1,
		MachineIDHash:  hash(machineID),
		MACHash:        hash(mac),
		Fingerprint:    hash(machineID) + ":" + hash(mac),
		CurrentKeyID:   "K-OLD",
		CurrentSerial:  "SN-1",
		ProductVersion: "1.5.0",
		GeneratedAt:    time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	req.Checksum = hex.EncodeToString(sum[:])
	return req
}

func writeRequestFile(t *testing.T, req activationRequest) string {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "activation-request.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyChecksum_AcceptsWellFormedRequest(t *testing.T) {
	req := buildRequest(t, "machine-a", "aa:bb:cc:dd:ee:ff")
	if !verifyChecksum(req) {
		t.Fatal("verifyChecksum rejected a well-formed request")
	}
}

func TestVerifyChecksum_RejectsTamperedFingerprint(t *testing.T) {
	req := buildRequest(t, "machine-a", "aa:bb:cc:dd:ee:ff")
	req.Fingerprint = "hand-edited:value"
	if verifyChecksum(req) {
		t.Fatal("verifyChecksum accepted a request whose fingerprint was edited")
	}
}

// TestCmdActivate_SignsAgainstRequestFingerprint_NeverAnOperatorTypedValue is
// the FR-123 gate-item-3 proof at the tool level: even if the flag-driven
// path is invoked with signing details, the fingerprint always comes from
// the request file, never from anything else — there is no -fingerprint flag
// on `activate` at all, so this test also documents that by construction.
func TestCmdActivate_SignsAgainstRequestFingerprint(t *testing.T) {
	keyFile, pub := writeTempKey(t)
	req := buildRequest(t, "machine-a", "aa:bb:cc:dd:ee:ff")
	reqPath := writeRequestFile(t, req)
	outPath := filepath.Join(t.TempDir(), "license.json")

	err := cmdActivate([]string{
		"-request", reqPath,
		"-key", keyFile, "-keyid", "K-NEW", "-licensee", "Acme ISP",
		"-tier", "pro", "-max-subscribers", "1000",
		"-entitlements", "white_label,self_service",
		"-out", outPath,
	})
	if err != nil {
		t.Fatalf("cmdActivate: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var b blob
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(b.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, b.Payload, sig) {
		t.Fatal("activated license does not verify")
	}
	var p payload
	if err := json.Unmarshal(b.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Fingerprint != req.Fingerprint {
		t.Fatalf("signed fingerprint = %q, want exactly the request file's %q", p.Fingerprint, req.Fingerprint)
	}
	if p.Version != 2 || len(p.Entitlements) != 2 {
		t.Fatalf("v2 fields not set: %+v", p)
	}
}

func TestCmdActivate_RefusesTamperedRequest(t *testing.T) {
	keyFile, _ := writeTempKey(t)
	req := buildRequest(t, "machine-a", "aa:bb:cc:dd:ee:ff")
	req.Fingerprint = "tampered:value" // edited after the checksum was computed
	reqPath := writeRequestFile(t, req)

	err := cmdActivate([]string{
		"-request", reqPath,
		"-key", keyFile, "-keyid", "K-NEW", "-licensee", "Acme ISP",
		"-tier", "pro", "-max-subscribers", "1000",
	})
	if err == nil {
		t.Fatal("cmdActivate signed a license from a request that failed its checksum check")
	}
}

func TestCmdActivate_RequiresRequestFlag(t *testing.T) {
	if err := cmdActivate(nil); err == nil {
		t.Fatal("cmdActivate accepted no -request flag")
	}
}
