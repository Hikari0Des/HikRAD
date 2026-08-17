package license

import (
	"encoding/json"
	"time"
)

// State is the license lifecycle state (FR-50.3), persisted in
// license.state (migration 0410) and returned verbatim by GET
// /api/v1/license.
type State string

const (
	StateValid        State = "valid"
	StateGrace        State = "grace"
	StateExpiredGrace State = "expired_grace"
)

// GracePeriod is the fixed 14-day window (FR-50.3) between a fingerprint
// mismatch first being observed and the panel going read-only.
const GracePeriod = 14 * 24 * time.Hour

// Record is the persisted license row plus the fields Evaluate needs to
// advance the state machine. IssuedFingerprint is the fingerprint the
// license was issued for (payload.Fingerprint, duplicated into its own
// column for query convenience); it is compared against the server's live
// fingerprint on every evaluation.
//
// PayloadRaw/Signature (FR-124) are the exact bytes the license row stores —
// carried alongside the already-decoded Payload so a caller can re-run
// Verify against them before trusting Payload for anything enforcement-
// relevant. Re-marshaling Payload would not do: json.Marshal is not
// guaranteed to reproduce the exact bytes a signature was computed over
// (unknown future fields would be silently dropped, for one), so the single
// verified path this phase requires re-checks the ORIGINAL bytes, not a
// round-tripped reconstruction of them.
type Record struct {
	KeyID             string
	Payload           Payload
	PayloadRaw        json.RawMessage
	Signature         string
	IssuedFingerprint string
	State             State
	GraceStartedAt    *time.Time
}

// Evaluate recomputes rec's state against the server's current fingerprint
// at time now, and returns the updated record. It is pure (no I/O, no
// clock reads beyond the passed-in now) so the grace matrix is exhaustively
// unit-testable; internal/platform is responsible for persisting the result
// when it differs from what was loaded.
//
// Rules (FR-50.3, AC-50b):
//   - a v2 payload's ExpiresAt is in the past -> expired_grace immediately,
//     no 14-day tolerance window (FR-124). A calendar expiry is a known,
//     non-accidental boundary — unlike fingerprint drift, which deserves the
//     benefit of the doubt for VM clones/disk migrations, an expiry date is
//     exactly what was agreed at issuance. Reuses expired_grace rather than a
//     new state so RADIUS/accounting's "grace never gates" exemption (which
//     keys off state, not off *why* the state is what it is) covers expiry
//     for free — the invariant is "expired_grace never stops RADIUS", not
//     "expired_grace-from-fingerprint-drift never stops RADIUS".
//   - fingerprint matches (within tolerance) -> valid, any grace timer cleared.
//     This is what makes "restore to the same hardware" a no-op and what makes
//     uploading a re-issued key for the new fingerprint clear the banner.
//   - first observed mismatch (state was valid) -> grace, timer starts now.
//   - already in grace, < 14 days elapsed -> stays grace.
//   - already in grace, >= 14 days elapsed -> expired_grace.
//   - already expired_grace -> stays expired_grace until the fingerprint
//     matches again (re-issued key installed).
func Evaluate(rec Record, currentFingerprint string, now time.Time) Record {
	if rec.Payload.ExpiresAt != nil && now.After(*rec.Payload.ExpiresAt) {
		rec.State = StateExpiredGrace
		return rec
	}

	if WithinTolerance(rec.IssuedFingerprint, currentFingerprint) {
		rec.State = StateValid
		rec.GraceStartedAt = nil
		return rec
	}

	switch rec.State {
	case StateGrace:
		if rec.GraceStartedAt != nil && now.Sub(*rec.GraceStartedAt) >= GracePeriod {
			rec.State = StateExpiredGrace
		}
		// else: still within the window, stays grace.
	case StateExpiredGrace:
		// stays expired_grace: only a matching fingerprint (handled above)
		// clears it.
	default: // StateValid or zero-value -> mismatch just appeared.
		t := now
		rec.GraceStartedAt = &t
		rec.State = StateGrace
	}
	return rec
}

// TransitionReason names why Evaluate moved (or kept) rec's state, for the
// migration-0596 licence-state audit log (FR-124) — a human-readable "why"
// alongside the raw from/to states.
type TransitionReason string

const (
	ReasonFingerprintMatch          TransitionReason = "fingerprint_match"
	ReasonFingerprintMismatchFirst  TransitionReason = "fingerprint_mismatch_first_seen"
	ReasonGraceWindowActive         TransitionReason = "grace_window_active"
	ReasonGraceWindowElapsed        TransitionReason = "grace_window_elapsed"
	ReasonExpiredGraceStillMismatch TransitionReason = "expired_grace_still_mismatch"
	ReasonLicenseExpired            TransitionReason = "license_expired"
)

// EvaluateReason derives the audit-log reason for the before -> after
// transition Evaluate just produced. Pure, and deliberately re-derives from
// the same fields Evaluate itself branches on rather than having Evaluate
// return a reason directly, so Evaluate's exported signature/behavior for
// existing callers (the pure grace matrix already unit-tested above) stays
// completely unchanged.
func EvaluateReason(before, after Record, currentFingerprint string, now time.Time) TransitionReason {
	if after.Payload.ExpiresAt != nil && now.After(*after.Payload.ExpiresAt) {
		return ReasonLicenseExpired
	}
	if WithinTolerance(after.IssuedFingerprint, currentFingerprint) {
		return ReasonFingerprintMatch
	}
	switch before.State {
	case StateGrace:
		if after.State == StateExpiredGrace {
			return ReasonGraceWindowElapsed
		}
		return ReasonGraceWindowActive
	case StateExpiredGrace:
		return ReasonExpiredGraceStillMismatch
	default:
		return ReasonFingerprintMismatchFirst
	}
}

// GraceExpiresAt returns when a grace-mode record's read-only cutover
// happens, or the zero time if rec is not currently in grace.
func (rec Record) GraceExpiresAt() time.Time {
	if rec.State != StateGrace || rec.GraceStartedAt == nil {
		return time.Time{}
	}
	return rec.GraceStartedAt.Add(GracePeriod)
}
