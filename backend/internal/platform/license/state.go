package license

import "time"

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
type Record struct {
	KeyID             string
	Payload           Payload
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
//   - fingerprint matches (within tolerance) -> valid, any grace timer cleared.
//     This is what makes "restore to the same hardware" a no-op and what makes
//     uploading a re-issued key for the new fingerprint clear the banner.
//   - first observed mismatch (state was valid) -> grace, timer starts now.
//   - already in grace, < 14 days elapsed -> stays grace.
//   - already in grace, >= 14 days elapsed -> expired_grace.
//   - already expired_grace -> stays expired_grace until the fingerprint
//     matches again (re-issued key installed).
func Evaluate(rec Record, currentFingerprint string, now time.Time) Record {
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

// GraceExpiresAt returns when a grace-mode record's read-only cutover
// happens, or the zero time if rec is not currently in grace.
func (rec Record) GraceExpiresAt() time.Time {
	if rec.State != StateGrace || rec.GraceStartedAt == nil {
		return time.Time{}
	}
	return rec.GraceStartedAt.Add(GracePeriod)
}
