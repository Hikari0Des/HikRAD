package platform

// DB-backed suite for license persistence (gated on HIKRAD_TEST_DB_URL,
// matching the repo pattern). Exercises the load/save round-trip and
// RefreshLicenseCache's state-transition + grace-alert side effects against
// real Postgres.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	rec := license.Record{
		KeyID:             "K-1",
		IssuedFingerprint: fp,
		State:             license.StateValid,
		Payload: license.Payload{
			KeyID:           "K-1",
			Licensee:        "Test ISP",
			IssuedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Tier:            "5k",
			MaxSubscribers:  5000,
			EntitledVersion: "1",
			Fingerprint:     fp,
		},
	}
	if err := SaveLicenseRecord(ctx, db, rec, "sig-b64"); err != nil {
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

	rec := license.Record{
		KeyID:             "K-2",
		IssuedFingerprint: issuedFP,
		State:             license.StateValid,
		Payload:           license.Payload{KeyID: "K-2", Licensee: "Grace Co", Fingerprint: issuedFP},
	}
	if err := SaveLicenseRecord(ctx, db, rec, "sig-b64"); err != nil {
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

	rec := license.Record{
		KeyID:             "K-3",
		IssuedFingerprint: fp,
		State:             license.StateValid,
		Payload:           license.Payload{KeyID: "K-3", Licensee: "Same Host Co", Fingerprint: fp},
	}
	if err := SaveLicenseRecord(ctx, db, rec, "sig-b64"); err != nil {
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
