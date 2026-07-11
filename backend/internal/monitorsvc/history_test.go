package monitorsvc

import (
	"testing"
	"time"
)

// downtimeFromSeries must reconstruct outage windows from ICMP samples using the
// same state machine as live detection, so the status page log matches when
// alerts actually fired.
func TestDowntimeFromSeries(t *testing.T) {
	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	tick := func(n int) time.Time { return base.Add(time.Duration(n) * 15 * time.Second) }
	icmp := func(n int, ok bool) probeSample { return probeSample{At: tick(n), Kind: "icmp", OK: ok} }

	// up*2, then 5 misses (down at the 4th miss = sample index 5), then recovery.
	series := []probeSample{
		icmp(0, true), icmp(1, true),
		icmp(2, false), icmp(3, false), icmp(4, false), icmp(5, false), icmp(6, false),
		icmp(7, true), icmp(8, true),
	}
	end := tick(9)
	windows := downtimeFromSeries(series, end)
	if len(windows) != 1 {
		t.Fatalf("expected 1 downtime window, got %d: %+v", len(windows), windows)
	}
	w := windows[0]
	// Down edge fires at the 4th consecutive miss (index 5); recovery at index 7.
	if !w.From.Equal(tick(5)) {
		t.Fatalf("window.From = %v, want %v", w.From, tick(5))
	}
	if w.To == nil || !w.To.Equal(tick(7)) {
		t.Fatalf("window.To = %v, want %v", w.To, tick(7))
	}
	if w.Seconds != 30 {
		t.Fatalf("window.Seconds = %d, want 30", w.Seconds)
	}
}

func TestDowntimeFromSeries_StillDown(t *testing.T) {
	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	tick := func(n int) time.Time { return base.Add(time.Duration(n) * 15 * time.Second) }
	var series []probeSample
	for i := 0; i < 6; i++ {
		series = append(series, probeSample{At: tick(i), Kind: "icmp", OK: false})
	}
	end := tick(10)
	windows := downtimeFromSeries(series, end)
	if len(windows) != 1 {
		t.Fatalf("expected 1 open window, got %d", len(windows))
	}
	if windows[0].To != nil {
		t.Fatal("still-down window should have nil To")
	}
}

// SNMP-only samples must be ignored by the ICMP-driven downtime reconstruction.
func TestDowntimeFromSeries_IgnoresSNMP(t *testing.T) {
	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	series := []probeSample{
		{At: base, Kind: "snmp", OK: false},
		{At: base.Add(time.Second), Kind: "snmp", OK: false},
	}
	if w := downtimeFromSeries(series, base.Add(time.Minute)); len(w) != 0 {
		t.Fatalf("snmp rows should not create downtime windows: %+v", w)
	}
}
