package accounting

import (
	"testing"
	"time"
)

func TestTotalBytes(t *testing.T) {
	if got := totalBytes(100, 0); got != 100 {
		t.Fatalf("no gigawords: got %d", got)
	}
	// 2 gigawords + 100 octets.
	want := uint64(2)<<32 | 100
	if got := totalBytes(100, 2); got != want {
		t.Fatalf("gigawords: got %d want %d", got, want)
	}
	// Octets are masked to 32 bits (a NAS that leaves high bits set).
	if got := totalBytes(0x1_0000_0064, 1); got != (uint64(1)<<32|0x64) {
		t.Fatalf("octet mask: got %d", got)
	}
}

func TestAdvanceNormal(t *testing.T) {
	newTotal, delta := advance(1000, 1500, 0)
	if newTotal != 1500 || delta != 500 {
		t.Fatalf("normal: newTotal=%d delta=%d", newTotal, delta)
	}
}

func TestAdvanceGigawords(t *testing.T) {
	last := uint64(4_000_000_000) // ~ just under 2^32
	// Next record: gigawords=1, low word small → 2^32 + 200.
	newTotal, delta := advance(last, 200, 1)
	want := uint64(1)<<32 | 200
	if newTotal != want {
		t.Fatalf("gigaword total: got %d want %d", newTotal, want)
	}
	if delta != want-last {
		t.Fatalf("gigaword delta: got %d want %d", delta, want-last)
	}
}

func TestAdvanceWrapWithoutGigawords(t *testing.T) {
	// Low word was high (near 2^32), new low word is small, no gigawords → the
	// NAS wrapped the 32-bit counter without reporting gigawords.
	last := uint64(0xFFFF_FF00)
	newTotal, delta := advance(last, 0x100, 0)
	wantTotal := uint64(1)<<32 | 0x100
	if newTotal != wantTotal {
		t.Fatalf("wrap total: got %d want %d", newTotal, wantTotal)
	}
	if delta != wantTotal-last {
		t.Fatalf("wrap delta: got %d want %d", delta, wantTotal-last)
	}
}

func TestAdvanceReset(t *testing.T) {
	// Both last and new are small: a genuine counter reset, not a wrap → count
	// the new total from zero rather than emit a negative/huge delta.
	newTotal, delta := advance(5000, 100, 0)
	if newTotal != 100 || delta != 100 {
		t.Fatalf("reset: newTotal=%d delta=%d", newTotal, delta)
	}
}

func TestParseEventTime(t *testing.T) {
	fallback := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	// Unix seconds.
	got := parseEventTime("1720612800", fallback)
	if got.Unix() != 1720612800 {
		t.Fatalf("unix: got %v", got)
	}
	// FreeRADIUS date rendering.
	got = parseEventTime("Jul 10 2026 14:23:01 UTC", fallback)
	if got.Year() != 2026 || got.Month() != time.July || got.Day() != 10 || got.Hour() != 14 {
		t.Fatalf("date: got %v", got)
	}
	// Empty → fallback.
	if got := parseEventTime("", fallback); !got.Equal(fallback) {
		t.Fatalf("empty: got %v want %v", got, fallback)
	}
	// Garbage → fallback.
	if got := parseEventTime("not-a-time", fallback); !got.Equal(fallback) {
		t.Fatalf("garbage: got %v", got)
	}
}

func TestParseRecordValidation(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	if _, err := parseRecord([]byte(`{"record_type":"start"}`), now); err == nil {
		t.Fatal("expected error for missing acct_session_id")
	}
	if _, err := parseRecord([]byte(`{"record_type":"bogus","acct_session_id":"x"}`), now); err == nil {
		t.Fatal("expected error for bad record_type")
	}
	if _, err := parseRecord([]byte(`{bad json`), now); err == nil {
		t.Fatal("expected error for bad json")
	}
	r, err := parseRecord([]byte(`{"record_type":"Interim","acct_session_id":"abc","event_time":"1720612800"}`), now)
	if err != nil {
		t.Fatalf("valid record: %v", err)
	}
	if r.RecordType != RecordInterim {
		t.Fatalf("record_type not normalized: %q", r.RecordType)
	}
	if r.ReceiptTime.IsZero() || r.EventTimeParsed.Unix() != 1720612800 {
		t.Fatalf("stamps not set: receipt=%v event=%v", r.ReceiptTime, r.EventTimeParsed)
	}
}

func TestRates(t *testing.T) {
	from := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Second)
	// 1250 bytes over 10 s = 1250*8/10 = 1000 bps.
	down, up := rates(1250, 0, from, to)
	if down != 1000 || up != 0 {
		t.Fatalf("rates: down=%d up=%d", down, up)
	}
	// Non-positive interval → 0, no divide-by-zero.
	if down, up := rates(1000, 1000, to, from); down != 0 || up != 0 {
		t.Fatalf("neg interval: down=%d up=%d", down, up)
	}
}
