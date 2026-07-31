package platform

import "testing"

// TestEvalSubscriberCap covers the FR-105 buffer math and the three bands
// (under cap, warn band, blocked) plus the unlimited/absent-license case.
func TestEvalSubscriberCap(t *testing.T) {
	t.Cleanup(func() { SetCachedLicenseMaxSubscribersForTest(0) })

	// Unlimited (cap 0) is never Over/Blocked regardless of count.
	SetCachedLicenseMaxSubscribersForTest(0)
	if st := EvalSubscriberCap(1_000_000); st.Over || st.Blocked || !st.Unlimited {
		t.Fatalf("unlimited should never block: %+v", st)
	}

	// pro tier: cap 1000, buffer = ceil(5%) = 50 → hard wall at 1050.
	SetCachedLicenseMaxSubscribersForTest(1000)
	cases := []struct {
		count           int
		wantOver, block bool
	}{
		{999, false, false},   // under cap
		{1000, false, false},  // exactly at cap
		{1001, true, false},   // warn band
		{1050, true, false},   // top of buffer, still allowed
		{1051, true, true},    // past cap+buffer → blocked
		{5000, true, true},    // far past
	}
	for _, c := range cases {
		st := EvalSubscriberCap(c.count)
		if st.Cap != 1000 || st.Buffer != 50 {
			t.Fatalf("count %d: cap/buffer wrong: %+v", c.count, st)
		}
		if st.Over != c.wantOver || st.Blocked != c.block {
			t.Fatalf("count %d: got over=%v blocked=%v, want over=%v blocked=%v",
				c.count, st.Over, st.Blocked, c.wantOver, c.block)
		}
	}

	// starter tier: cap 200, 5% = 10 → buffer floor is exactly 10.
	SetCachedLicenseMaxSubscribersForTest(200)
	if st := EvalSubscriberCap(211); st.Buffer != 10 || !st.Blocked {
		t.Fatalf("starter buffer/block wrong: %+v", st)
	}
	if st := EvalSubscriberCap(210); st.Blocked {
		t.Fatalf("starter at cap+buffer should be allowed: %+v", st)
	}

	// Small cap: buffer floors at 10 (5% of 50 = 3, floored to 10).
	SetCachedLicenseMaxSubscribersForTest(50)
	if st := EvalSubscriberCap(60); st.Buffer != 10 || st.Blocked {
		t.Fatalf("small-cap buffer floor wrong: %+v", st)
	}
}
