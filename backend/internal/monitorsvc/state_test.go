package monitorsvc

import "testing"

// The state machine is the reachability core: 4 consecutive misses flip down
// (edge once), the first success flips up (edge once). These cases mirror the
// gate item 6/8 flap + recovery scenarios with no network.

func TestState_DownAfterFourMisses_EdgeOnce(t *testing.T) {
	var s targetState
	// First three misses: no edge, still not down.
	for i := 0; i < downThreshold-1; i++ {
		if tr := s.observe(false); tr.toDown || tr.toUp {
			t.Fatalf("miss %d: unexpected edge %+v", i+1, tr)
		}
		if s.status == statusDown {
			t.Fatalf("miss %d: down too early", i+1)
		}
	}
	// Fourth miss crosses the threshold → down edge exactly once.
	tr := s.observe(false)
	if !tr.toDown {
		t.Fatal("4th miss: expected toDown edge")
	}
	if s.status != statusDown {
		t.Fatalf("status = %q, want down", s.status)
	}
	// Further misses while down: no repeated edge (no alert storm).
	if tr := s.observe(false); tr.toDown {
		t.Fatal("5th miss: down edge fired twice")
	}
}

func TestState_RecoveryEdgeOnce(t *testing.T) {
	var s targetState
	for i := 0; i < downThreshold; i++ {
		s.observe(false)
	}
	if s.status != statusDown {
		t.Fatal("precondition: should be down")
	}
	tr := s.observe(true)
	if !tr.toUp {
		t.Fatal("recovery: expected toUp edge")
	}
	if s.status != statusUp {
		t.Fatalf("status = %q, want up", s.status)
	}
	// Continued success: no repeated up edge.
	if tr := s.observe(true); tr.toUp {
		t.Fatal("second success fired up edge again")
	}
}

func TestState_FlapBelowThresholdNeverDowns(t *testing.T) {
	var s targetState
	// miss, miss, miss, success, miss, miss, miss, success — never 4 in a row.
	seq := []bool{false, false, false, true, false, false, false, true}
	for i, ok := range seq {
		tr := s.observe(ok)
		if tr.toDown {
			t.Fatalf("step %d: flapped to down without 4 consecutive misses", i)
		}
	}
	if s.status == statusDown {
		t.Fatal("ended down despite never missing 4 in a row")
	}
}

func TestState_FirstSuccessNoUpEdge(t *testing.T) {
	var s targetState
	// A brand-new target that succeeds first should settle up with no toUp edge
	// (there was no prior down to recover from).
	if tr := s.observe(true); tr.toUp {
		t.Fatal("first-ever success should not fire a recovery edge")
	}
	if s.status != statusUp {
		t.Fatalf("status = %q, want up", s.status)
	}
}
