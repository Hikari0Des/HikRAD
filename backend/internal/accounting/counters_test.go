package accounting

import "testing"

func TestCounterInvariant(t *testing.T) {
	var c counters
	c.received.Store(100)
	c.deduplicated.Store(10)
	c.persisted.Store(85)

	// received - deduplicated - in_queue == persisted → 100-10-5 == 85.
	if s := c.snapshot(5); !s.InvariantOK {
		t.Fatalf("expected invariant to hold, got %+v", s)
	}
	// A leak (in_queue too low for what is unaccounted) breaks it.
	if s := c.snapshot(4); s.InvariantOK {
		t.Fatalf("expected invariant to fail, got %+v", s)
	}
}

func TestCounterSnapshotFields(t *testing.T) {
	var c counters
	c.received.Store(3)
	c.enqueued.Store(2)
	c.spilled.Store(1)
	c.drained.Store(1)
	c.persisted.Store(2)
	c.deduplicated.Store(1)
	c.reaped.Store(4)
	c.orphanStops.Store(5)
	s := c.snapshot(0)
	if s.Received != 3 || s.Enqueued != 2 || s.Spilled != 1 || s.Drained != 1 ||
		s.Persisted != 2 || s.Deduplicated != 1 || s.Reaped != 4 || s.OrphanStops != 5 {
		t.Fatalf("snapshot mismatch: %+v", s)
	}
}
