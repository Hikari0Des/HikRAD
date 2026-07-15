package platform

// DB persistence + process-wide cache for the C4 license state machine
// (migration 0410). The pure grace-evaluation logic lives in
// internal/platform/license (no DB dependency, so it's unit-testable without
// Postgres); this file is the seam that loads/saves the `license` table and
// keeps a cheap in-memory cache LicenseGate reads on every request.
//
// internal/platform/setupapi (a sibling package, not platform itself — see
// config.go's "nothing here registers HTTP routes") owns the HTTP handlers
// and calls these exported functions; it also owns permission checks via
// internal/auth, which platform cannot import (auth already imports
// platform, so the reverse edge would be a cycle).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/hikrad/hikrad/internal/platform/license"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var cachedLicenseState atomic.Value // holds license.State; zero value -> StateValid (no license row yet)

// CachedLicenseState returns the last-evaluated license state without a DB
// round-trip. LicenseGate reads this on every request; RefreshLicenseCache
// (called at boot and on a ticker, plus synchronously after every upload)
// keeps it current within one refresh interval.
func CachedLicenseState() license.State {
	if v, ok := cachedLicenseState.Load().(license.State); ok && v != "" {
		return v
	}
	return license.StateValid
}

// SetCachedLicenseStateForTest overrides the process-wide license cache.
// Exported solely so internal/httpapi's license-gate coverage-map test (in a
// different package, which cannot reach an unexported var) can exercise every
// state without standing up a Postgres-backed license row for each case.
// Never called outside tests.
func SetCachedLicenseStateForTest(s license.State) {
	cachedLicenseState.Store(s)
}

// LoadLicenseRecord reads the single `current` license row, if any. ok is
// false (with a nil error) when no license has ever been uploaded — a valid,
// unrestricted state for a fresh install (the wizard's license step hasn't
// run yet, or an operator chose to skip straight to internal testing).
func LoadLicenseRecord(ctx context.Context, db *pgxpool.Pool) (rec license.Record, ok bool, err error) {
	var payloadRaw []byte
	var state string
	err = db.QueryRow(ctx,
		`SELECT key_id, payload, fingerprint, state, grace_started_at
		   FROM license WHERE id = 'current'`).
		Scan(&rec.KeyID, &payloadRaw, &rec.IssuedFingerprint, &state, &rec.GraceStartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return license.Record{}, false, nil
	}
	if err != nil {
		return license.Record{}, false, fmt.Errorf("platform: load license: %w", err)
	}
	if err := json.Unmarshal(payloadRaw, &rec.Payload); err != nil {
		return license.Record{}, false, fmt.Errorf("platform: decode license payload: %w", err)
	}
	rec.State = license.State(state)
	return rec, true, nil
}

// SaveLicenseRecord upserts the full license row: called on upload (a new
// blob replaces licensee/tier/signature/etc. entirely). state/grace fields
// come from rec as already evaluated by the caller (setupapi calls
// license.Evaluate before saving so a freshly uploaded key that still
// mismatches the live fingerprint is stored honestly, not force-valid).
func SaveLicenseRecord(ctx context.Context, db *pgxpool.Pool, rec license.Record, signature string) error {
	payload, err := json.Marshal(rec.Payload)
	if err != nil {
		return fmt.Errorf("platform: encode license payload: %w", err)
	}
	_, err = db.Exec(ctx,
		`INSERT INTO license (id, key_id, payload, signature, fingerprint, state, grace_started_at, installed_at, updated_at)
		 VALUES ('current', $1, $2, $3, $4, $5, $6, now(), now())
		 ON CONFLICT (id) DO UPDATE SET
		   key_id = EXCLUDED.key_id, payload = EXCLUDED.payload, signature = EXCLUDED.signature,
		   fingerprint = EXCLUDED.fingerprint, state = EXCLUDED.state,
		   grace_started_at = EXCLUDED.grace_started_at, installed_at = now(), updated_at = now()`,
		rec.KeyID, payload, signature, rec.IssuedFingerprint, string(rec.State), rec.GraceStartedAt)
	if err != nil {
		return fmt.Errorf("platform: save license: %w", err)
	}
	cachedLicenseState.Store(rec.State)
	return nil
}

// saveLicenseState persists only the state/grace_started_at fields (the
// periodic re-evaluation path, which never touches the signed payload).
func saveLicenseState(ctx context.Context, db *pgxpool.Pool, rec license.Record) error {
	_, err := db.Exec(ctx,
		`UPDATE license SET state = $1, grace_started_at = $2, updated_at = now() WHERE id = 'current'`,
		string(rec.State), rec.GraceStartedAt)
	return err
}

// RefreshLicenseCache re-evaluates the installed license (if any) against the
// server's current fingerprint and now, persists a state transition, raises
// an in-app alert event the first time grace begins, and updates the
// process-wide cache. Called at boot, on a 10-minute ticker (setupapi.Module
// starts it), and synchronously right after an upload so the banner/gate
// react immediately rather than waiting for the next tick.
func RefreshLicenseCache(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) {
	rec, ok, err := LoadLicenseRecord(ctx, db)
	if err != nil {
		log.Error("license: refresh failed", "error", err)
		return
	}
	if !ok {
		cachedLicenseState.Store(license.StateValid)
		return
	}

	cur, ferr := license.Current()
	if ferr != nil {
		// Can't determine this host's fingerprint (stripped-down container,
		// missing /etc/machine-id mount): keep the last-known state rather
		// than guessing either direction.
		log.Warn("license: fingerprint unavailable, keeping last-known state", "error", ferr)
		cachedLicenseState.Store(rec.State)
		return
	}

	before := rec.State
	updated := license.Evaluate(rec, cur, time.Now().UTC())
	if updated.State != before {
		if err := saveLicenseState(ctx, db, updated); err != nil {
			log.Error("license: persist state transition failed", "error", err, "from", before, "to", updated.State)
		}
		if updated.State == license.StateGrace {
			raiseGraceAlert(ctx, db, log, updated)
		}
		log.Info("license: state transition", "from", before, "to", updated.State)
	}
	cachedLicenseState.Store(updated.State)
}

// raiseGraceAlert writes a row directly into monitorsvc's alert_events table
// (migration 0232) using its documented "always-record fallback" shape
// (rule_id NULL, contract: monitorsvc/alerts.go's `record` helper) so the
// in-app notification feed (GET /api/v1/live/notifications, SSE) surfaces the
// grace banner without platform importing internal/monitorsvc (which would
// be a path violation this phase — monitorsvc is Agent C's exclusive path).
// Best-effort: a failure here must never block the state transition itself.
func raiseGraceAlert(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, rec license.Record) {
	summary := fmt.Sprintf("License fingerprint mismatch: entered 14-day grace period (licensee %q)", rec.Payload.Licensee)
	payload, _ := json.Marshal(map[string]any{
		"key_id":             rec.KeyID,
		"issued_fingerprint": rec.IssuedFingerprint,
		"grace_started_at":   rec.GraceStartedAt,
	})
	_, err := db.Exec(ctx,
		`INSERT INTO alert_events (rule_id, state, type, summary, payload, deliveries)
		 VALUES (NULL, 'firing', 'license_grace', $1, $2, '[]')`,
		summary, payload)
	if err != nil {
		log.Warn("license: raise grace alert failed", "error", err)
	}
}
