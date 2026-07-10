package accounting

import (
	"os"
	"testing"
)

func TestSpillAppendDrain(t *testing.T) {
	sp, err := newSpill(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sp.close()

	payloads := [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`), []byte(`{"c":3}`)}
	for _, p := range payloads {
		if err := sp.append(p); err != nil {
			t.Fatal(err)
		}
	}
	if sp.pending() != 3 {
		t.Fatalf("pending: got %d want 3", sp.pending())
	}

	var got [][]byte
	drained, bad, err := sp.drain(func(p []byte) error {
		got = append(got, append([]byte(nil), p...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if drained != 3 || bad != 0 {
		t.Fatalf("drain: drained=%d bad=%d", drained, bad)
	}
	if sp.pending() != 0 {
		t.Fatalf("pending after drain: got %d", sp.pending())
	}
	for i, p := range got {
		if string(p) != string(payloads[i]) {
			t.Fatalf("order/content mismatch at %d: %q", i, p)
		}
	}
}

func TestSpillCorruptLineSkipped(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpill(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.append([]byte(`{"good":1}`)); err != nil {
		t.Fatal(err)
	}
	sp.close()

	// Corrupt the tail as an unclean shutdown might: a bad CRC and a torn line.
	f, err := os.OpenFile(sp.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("999999\tnot-base64!!!\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	sp2, err := newSpill(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sp2.close()

	var got int
	drained, bad, err := sp2.drain(func(p []byte) error { got++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if drained != 1 || got != 1 {
		t.Fatalf("expected 1 good record, drained=%d got=%d", drained, got)
	}
	if bad != 1 {
		t.Fatalf("expected 1 corrupt line skipped, bad=%d", bad)
	}
}

func TestSpillDisabledWithoutDir(t *testing.T) {
	sp, err := newSpill("")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.append([]byte(`x`)); err == nil {
		t.Fatal("expected append to fail when spill is disabled")
	}
	if sp.pending() != 0 {
		t.Fatalf("pending: got %d", sp.pending())
	}
}

func TestSpillDrainStopsOnPushError(t *testing.T) {
	sp, err := newSpill(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sp.close()
	for i := 0; i < 3; i++ {
		if err := sp.append([]byte(`{"n":1}`)); err != nil {
			t.Fatal(err)
		}
	}
	// A push failure must leave the WAL intact for a retry (nothing lost).
	drained, _, err := sp.drain(func(p []byte) error { return os.ErrClosed })
	if err == nil {
		t.Fatal("expected push error to propagate")
	}
	if drained != 0 {
		t.Fatalf("drained=%d, want 0 before failure", drained)
	}
	if sp.pending() != 3 {
		t.Fatalf("pending after failed drain: got %d want 3", sp.pending())
	}
}
