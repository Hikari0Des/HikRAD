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

// LicenseSnapshot is the ONE process-wide view of the installed license's
// enforcement-relevant fields (FR-124): state, subscriber cap, and the
// resolved entitlement set, derived from the SIGNED payload every time
// SaveLicenseRecord/RefreshLicenseCache runs and never written from
// anywhere else. Every enforcement call site (the cap check, entitlement
// gates like white-label, the read-only license gate) reads this one
// structure — never a settings row, a config file, or a DB column an
// operator could edit and have it take effect. RefreshLicenseCache
// re-verifies the stored payload's signature before building a new
// snapshot from it (see the doc comment there); a snapshot is otherwise
// never replaced with unverified data.
type LicenseSnapshot struct {
	State          license.State
	MaxSubscribers int
	Entitlements   map[string]bool
	ExpiresAt      *time.Time
	SupportUntil   *time.Time
}

var cachedLicenseSnapshot atomic.Value // holds LicenseSnapshot

func loadSnapshot() LicenseSnapshot {
	if v, ok := cachedLicenseSnapshot.Load().(LicenseSnapshot); ok {
		return v
	}
	return LicenseSnapshot{State: license.StateValid}
}

// entitlementSnapshot resolves every license.KnownEntitlements name against
// p.HasEntitlement into the map a LicenseSnapshot carries — the same
// derivation the License page's response uses, so the panel display and the
// enforcement path never have two different notions of what's granted.
func entitlementSnapshot(p license.Payload) map[string]bool {
	m := make(map[string]bool, len(license.KnownEntitlements))
	for _, name := range license.KnownEntitlements {
		m[name] = p.HasEntitlement(name)
	}
	return m
}

// CachedLicenseMaxSubscribers returns the licensed subscriber cap without a DB
// round-trip; 0 means unlimited (or no license installed).
func CachedLicenseMaxSubscribers() int {
	return loadSnapshot().MaxSubscribers
}

// LicenseHasEntitlement reports whether the currently cached license grants
// name (FR-122/FR-124), e.g. license.EntitlementWhiteLabel. False (never a
// panic) when no license is installed or the snapshot hasn't been primed
// yet — an absent/unverifiable license grants nothing, which is the
// fail-safe direction for every entitlement this phase defines (white_label
// false means the attribution mark stays visible, not the other way round).
func LicenseHasEntitlement(name string) bool {
	return loadSnapshot().Entitlements[name]
}

// CachedLicenseExpiresAt/CachedLicenseSupportUntil expose the resolved
// snapshot's calendar fields for the License page (FR-125); nil means
// perpetual/unset.
func CachedLicenseExpiresAt() *time.Time    { return loadSnapshot().ExpiresAt }
func CachedLicenseSupportUntil() *time.Time { return loadSnapshot().SupportUntil }

// storeSnapshot is the one place cachedLicenseSnapshot is ever written —
// called by SaveLicenseRecord/RefreshLicenseCache (the two verified
// production paths) and by the "ForTest" setters below. Kept unexported and
// distinct from the ForTest name so a call site never reads as "test-only
// code in the production path" by accident.
func storeSnapshot(s LicenseSnapshot) {
	if s.Entitlements == nil {
		s.Entitlements = map[string]bool{}
	}
	cachedLicenseSnapshot.Store(s)
}

// SetCachedLicenseSnapshotForTest overrides the entire process-wide
// snapshot in one call — the FR-124 test seam for entitlement/expiry
// coverage that doesn't want to stand up a signed license row. Never called
// outside tests.
func SetCachedLicenseSnapshotForTest(s LicenseSnapshot) {
	storeSnapshot(s)
}

// SetCachedLicenseMaxSubscribersForTest overrides only the cached cap,
// leaving the rest of the snapshot as last stored. Kept as a thin wrapper
// over SetCachedLicenseSnapshotForTest so existing cap-enforcement tests
// (internal/platform/subscriber_cap_test.go) don't need to change shape.
func SetCachedLicenseMaxSubscribersForTest(n int) {
	s := loadSnapshot()
	s.MaxSubscribers = n
	storeSnapshot(s)
}

// SubscriberCapStatus describes how a subscriber count sits against the
// licensed cap (FR-105). Cap 0 => Unlimited (no query, never Blocked).
type SubscriberCapStatus struct {
	Cap       int  `json:"cap"`    // licensed MaxSubscribers; 0 = unlimited
	Buffer    int  `json:"buffer"` // overage buffer = max(10, ceil(cap*5%)); 0 when unlimited
	Count     int  `json:"count"`  // the evaluated count (post-add for a create check)
	Unlimited bool `json:"unlimited"`
	Over      bool `json:"over"`    // Count > Cap — in or past the warn band (renew/upgrade prompt)
	Blocked   bool `json:"blocked"` // Count > Cap+Buffer — creation must be refused
}

// EvalSubscriberCap evaluates a (would-be) subscriber count against the cached
// license cap. Pure: the buffer is max(10, ceil(cap*0.05)); Blocked means the
// count exceeds cap+buffer (refuse the create), Over means it exceeds cap (a
// warn-band prompt, still allowed). An unlimited/absent license is never
// Over/Blocked.
func EvalSubscriberCap(count int) SubscriberCapStatus {
	cap := CachedLicenseMaxSubscribers()
	if cap <= 0 {
		return SubscriberCapStatus{Count: count, Unlimited: true}
	}
	buffer := (cap + 19) / 20 // ceil(cap/20) == ceil(cap*0.05)
	if buffer < 10 {
		buffer = 10
	}
	return SubscriberCapStatus{
		Cap:     cap,
		Buffer:  buffer,
		Count:   count,
		Over:    count > cap,
		Blocked: count > cap+buffer,
	}
}

// CachedLicenseState returns the last-evaluated license state without a DB
// round-trip. LicenseGate reads this on every request; RefreshLicenseCache
// (called at boot and on a ticker, plus synchronously after every upload)
// keeps it current within one refresh interval.
func CachedLicenseState() license.State {
	if s := loadSnapshot().State; s != "" {
		return s
	}
	return license.StateValid
}

// SetCachedLicenseStateForTest overrides only the cached state, leaving the
// rest of the snapshot as last stored. Exported solely so internal/httpapi's
// license-gate coverage-map test (a different package, can't reach an
// unexported var) can exercise every state without standing up a
// Postgres-backed license row. Never called outside tests.
func SetCachedLicenseStateForTest(s license.State) {
	snap := loadSnapshot()
	snap.State = s
	storeSnapshot(snap)
}

// LoadLicenseRecord reads the single `current` license row, if any. ok is
// false (with a nil error) when no license has ever been uploaded — a valid,
// unrestricted state for a fresh install (the wizard's license step hasn't
// run yet, or an operator chose to skip straight to internal testing).
// rec.PayloadRaw/rec.Signature carry the exact stored bytes (not a
// re-marshaled reconstruction) so a caller can re-verify them — see
// RefreshLicenseCache, the one place that actually does.
func LoadLicenseRecord(ctx context.Context, db *pgxpool.Pool) (rec license.Record, ok bool, err error) {
	rec, _, ok, err = loadLicenseRow(ctx, db)
	return rec, ok, err
}

// loadLicenseRow is LoadLicenseRecord plus legacy: true when the row predates
// migration 0635 (payload_raw NULL), i.e. its exact signed bytes were never
// stored and its signature can no longer be re-verified.
func loadLicenseRow(ctx context.Context, db *pgxpool.Pool) (rec license.Record, legacy, ok bool, err error) {
	var payloadJSONB, payloadRaw []byte
	var signature string
	var state string
	err = db.QueryRow(ctx,
		`SELECT key_id, payload, payload_raw, signature, fingerprint, state, grace_started_at
		   FROM license WHERE id = 'current'`).
		Scan(&rec.KeyID, &payloadJSONB, &payloadRaw, &signature, &rec.IssuedFingerprint, &state, &rec.GraceStartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return license.Record{}, false, false, nil
	}
	if err != nil {
		return license.Record{}, false, false, fmt.Errorf("platform: load license: %w", err)
	}
	// payload_raw (migration 0635) is the verbatim signed bytes; the jsonb
	// column is normalised by Postgres and can never pass re-verification.
	// A pre-0635 row has no raw bytes — fall back to the jsonb text so the
	// row still decodes; RefreshLicenseCache applies its legacy handling.
	if len(payloadRaw) == 0 {
		payloadRaw = payloadJSONB
		legacy = true
	}
	if err := json.Unmarshal(payloadRaw, &rec.Payload); err != nil {
		return license.Record{}, false, false, fmt.Errorf("platform: decode license payload: %w", err)
	}
	rec.PayloadRaw = json.RawMessage(payloadRaw)
	rec.Signature = signature
	rec.State = license.State(state)
	return rec, legacy, true, nil
}

// SaveLicenseRecord upserts the full license row: called on upload (a new
// blob replaces licensee/tier/signature/etc. entirely). state/grace fields
// come from rec as already evaluated by the caller (setupapi calls
// license.Evaluate before saving so a freshly uploaded key that still
// mismatches the live fingerprint is stored honestly, not force-valid).
// installLicense already ran license.Verify against this exact payload
// before calling this function, so it is safe to build the cache snapshot
// straight from rec.Payload here — the re-verify-on-load discipline
// (RefreshLicenseCache) exists for the path this function doesn't cover:
// a row that changed via something other than a verified upload.
//
// rec.PayloadRaw must be the exact bytes the signature covers (the uploaded
// blob's payload, verbatim). It is persisted to payload_raw (migration 0635)
// because the jsonb payload column normalises key order/whitespace, and
// RefreshLicenseCache re-verifies the signature on every boot/tick — storing
// only a re-marshaled/jsonb copy made that re-verification fail for every
// installed license, so the grace/expiry state machine never ran. When
// PayloadRaw is empty (callers that only hold a decoded Payload) it falls back
// to json.Marshal(rec.Payload), which is byte-identical to license.Sign's
// encoding for a payload this build fully understands.
func SaveLicenseRecord(ctx context.Context, db *pgxpool.Pool, rec license.Record, signature string) error {
	raw := []byte(rec.PayloadRaw)
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(rec.Payload)
		if err != nil {
			return fmt.Errorf("platform: encode license payload: %w", err)
		}
	}
	_, err := db.Exec(ctx,
		`INSERT INTO license (id, key_id, payload, payload_raw, signature, fingerprint, state, grace_started_at, installed_at, updated_at)
		 VALUES ('current', $1, $2::jsonb, $3, $4, $5, $6, $7, now(), now())
		 ON CONFLICT (id) DO UPDATE SET
		   key_id = EXCLUDED.key_id, payload = EXCLUDED.payload, payload_raw = EXCLUDED.payload_raw,
		   signature = EXCLUDED.signature,
		   fingerprint = EXCLUDED.fingerprint, state = EXCLUDED.state,
		   grace_started_at = EXCLUDED.grace_started_at, installed_at = now(), updated_at = now()`,
		rec.KeyID, string(raw), raw, signature, rec.IssuedFingerprint, string(rec.State), rec.GraceStartedAt)
	if err != nil {
		return fmt.Errorf("platform: save license: %w", err)
	}
	storeSnapshot(LicenseSnapshot{
		State:          rec.State,
		MaxSubscribers: rec.Payload.MaxSubscribers,
		Entitlements:   entitlementSnapshot(rec.Payload),
		ExpiresAt:      rec.Payload.ExpiresAt,
		SupportUntil:   rec.Payload.SupportUntil,
	})
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

// insertLicenseStateAudit records one migration-0596 transition row. Best
// effort — a failure here must never block the state transition itself,
// same posture as raiseGraceAlert.
func insertLicenseStateAudit(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, keyID string, before, after license.Record, currentFP string, reason license.TransitionReason) {
	_, err := db.Exec(ctx,
		`INSERT INTO license_state_audit (key_id, from_state, to_state, issued_fingerprint, current_fingerprint, reason)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		keyID, string(before.State), string(after.State), after.IssuedFingerprint, currentFP, string(reason))
	if err != nil {
		log.Warn("license: insert state-transition audit row failed", "error", err)
	}
}

// RefreshLicenseCache re-evaluates the installed license (if any) against the
// server's current fingerprint and now, persists a state transition, raises
// an in-app alert event the first time grace begins, and updates the
// process-wide cache. Called at boot, on a 10-minute ticker (setupapi.Module
// starts it), and synchronously right after an upload so the banner/gate
// react immediately rather than waiting for the next tick.
//
// FR-124 forgeability guard: before adopting rec.Payload into the cached
// snapshot, this function re-verifies rec.PayloadRaw/rec.Signature against
// license.ProductionPublicKey. installLicense already verified the payload
// once at upload time, but RefreshLicenseCache also runs unconditionally on
// every boot and every 10-minute tick — the only path that would ever pick
// up a license row edited directly in the database (or a config file, if one
// existed) rather than through a verified upload. A row that fails
// re-verification is never adopted: this function returns without touching
// cachedLicenseSnapshot, so the last-known-good in-memory snapshot keeps
// being what every enforcement call site reads. This is what makes gate
// item 7 ("edit the entitlement in the database... has no effect") true:
// any edit to payload or signature invalidates the signature over the
// *other* one, since Ed25519 signs the exact payload bytes.
func RefreshLicenseCache(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) {
	rec, legacy, ok, err := loadLicenseRow(ctx, db)
	if err != nil {
		log.Error("license: refresh failed", "error", err)
		return
	}
	if !ok {
		// No license row at all: the zero-license "valid", uncapped default
		// (FR-50 / AC-82b — a dev checkout or a fresh install before the
		// wizard's license step must not be blocked). Unchanged by FR-124.
		storeSnapshot(LicenseSnapshot{State: license.StateValid})
		return
	}

	verified, verr := license.Verify(license.ProductionPublicKey, license.Blob{Payload: rec.PayloadRaw, Signature: rec.Signature})
	// unverifiedLegacy: a pre-0635 row whose exact signed bytes were never
	// stored (the jsonb copy is normalised by Postgres), so it cannot be
	// re-verified through no fault of the operator. Refusing it outright would
	// leave the fingerprint/expiry state machine permanently switched off for
	// every upgraded install (the bug 0635 fixes); adopting it as verified
	// would break FR-124. Middle ground: the grace/expiry state machine and the
	// cap still run off the stored decoded payload (so mismatch and expiry are
	// enforced, never silently "valid"), but it grants NO entitlements — an
	// unverifiable licence grants nothing (FR-124) — until the operator
	// re-uploads the licence file once, which stores payload_raw.
	unverifiedLegacy := false
	switch {
	case verr == nil:
		rec.Payload = verified
	case legacy:
		unverifiedLegacy = true
		log.Warn("license: installed licence predates exact-bytes storage (migration 0635) and cannot be signature re-verified — "+
			"grace/expiry still enforced from the stored payload, entitlements withheld; re-upload the licence file to restore full verification",
			"error", verr, "key_id", rec.KeyID)
	default:
		log.Error("license: stored record failed signature re-verification — refusing to adopt, keeping last-known-good cache", "error", verr, "key_id", rec.KeyID)
		return
	}
	entitlements := entitlementSnapshot(rec.Payload)
	if unverifiedLegacy {
		entitlements = map[string]bool{}
	}

	cur, ferr := license.Current()
	if ferr != nil {
		// Can't determine this host's fingerprint (stripped-down container,
		// missing /etc/machine-id mount): keep the last-known state rather
		// than guessing either direction, but the verified entitlements/cap
		// still update — those don't depend on the fingerprint.
		log.Warn("license: fingerprint unavailable, keeping last-known state", "error", ferr)
		snap := loadSnapshot()
		snap.MaxSubscribers = rec.Payload.MaxSubscribers
		snap.Entitlements = entitlements
		snap.ExpiresAt = rec.Payload.ExpiresAt
		snap.SupportUntil = rec.Payload.SupportUntil
		storeSnapshot(snap)
		return
	}

	before := rec.State
	now := time.Now().UTC()
	updated := license.Evaluate(rec, cur, now)
	if updated.State != before {
		if err := saveLicenseState(ctx, db, updated); err != nil {
			log.Error("license: persist state transition failed", "error", err, "from", before, "to", updated.State)
		}
		reason := license.EvaluateReason(rec, updated, cur, now)
		insertLicenseStateAudit(ctx, db, log, rec.KeyID, rec, updated, cur, reason)
		if updated.State == license.StateGrace {
			raiseGraceAlert(ctx, db, log, updated)
		}
		log.Info("license: state transition", "from", before, "to", updated.State, "reason", reason)
	}
	// Whenever the license reads valid, clear any firing grace notifications —
	// not just on the transition edge, so a pile of stale rows from earlier
	// fingerprint flapping self-heals on the next boot/tick. Idempotent: a
	// no-op when nothing is firing.
	if updated.State == license.StateValid {
		resolveGraceAlerts(ctx, db, log)
	}
	storeSnapshot(LicenseSnapshot{
		State:          updated.State,
		MaxSubscribers: updated.Payload.MaxSubscribers,
		Entitlements:   entitlements,
		ExpiresAt:      updated.Payload.ExpiresAt,
		SupportUntil:   updated.Payload.SupportUntil,
	})
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
	// Dedup: only one firing grace notification at a time. Without this, every
	// fingerprint flap raised a fresh row and they piled up in the feed.
	_, err := db.Exec(ctx,
		`INSERT INTO alert_events (rule_id, state, type, summary, payload, deliveries)
		 SELECT NULL, 'firing', 'license_grace', $1, $2, '[]'
		 WHERE NOT EXISTS (
		     SELECT 1 FROM alert_events WHERE type = 'license_grace' AND state = 'firing'
		 )`,
		summary, payload)
	if err != nil {
		log.Warn("license: raise grace alert failed", "error", err)
	}
}

// resolveGraceAlerts marks every still-firing license_grace notification
// resolved. Called whenever the license reads valid again (re-issued key
// uploaded, or the fingerprint drifted back within tolerance) so the mismatch
// banner in the notification feed clears instead of lingering forever.
func resolveGraceAlerts(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) {
	if _, err := db.Exec(ctx,
		`UPDATE alert_events SET state = 'resolved'
		 WHERE type = 'license_grace' AND state = 'firing'`,
	); err != nil {
		log.Warn("license: resolve grace alerts failed", "error", err)
	}
}
