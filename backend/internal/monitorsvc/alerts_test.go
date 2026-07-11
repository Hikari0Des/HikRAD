package monitorsvc

import (
	"context"
	"testing"
	"time"
)

// Quiet-hours matrix: overnight-wrap and same-day windows, plus the boundary
// instants (start included, end excluded) so a boundary event fires once.
func TestInQuietHours(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 7, 11, h, m, 0, 0, baghdad) }
	cases := []struct {
		name  string
		qh    quietHours
		now   time.Time
		quiet bool
	}{
		{"overnight midnight", quietHours{"22:00", "07:00"}, at(1, 0), true},
		{"overnight evening", quietHours{"22:00", "07:00"}, at(23, 0), true},
		{"overnight daytime", quietHours{"22:00", "07:00"}, at(12, 0), false},
		{"overnight start boundary", quietHours{"22:00", "07:00"}, at(22, 0), true},
		{"overnight end boundary", quietHours{"22:00", "07:00"}, at(7, 0), false},
		{"sameday inside", quietHours{"01:00", "05:00"}, at(3, 0), true},
		{"sameday outside", quietHours{"01:00", "05:00"}, at(6, 0), false},
		{"empty window", quietHours{"", ""}, at(3, 0), false},
		{"equal bounds", quietHours{"09:00", "09:00"}, at(9, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inQuietHours(tc.now, tc.qh); got != tc.quiet {
				t.Fatalf("inQuietHours(%v,%+v) = %v, want %v", tc.now, tc.qh, got, tc.quiet)
			}
		})
	}
}

// Quiet hours suppress the alert-out channels but never in-app.
func TestEffectiveChannels_QuietKeepsInApp(t *testing.T) {
	a := &alertEngine{loc: baghdad, now: func() time.Time { return time.Date(2026, 7, 11, 23, 0, 0, 0, baghdad) }}
	r := rule{
		Channels:   []string{chInApp, chTelegram, chEmail, chWhatsApp},
		QuietHours: &quietHours{"22:00", "07:00"},
	}
	got := a.effectiveChannels(r)
	if len(got) != 1 || got[0] != chInApp {
		t.Fatalf("quiet hours: channels = %v, want [inapp] only", got)
	}

	// Outside quiet hours: all channels pass.
	a.now = func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, baghdad) }
	if got := a.effectiveChannels(r); len(got) != 4 {
		t.Fatalf("daytime: channels = %v, want all 4", got)
	}
}

// Cooldown suppresses a repeat fire within the window, then allows it after.
func TestMemoryCooldown(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cd := newMemoryCooldown(func() time.Time { return now })
	ctx := context.Background()
	if !cd.claim(ctx, "r1", time.Minute) {
		t.Fatal("first claim should proceed")
	}
	if cd.claim(ctx, "r1", time.Minute) {
		t.Fatal("second claim within window should be suppressed")
	}
	// A different rule is independent.
	if !cd.claim(ctx, "r2", time.Minute) {
		t.Fatal("distinct rule should proceed")
	}
	// After the window elapses, the rule fires again.
	now = now.Add(2 * time.Minute)
	if !cd.claim(ctx, "r1", time.Minute) {
		t.Fatal("claim after cooldown should proceed")
	}
}

func TestNumFromThreshold(t *testing.T) {
	r := rule{Threshold: map[string]any{"percent": float64(85)}}
	if got := numFromThreshold(r, "percent", 50); got != 85 {
		t.Fatalf("percent = %v, want 85", got)
	}
	if got := numFromThreshold(r, "missing", 7); got != 7 {
		t.Fatalf("default = %v, want 7", got)
	}
	if !isInvariantRule(rule{Threshold: map[string]any{"invariant": true}}) {
		t.Fatal("invariant rule not detected")
	}
	if isInvariantRule(rule{Threshold: map[string]any{"depth": float64(1000)}}) {
		t.Fatal("depth rule wrongly flagged invariant")
	}
}
