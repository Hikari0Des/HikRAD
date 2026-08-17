package platform

// DB-backed suite for license persistence (gated on HIKRAD_TEST_DB_URL,
// matching the repo pattern). Exercises the load/save round-trip and
// RefreshLicenseCache's state-transition + grace-alert side effects against
// real Postgres.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/jackc/pgx/v5/pgxpool"
)

// signTestPayload mirrors scripts/license-tool's signing exactly and
// substitutes a throwaway keypair for license.ProductionPublicKey via
// t.Cleanup restore — the same trick internal/platform/setupapi's
// wizard_db_test.go uses. RefreshLicenseCache re-verifies the stored
// signature (FR-124), so a DB test that wants a row to actually be adopted
// into the cache now needs a real signature, not a placeholder string.
func signTestPayload(t *testing.T, p license.Payload) (payloadRaw json.RawMessage, signature string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	origPub := license.ProductionPublicKey
	license.ProductionPublicKey = pub
	t.Cleanup(func() { license.ProductionPublicKey = origPub })

	blob, err := license.Sign(priv, p)
	if err != nil {
		t.Fatal(err)
	}
	return blob.Payload, blob.Signature
}

func setupLicenseTestDB(t *testing.T) (*pgxpool.Pool, *slog.Logger) {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping license DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	// Each test owns the singleton row exclusively via t.Cleanup; tests in
	// this file must not run in parallel with each other (they don't opt in).
	if _, err := db.Exec(ctx, `DELETE FROM license WHERE id = 'current'`); err != nil {
		t.Fatalf("reset license row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM license WHERE id = 'current'`)
	})
	return db, log
}

func TestLicenseLoadSaveRoundTrip(t *testing.T) {
	db, _ := setupLicenseTestDB(t)
	ctx := context.Background()

	_, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil {
		t.Fatalf("LoadLicenseRecord (empty): %v", err)
	}
	if ok {
		t.Fatal("expected no license row before any save")
	}

	fp := license.Compose("machine-a", "aa:bb:cc:dd:ee:ff")
	payload := license.Payload{
		KeyID:           "K-1",
		Licensee:        "Test ISP",
		IssuedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Tier:            "5k",
		MaxSubscribers:  5000,
		EntitledVersion: "1",
		Fingerprint:     fp,
	}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{
		KeyID:             "K-1",
		IssuedFingerprint: fp,
		State:             license.StateValid,
		Payload:           payload,
	}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil {
		t.Fatalf("LoadLicenseRecord: %v", err)
	}
	if !ok {
		t.Fatal("expected a license row after save")
	}
	if got.KeyID != "K-1" || got.Payload.Licensee != "Test ISP" || got.IssuedFingerprint != fp {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if CachedLicenseState() != license.StateValid {
		t.Errorf("cache after save = %v, want valid", CachedLicenseState())
	}
}

func TestRefreshLicenseCacheEntersGraceAndAlerts(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	// Force a fingerprint the live host will never match, so
	// RefreshLicenseCache observes a mismatch deterministically regardless
	// of the CI runner's actual machine-id/MAC.
	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "ci-runner-does-not-matter")
	issuedFP := license.Compose("definitely-not-this-machine", "00:00:00:00:00:00")

	payload := license.Payload{KeyID: "K-2", Licensee: "Grace Co", Fingerprint: issuedFP}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{
		KeyID:             "K-2",
		IssuedFingerprint: issuedFP,
		State:             license.StateValid,
		Payload:           payload,
	}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	var before int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM alert_events WHERE type = 'license_grace'`).Scan(&before)

	RefreshLicenseCache(ctx, db, log)

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil || !ok {
		t.Fatalf("LoadLicenseRecord after refresh: ok=%v err=%v", ok, err)
	}
	if got.State != license.StateGrace {
		t.Fatalf("state after refresh = %v, want grace", got.State)
	}
	if got.GraceStartedAt == nil {
		t.Fatal("grace_started_at not set")
	}
	if CachedLicenseState() != license.StateGrace {
		t.Errorf("cache after refresh = %v, want grace", CachedLicenseState())
	}

	var after int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM alert_events WHERE type = 'license_grace'`).Scan(&after); err != nil {
		t.Fatalf("count alert_events: %v", err)
	}
	if after != before+1 {
		t.Errorf("alert_events(type=license_grace) = %d, want %d (exactly one new row on grace entry)", after, before+1)
	}

	// A second refresh with no state change must not raise a duplicate alert.
	RefreshLicenseCache(ctx, db, log)
	var stillAfter int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM alert_events WHERE type = 'license_grace'`).Scan(&stillAfter)
	if stillAfter != after {
		t.Errorf("repeat refresh raised another alert: %d -> %d", after, stillAfter)
	}
}

func TestRefreshLicenseCacheRestoreToSameHardwareStaysValid(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "same-host")
	fp, err := license.Current()
	if err != nil {
		t.Fatalf("license.Current: %v", err)
	}

	payload := license.Payload{KeyID: "K-3", Licensee: "Same Host Co", Fingerprint: fp}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{
		KeyID:             "K-3",
		IssuedFingerprint: fp,
		State:             license.StateValid,
		Payload:           payload,
	}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	RefreshLicenseCache(ctx, db, log)

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil || !ok {
		t.Fatalf("LoadLicenseRecord: ok=%v err=%v", ok, err)
	}
	if got.State != license.StateValid {
		t.Errorf("same-hardware restore: state = %v, want valid", got.State)
	}
}

// TestLicenseHasEntitlement_DBRowEditDoesNotGrant is gate item 7's core
// proof: entitlements are derived from the SIGNED payload on every
// evaluation, never trusted from a DB column an operator could edit
// directly. There is no settings key or config file this could alternatively
// be forged through either — LicenseHasEntitlement/CachedLicenseMaxSubscribers
// read only the process-wide snapshot RefreshLicenseCache builds from a
// signature-reverified row, full stop.
func TestLicenseHasEntitlement_DBRowEditDoesNotGrant(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "forgery-test-host")
	fp, err := license.Current()
	if err != nil {
		t.Fatalf("license.Current: %v", err)
	}

	payload := license.Payload{
		KeyID: "K-FORGE", Licensee: "Forgery Co", Tier: "pro", MaxSubscribers: 1000,
		Fingerprint: fp, Version: 2, Entitlements: []string{}, // deliberately NOT granted
	}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{KeyID: "K-FORGE", IssuedFingerprint: fp, State: license.StateValid, Payload: payload}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}
	RefreshLicenseCache(ctx, db, log)

	if LicenseHasEntitlement(license.EntitlementWhiteLabel) {
		t.Fatal("white_label reported granted before any tampering — test setup wrong")
	}
	capBefore := CachedLicenseMaxSubscribers()

	// The forgery: edit the payload directly via SQL, bypassing the app and
	// the signature entirely — exactly what an operator with DB access could
	// attempt. The signature column is left as the ORIGINAL value, which now
	// no longer matches the edited payload bytes.
	if _, err := db.Exec(ctx,
		`UPDATE license SET payload = jsonb_set(jsonb_set(payload, '{entitlements}', '["white_label"]'), '{max_subscribers}', '999999') WHERE id = 'current'`,
	); err != nil {
		t.Fatalf("simulate DB tampering: %v", err)
	}

	// A tampered row must fail re-verification and NOT be adopted — the
	// in-memory snapshot must stay exactly what it was before the tamper.
	RefreshLicenseCache(ctx, db, log)

	if LicenseHasEntitlement(license.EntitlementWhiteLabel) {
		t.Fatal("editing the entitlements column directly in the DB granted white_label — forgeable")
	}
	if got := CachedLicenseMaxSubscribers(); got != capBefore {
		t.Fatalf("cap changed after DB tampering: got %d, want unchanged %d", got, capBefore)
	}
}

// TestRefreshLicenseCache_RecordsAuditRowOnTransition covers migration
// 0596: every state transition (not every periodic evaluation) gets exactly
// one license_state_audit row naming the from/to state and why.
func TestRefreshLicenseCache_RecordsAuditRowOnTransition(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM license_state_audit`) })

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "audit-test-host-does-not-match")
	issuedFP := license.Compose("definitely-not-this-machine-either", "00:00:00:00:00:01")
	payload := license.Payload{KeyID: "K-AUDIT", Licensee: "Audit Co", Fingerprint: issuedFP}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{KeyID: "K-AUDIT", IssuedFingerprint: issuedFP, State: license.StateValid, Payload: payload}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	RefreshLicenseCache(ctx, db, log) // valid -> grace: exactly one audit row

	var count int
	var reason string
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM license_state_audit WHERE key_id = 'K-AUDIT'`).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit rows after first transition = %d, want 1", count)
	}
	if err := db.QueryRow(ctx,
		`SELECT reason FROM license_state_audit WHERE key_id = 'K-AUDIT' ORDER BY id DESC LIMIT 1`).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != string(license.ReasonFingerprintMismatchFirst) {
		t.Errorf("reason = %q, want %q", reason, license.ReasonFingerprintMismatchFirst)
	}

	// A second refresh with no state change must not add another row.
	RefreshLicenseCache(ctx, db, log)
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM license_state_audit WHERE key_id = 'K-AUDIT'`).Scan(&count); err != nil {
		t.Fatalf("count audit rows (2nd): %v", err)
	}
	if count != 1 {
		t.Fatalf("audit rows after a no-op refresh = %d, want still 1 (transitions only, not every evaluation)", count)
	}
}
