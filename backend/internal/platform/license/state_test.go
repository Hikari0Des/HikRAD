package license

import (
	"testing"
	"time"
)

func TestEvaluateGraceStateMachine(t *testing.T) {
	issued := Compose("machine-a", "aa:bb:cc:dd:ee:ff")
	current := issued
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 1. Matching fingerprint stays valid.
	rec := Record{State: StateValid, IssuedFingerprint: issued}
	rec = Evaluate(rec, current, t0)
	if rec.State != StateValid || rec.GraceStartedAt != nil {
		t.Fatalf("matching fingerprint: got %+v, want valid/no timer", rec)
	}

	// 2. Fingerprint mismatch from valid -> grace, timer starts.
	newFP := Compose("machine-b", "11:22:33:44:55:66")
	rec = Evaluate(rec, newFP, t0)
	if rec.State != StateGrace {
		t.Fatalf("first mismatch: state = %v, want grace", rec.State)
	}
	if rec.GraceStartedAt == nil || !rec.GraceStartedAt.Equal(t0) {
		t.Fatalf("grace timer = %v, want %v", rec.GraceStartedAt, t0)
	}

	// 3. Still within 14 days -> stays grace, timer unchanged.
	t1 := t0.Add(13 * 24 * time.Hour)
	rec = Evaluate(rec, newFP, t1)
	if rec.State != StateGrace {
		t.Fatalf("day 13: state = %v, want grace", rec.State)
	}
	if !rec.GraceStartedAt.Equal(t0) {
		t.Fatalf("grace timer moved: %v, want %v", rec.GraceStartedAt, t0)
	}

	// 4. Exactly at 14 days -> expired_grace.
	t2 := t0.Add(GracePeriod)
	rec = Evaluate(rec, newFP, t2)
	if rec.State != StateExpiredGrace {
		t.Fatalf("day 14: state = %v, want expired_grace", rec.State)
	}

	// 5. Stays expired_grace as time keeps passing.
	t3 := t2.Add(30 * 24 * time.Hour)
	rec = Evaluate(rec, newFP, t3)
	if rec.State != StateExpiredGrace {
		t.Fatalf("day 44: state = %v, want expired_grace", rec.State)
	}

	// 6. Re-issued key for the new fingerprint clears the banner immediately,
	// from any prior state including expired_grace (AC-50b: "installing a
	// re-issued key clears the banner").
	rec.IssuedFingerprint = newFP
	rec = Evaluate(rec, newFP, t3)
	if rec.State != StateValid || rec.GraceStartedAt != nil {
		t.Fatalf("re-issue: got %+v, want valid/no timer", rec)
	}
}

func TestEvaluateRestoreToSameHardwareNeverEntersGrace(t *testing.T) {
	// AC-50b / edge case: restoring a backup onto the exact same machine
	// (same machine-id, same MAC) must never trip grace, regardless of what
	// state was serialized into the restored archive.
	fp := Compose("machine-a", "aa:bb:cc:dd:ee:ff")
	rec := Record{State: StateValid, IssuedFingerprint: fp}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rec = Evaluate(rec, fp, now)
	if rec.State != StateValid || rec.GraceStartedAt != nil {
		t.Fatalf("same-hardware restore: got %+v, want valid/no timer", rec)
	}
}

func TestEvaluateSingleComponentDriftTolerated(t *testing.T) {
	issued := Compose("machine-a", "aa:bb:cc:dd:ee:ff")
	vmCloneNewMAC := Compose("machine-a", "de:ad:be:ef:00:01")
	rec := Record{State: StateValid, IssuedFingerprint: issued}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec = Evaluate(rec, vmCloneNewMAC, now)
	if rec.State != StateValid {
		t.Fatalf("single-component drift: state = %v, want valid (tolerated)", rec.State)
	}
}

func TestGraceExpiresAt(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := Record{State: StateGrace, GraceStartedAt: &t0}
	want := t0.Add(GracePeriod)
	if got := rec.GraceExpiresAt(); !got.Equal(want) {
		t.Errorf("GraceExpiresAt = %v, want %v", got, want)
	}

	valid := Record{State: StateValid}
	if got := valid.GraceExpiresAt(); !got.IsZero() {
		t.Errorf("GraceExpiresAt on valid record = %v, want zero", got)
	}
}
