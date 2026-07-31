package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestServeIssue_ProducesVerifiableBlob drives the web issuer's POST /issue
// handler and asserts the signed license it returns authenticates under the
// vendor public key — i.e. the graphical path shares the CLI's signing core and
// the same wire-format contract with the verifier.
func TestServeIssue_ProducesVerifiableBlob(t *testing.T) {
	keyFile, pub := writeTempKey(t)
	srv := &issuerServer{keyFile: keyFile, keyID: "K-2026-0001"}

	form := url.Values{
		"licensee":        {"Acme ISP"},
		"tier":            {"pro"}, // catalog tier → max fixed at 1000
		"max_subscribers": {"999"}, // ignored for a catalog tier
		"version":         {"1"},
		"fingerprint":     {"abc123:def456"},
	}
	req := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleIssue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The signed blob is echoed inside the <textarea>. Pull the base64 download
	// URI instead — it is the exact bytes, unescaped by the HTML template.
	m := regexp.MustCompile(`data:application/json;base64,([A-Za-z0-9+/=]+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no download data URI in response: %s", body)
	}
	enc, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatalf("decode data URI: %v", err)
	}

	var b blob
	if err := json.Unmarshal(enc, &b); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(b.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if !ed25519.Verify(pub, b.Payload, sig) {
		t.Fatal("signature does not verify over the payload bytes")
	}
	var p payload
	if err := json.Unmarshal(b.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	// A catalog tier fixes its own count — the posted 999 must be ignored.
	if p.Tier != "pro" || p.MaxSubscribers != 1000 || p.Licensee != "Acme ISP" {
		t.Fatalf("payload fields wrong: %+v", p)
	}
	if p.Fingerprint != "abc123:def456" {
		t.Fatalf("fingerprint = %q", p.Fingerprint)
	}
}

// TestServeIssue_MissingFieldsShowsError asserts a required-field omission
// re-renders the form with an error rather than signing.
func TestServeIssue_MissingFieldsShowsError(t *testing.T) {
	keyFile, _ := writeTempKey(t)
	srv := &issuerServer{keyFile: keyFile, keyID: "K-1"}

	form := url.Values{"licensee": {"X"}, "tier": {"pro"}} // no fingerprint
	req := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleIssue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (form re-render)", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "data:application/json") {
		t.Fatal("a license was signed despite a missing fingerprint")
	}
	if !strings.Contains(rec.Body.String(), "required") {
		t.Fatalf("expected a validation error in the body")
	}
}

// TestIsLoopbackHost guards the default-safe listener check.
func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "::1", "localhost", ""} {
		if !isLoopbackHost(h) {
			t.Errorf("%q should be loopback", h)
		}
	}
	for _, h := range []string{"0.0.0.0", "192.168.1.10", "example.com"} {
		if isLoopbackHost(h) {
			t.Errorf("%q should NOT be loopback", h)
		}
	}
}
