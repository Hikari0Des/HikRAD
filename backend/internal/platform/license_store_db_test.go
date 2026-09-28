package platform

// DB-backed suite for license persistence (gated on HIKRAD_TEST_DB_URL,
// matching the repo pattern). Exercises the load/save round-trip and
// RefreshLicenseCache's state-transition + grace-alert side effects against
// real Postgres.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	// raiseGraceAlert dedups on "a license_grace alert is already firing", so
	// a firing row left by an earlier test (or an earlier run against the same
	// database) would swallow this test's grace alert. Start from none firing.
	if _, err := db.Exec(ctx, `UPDATE alert_events SET state = 'resolved' WHERE type = 'license_grace' AND state = 'firing'`); err != nil {
		t.Fatalf("resolve stale license_grace alerts: %v", err)
	}
	return db, log
}

// uniqueKeyID returns a key id unique to this test run. license_state_audit
// is append-only (a BEFORE DELETE trigger refuses cleanup), so audit-row
// counts must be scoped to a key id no earlier run can have used.
func uniqueKeyID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
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
	// No cleanup of license_state_audit: it is append-only by design (the
	// DELETE a cleanup would issue is refused by trigger), so the key id is
	// unique per run instead.
	keyID := uniqueKeyID("K-AUDIT")

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "audit-test-host-does-not-match")
	issuedFP := license.Compose("definitely-not-this-machine-either", "00:00:00:00:00:01")
	payload := license.Payload{KeyID: keyID, Licensee: "Audit Co", Fingerprint: issuedFP}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{KeyID: keyID, IssuedFingerprint: issuedFP, State: license.StateValid, Payload: payload}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	RefreshLicenseCache(ctx, db, log) // valid -> grace: exactly one audit row

	var count int
	var reason string
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM license_state_audit WHERE key_id = $1`, keyID).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit rows after first transition = %d, want 1", count)
	}
	if err := db.QueryRow(ctx,
		`SELECT reason FROM license_state_audit WHERE key_id = $1 ORDER BY id DESC LIMIT 1`, keyID).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != string(license.ReasonFingerprintMismatchFirst) {
		t.Errorf("reason = %q, want %q", reason, license.ReasonFingerprintMismatchFirst)
	}

	// A second refresh with no state change must not add another row.
	RefreshLicenseCache(ctx, db, log)
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM license_state_audit WHERE key_id = $1`, keyID).Scan(&count); err != nil {
		t.Fatalf("count audit rows (2nd): %v", err)
	}
	if count != 1 {
		t.Fatalf("audit rows after a no-op refresh = %d, want still 1 (transitions only, not every evaluation)", count)
	}
}

// signRawForTest signs raw verbatim (not a json.Marshal of a Payload) under a
// throwaway key swapped in for license.ProductionPublicKey — for payloads
// whose wire bytes deliberately differ from Go's canonical encoding.
func signRawForTest(t *testing.T, raw []byte) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	origPub := license.ProductionPublicKey
	license.ProductionPublicKey = pub
	t.Cleanup(func() { license.ProductionPublicKey = origPub })
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw))
}

// TestRefreshLicenseCache_ReverifiesExactUploadedBytes is the regression
// guard for the jsonb-normalisation bug: the license table's payload column
// is jsonb, which reorders keys and strips whitespace, so a row whose signed
// bytes were only kept there failed FR-124 re-verification on every refresh
// and the grace/expiry state machine never ran (a mismatched or expired
// licence stayed "valid" forever). The uploaded bytes here are deliberately
// NOT Go's canonical encoding (reordered keys, indentation), exactly what a
// jsonb round-trip can never reproduce; they must survive in payload_raw and
// verify, so the mismatch is detected and the signed entitlement adopted.
func TestRefreshLicenseCache_ReverifiesExactUploadedBytes(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "exact-bytes-host")
	issuedFP := license.Compose("some-other-machine", "00:00:00:00:00:02")
	raw := []byte("{\n  \"version\": 2,\n  \"licensee\": \"Bytes Co\",\n  \"key_id\": \"K-BYTES\",\n" +
		"  \"fingerprint\": \"" + issuedFP + "\",\n  \"max_subscribers\": 500,\n  \"entitlements\": [\"white_label\"]\n}")
	signature := signRawForTest(t, raw)
	payload, err := license.Verify(license.ProductionPublicKey, license.Blob{Payload: raw, Signature: signature})
	if err != nil {
		t.Fatalf("test setup: raw payload does not verify: %v", err)
	}
	rec := license.Record{KeyID: payload.KeyID, Payload: payload, PayloadRaw: raw, IssuedFingerprint: issuedFP, State: license.StateValid}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}

	RefreshLicenseCache(ctx, db, log)

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil || !ok {
		t.Fatalf("LoadLicenseRecord: ok=%v err=%v", ok, err)
	}
	if string(got.PayloadRaw) != string(raw) {
		t.Errorf("stored payload bytes changed on round-trip:\n got %q\nwant %q", got.PayloadRaw, raw)
	}
	if got.State != license.StateGrace {
		t.Fatalf("state = %v, want grace (a mismatched licence must not stay valid)", got.State)
	}
	if !LicenseHasEntitlement(license.EntitlementWhiteLabel) {
		t.Error("verified white_label entitlement not adopted")
	}
	if CachedLicenseMaxSubscribers() != 500 {
		t.Errorf("cap = %d, want 500", CachedLicenseMaxSubscribers())
	}
}

// saveLegacyLicenseRow simulates a row installed before migration 0635: the
// signed bytes survive only as normalised jsonb and payload_raw is NULL.
func saveLegacyLicenseRow(t *testing.T, db *pgxpool.Pool, p license.Payload) {
	t.Helper()
	ctx := context.Background()
	_, signature := signTestPayload(t, p)
	rec := license.Record{KeyID: p.KeyID, Payload: p, IssuedFingerprint: p.Fingerprint, State: license.StateValid}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE license SET payload_raw = NULL WHERE id = 'current'`); err != nil {
		t.Fatalf("null payload_raw: %v", err)
	}
}

// TestRefreshLicenseCache_LegacyRowStillEnforcesGraceWithoutEntitlements
// covers upgraded installs whose licence predates payload_raw: the row can't
// be signature re-verified, but it must neither brick the install nor stay
// silently "valid" — fingerprint mismatch still enters grace (with the
// transition audited) and the cap still applies, while no entitlement is
// granted from the unverifiable payload (FR-124: unverifiable grants nothing).
func TestRefreshLicenseCache_LegacyRowStillEnforcesGraceWithoutEntitlements(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()
	keyID := uniqueKeyID("K-LEGACY")

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "legacy-host")
	issuedFP := license.Compose("legacy-other-machine", "00:00:00:00:00:03")
	saveLegacyLicenseRow(t, db, license.Payload{
		KeyID: keyID, Licensee: "Legacy Co", Fingerprint: issuedFP, Version: 2,
		MaxSubscribers: 200, Entitlements: []string{license.EntitlementWhiteLabel},
	})

	RefreshLicenseCache(ctx, db, log)

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil || !ok {
		t.Fatalf("LoadLicenseRecord: ok=%v err=%v", ok, err)
	}
	if got.State != license.StateGrace {
		t.Fatalf("legacy row: state = %v, want grace", got.State)
	}
	if CachedLicenseState() != license.StateGrace {
		t.Errorf("legacy row: cached state = %v, want grace", CachedLicenseState())
	}
	if CachedLicenseMaxSubscribers() != 200 {
		t.Errorf("legacy row: cap = %d, want 200 (cap still enforced)", CachedLicenseMaxSubscribers())
	}
	if LicenseHasEntitlement(license.EntitlementWhiteLabel) {
		t.Error("legacy (unverifiable) row granted white_label — entitlements must be withheld until re-upload")
	}
	var audits int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM license_state_audit WHERE key_id = $1`, keyID).Scan(&audits); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if audits != 1 {
		t.Errorf("legacy row: audit rows = %d, want 1", audits)
	}
}

// TestRefreshLicenseCache_LegacyRowStillEnforcesExpiry: calendar expiry on a
// legacy row lands in expired_grace just like a verified one.
func TestRefreshLicenseCache_LegacyRowStillEnforcesExpiry(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "legacy-exp-host")
	fp, err := license.Current()
	if err != nil {
		t.Fatalf("license.Current: %v", err)
	}
	past := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	saveLegacyLicenseRow(t, db, license.Payload{
		KeyID: "K-LEGACY-EXP", Licensee: "Expired Legacy Co", Fingerprint: fp, Version: 2, ExpiresAt: &past,
	})

	RefreshLicenseCache(ctx, db, log)

	got, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil || !ok {
		t.Fatalf("LoadLicenseRecord: ok=%v err=%v", ok, err)
	}
	if got.State != license.StateExpiredGrace {
		t.Fatalf("expired legacy row: state = %v, want expired_grace", got.State)
	}
}

// TestRefreshLicenseCache_TamperedCurrentRowStillRejected: a post-0635 row
// (payload_raw present) whose bytes no longer match the signature is a
// tamper, not a legacy row — it must still be refused outright (FR-124).
func TestRefreshLicenseCache_TamperedCurrentRowStillRejected(t *testing.T) {
	db, log := setupLicenseTestDB(t)
	ctx := context.Background()

	t.Setenv("HIKRAD_MACHINE_ID_OVERRIDE", "tamper-host")
	fp, err := license.Current()
	if err != nil {
		t.Fatalf("license.Current: %v", err)
	}
	payload := license.Payload{KeyID: "K-TAMPER", Licensee: "Tamper Co", Fingerprint: fp, Version: 2, MaxSubscribers: 100, Entitlements: []string{}}
	_, signature := signTestPayload(t, payload)
	rec := license.Record{KeyID: "K-TAMPER", Payload: payload, IssuedFingerprint: fp, State: license.StateValid}
	if err := SaveLicenseRecord(ctx, db, rec, signature); err != nil {
		t.Fatalf("SaveLicenseRecord: %v", err)
	}
	RefreshLicenseCache(ctx, db, log)
	if CachedLicenseMaxSubscribers() != 100 {
		t.Fatalf("setup: cap = %d, want 100", CachedLicenseMaxSubscribers())
	}

	forged := []byte(`{"key_id":"K-TAMPER","licensee":"Tamper Co","fingerprint":"` + fp +
		`","version":2,"max_subscribers":0,"entitlements":["white_label"]}`)
	if _, err := db.Exec(ctx, `UPDATE license SET payload_raw = $1, payload = $2::jsonb WHERE id = 'current'`, forged, string(forged)); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	RefreshLicenseCache(ctx, db, log)
	if LicenseHasEntitlement(license.EntitlementWhiteLabel) || CachedLicenseMaxSubscribers() != 100 {
		t.Fatalf("tampered payload_raw adopted: white_label=%v cap=%d",
			LicenseHasEntitlement(license.EntitlementWhiteLabel), CachedLicenseMaxSubscribers())
	}
}
