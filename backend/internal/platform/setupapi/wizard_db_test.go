package setupapi

// DB-backed suite for the first-run wizard + license upload flow (gated on
// HIKRAD_TEST_DB_URL, matching the repo pattern). Unlike the other DB-backed
// suites in this repo (which share one DB and never truncate, since they
// only assert about rows they themselves created), "setup only works before
// any admin exists" fundamentally needs a managers table with zero rows —
// something no other suite can promise on a shared database. So this file
// creates and drops its own throwaway database per run instead.
//
// It also locks in a real bug found only by manually driving this exact
// flow against a live server: license.Verify was fine, but installLicense
// used to pre-compute the grace transition and save it directly, which
// skipped RefreshLicenseCache's alert-raising comparison entirely — a
// license upload for a mismatched fingerprint silently entered grace with
// no alert_events row. See license_api.go's installLicense comment.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/jackc/pgx/v5/pgxpool"
)

type wizardEnv struct {
	srv *httptest.Server
	db  *pgxpool.Pool
}

func setupWizardEnv(t *testing.T) wizardEnv {
	t.Helper()
	base := os.Getenv("HIKRAD_TEST_DB_URL")
	if base == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping setupapi wizard DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse HIKRAD_TEST_DB_URL: %v", err)
	}
	dbName := fmt.Sprintf("hikrad_setupapi_test_%s", uniqSuffix())

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		admin.Close()
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+dbName+`" WITH (FORCE)`)
	})

	u2 := *u
	u2.Path = "/" + dbName
	dbURL := u2.String()

	if err := platform.Migrate(dbURL, "../../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(db.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	t.Setenv("HIKRAD_DB_URL", dbURL)
	t.Setenv("HIKRAD_REDIS_URL", "redis://unused:6379/0")
	t.Setenv("HIKRAD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("HIKRAD_JWT_SECRET", "setupapi-wizard-test-secret")
	t.Setenv("HIKRAD_ENV", "dev")

	deps := httpapi.Deps{DB: db, Settings: platform.NewSettings(db), Log: log}
	srv := httptest.NewServer(httpapi.NewRouter(deps, true))
	t.Cleanup(srv.Close)
	return wizardEnv{srv: srv, db: db}
}

func uniqSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type wizResp struct {
	status int
	body   []byte
}

func (r wizResp) json(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", r.body, err)
	}
}

func wizCall(t *testing.T, e wizardEnv, method, path, token string, body []byte) wizResp {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return wizResp{status: resp.StatusCode, body: raw}
}

// issueTestLicense mirrors scripts/license-tool's signing exactly (compact
// JSON, plain ed25519.Sign over the marshaled bytes) against a throwaway
// keypair substituted for the production one via t.Cleanup restore.
func issueTestLicense(t *testing.T, keyID, licensee, fingerprint string) (blob []byte, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	origPub := license.ProductionPublicKey
	license.ProductionPublicKey = pub
	t.Cleanup(func() { license.ProductionPublicKey = origPub })

	p := license.Payload{
		KeyID: keyID, Licensee: licensee, Tier: "5k", MaxSubscribers: 5000,
		EntitledVersion: "1", Fingerprint: fingerprint,
	}
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

func TestWizardGatingAndLicenseUploadGraceAlert(t *testing.T) {
	e := setupWizardEnv(t)
	ctx := context.Background()

	// 1. Fresh DB: setup is open, no license, no admin.
	r := wizCall(t, e, "GET", "/api/v1/setup/status", "", nil)
	var status setupStatusResponse
	r.json(t, &status)
	if status.AdminExists || status.LicenseInstalled {
		t.Fatalf("fresh DB: status = %+v, want both false", status)
	}

	// 2. Fingerprint is always shown, even pre-admin.
	r = wizCall(t, e, "GET", "/api/v1/setup/license", "", nil)
	if r.status != http.StatusOK {
		t.Fatalf("GET setup/license = %d: %s", r.status, r.body)
	}
	var lic licenseResponse
	r.json(t, &lic)
	if lic.Fingerprint == "" {
		t.Fatal("setup/license did not return a fingerprint")
	}

	// 3. Upload a license issued for THIS server's fingerprint -> valid.
	blob, _ := issueTestLicense(t, "K-TEST-1", "Test ISP", lic.Fingerprint)
	r = wizCall(t, e, "POST", "/api/v1/setup/license", "", blob)
	if r.status != http.StatusOK {
		t.Fatalf("upload matching license: %d: %s", r.status, r.body)
	}
	r.json(t, &lic)
	if lic.State != string(license.StateValid) {
		t.Fatalf("state after matching upload = %q, want valid", lic.State)
	}

	// 4. Create the first admin -> setup routes close.
	r = wizCall(t, e, "POST", "/api/v1/setup/admin", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusCreated {
		t.Fatalf("create admin: %d: %s", r.status, r.body)
	}
	r = wizCall(t, e, "GET", "/api/v1/setup/license", "", nil)
	if r.status != http.StatusForbidden {
		t.Fatalf("setup/license after admin created: status = %d, want 403 setup_complete", r.status)
	}
	r = wizCall(t, e, "POST", "/api/v1/setup/admin", "",
		mustJSON(t, map[string]string{"username": "admin2", "password": "SuperSecret123"}))
	if r.status != http.StatusForbidden {
		t.Fatalf("second setup/admin: status = %d, want 403 (only one admin may be created this way)", r.status)
	}

	// 5. Log in, then upload a license for a DIFFERENT fingerprint via the
	// authenticated endpoint -> grace, and exactly one alert_events row.
	r = wizCall(t, e, "POST", "/api/v1/auth/login", "",
		mustJSON(t, map[string]string{"username": "admin", "password": "SuperSecret123"}))
	if r.status != http.StatusOK {
		t.Fatalf("login: %d: %s", r.status, r.body)
	}
	var login struct {
		AccessToken string `json:"access_token"`
	}
	r.json(t, &login)

	var before int
	_ = e.db.QueryRow(ctx, `SELECT count(*) FROM alert_events WHERE type = 'license_grace'`).Scan(&before)

	mismatchFP := license.Compose("some-other-machine", "de:ad:be:ef:00:00")
	blob2, _ := issueTestLicense(t, "K-TEST-2", "Different ISP", mismatchFP)
	r = wizCall(t, e, "POST", "/api/v1/license", login.AccessToken, blob2)
	if r.status != http.StatusOK {
		t.Fatalf("upload mismatched license: %d: %s", r.status, r.body)
	}
	r.json(t, &lic)
	if lic.State != string(license.StateGrace) {
		t.Fatalf("state after mismatched upload = %q, want grace", lic.State)
	}
	if lic.GraceStartedAt == nil {
		t.Fatal("grace_started_at not set after mismatched upload")
	}

	var after int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM alert_events WHERE type = 'license_grace'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Errorf("alert_events(license_grace) = %d, want %d (exactly one row on grace entry)", after, before+1)
	}

	// 6. A settings write must succeed while merely in grace (only
	// expired_grace makes the panel read-only).
	r = wizCall(t, e, "PUT", "/api/v1/settings/locale", login.AccessToken,
		mustJSON(t, map[string]string{"timezone": "Asia/Baghdad"}))
	if r.status != http.StatusOK {
		t.Fatalf("settings write during grace: status = %d, want 200", r.status)
	}
}

func TestSettingsDataRetentionFloorRejected(t *testing.T) {
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

	r = wizCall(t, e, "PUT", "/api/v1/settings/data_retention", login.AccessToken,
		mustJSON(t, map[string]int{"raw_months": 6}))
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("raw_months=6 (below the 12-month FR-33 floor): status = %d, want 422", r.status)
	}
	if !strings.Contains(string(r.body), "raw_months") {
		t.Errorf("error body doesn't mention the offending field: %s", r.body)
	}

	r = wizCall(t, e, "PUT", "/api/v1/settings/data_retention", login.AccessToken,
		mustJSON(t, map[string]int{"raw_months": 12, "rollup_years": 3}))
	if r.status != http.StatusOK {
		t.Fatalf("at-floor values should be accepted: status = %d: %s", r.status, r.body)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
