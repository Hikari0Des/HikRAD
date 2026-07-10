package radius

import "testing"

func TestRangeSize(t *testing.T) {
	cases := map[string]int64{
		"10.0.0.0/24": 256,
		"10.0.0.0/30": 4,
		"10.0.0.5":    1,
		"10.0.0.5/32": 1,
		"bogus":       0,
	}
	for in, want := range cases {
		if got := rangeSize(in); got != want {
			t.Fatalf("rangeSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPoolSize(t *testing.T) {
	if got := poolSize([]string{"10.0.0.0/24", "10.0.1.0/24"}); got != 512 {
		t.Fatalf("poolSize = %d, want 512", got)
	}
}

func TestValidateRanges(t *testing.T) {
	if _, err := validateRanges([]string{"10.0.0.0/24", "192.168.1.1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := validateRanges(nil); err == nil {
		t.Fatal("expected error for empty ranges")
	}
	if _, err := validateRanges([]string{"nope"}); err == nil {
		t.Fatal("expected error for invalid range")
	}
}

func TestStaticIPInPool(t *testing.T) {
	ranges := []string{"10.0.0.0/24", "192.168.5.7"}
	if !staticIPInPool("10.0.0.55", ranges) {
		t.Fatal("10.0.0.55 should be in 10.0.0.0/24")
	}
	if !staticIPInPool("192.168.5.7", ranges) {
		t.Fatal("exact IP should match")
	}
	if staticIPInPool("10.0.1.1", ranges) {
		t.Fatal("10.0.1.1 should not be in pool")
	}
}

func TestPoolViewUtilization(t *testing.T) {
	p := poolRow{Name: "active", Ranges: []string{"10.0.0.0/24"}, Purpose: "active"}

	// 200/256 = 78.125% — under the 90% exhaustion threshold.
	under := p.view(200)
	if under.Size != 256 {
		t.Fatalf("size = %d", under.Size)
	}
	if under.UtilPercent != 78.13 {
		t.Fatalf("util = %v, want 78.13", under.UtilPercent)
	}
	if under.Exhausted {
		t.Fatal("78% must not be exhausted")
	}

	// 240/256 = 93.75% — over the threshold.
	over := p.view(240)
	if !over.Exhausted {
		t.Fatalf("93.75%% should be exhausted (util=%v)", over.UtilPercent)
	}
}
