package subscribers

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"07701234567", "+9647701234567", true},    // local trunk form
		{"+9647701234567", "+9647701234567", true}, // international
		{"009647701234567", "+9647701234567", true},
		{"0770 123 4567", "+9647701234567", true}, // separators
		{"+964 770-123-4567", "+9647701234567", true},
		{"٠٧٧٠١٢٣٤٥٦٧", "+9647701234567", true}, // Eastern-Arabic digits
		{"", "", true},                 // optional / empty
		{"   ", "", true},              // whitespace-only
		{"12345", "", false},           // too short
		{"06701234567", "", false},     // not a 7-prefixed mobile
		{"+1 555 123 4567", "", false}, // non-Iraqi
	}
	for _, c := range cases {
		got, ok := normalizePhone(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizePhone(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRateString(t *testing.T) {
	cases := []struct {
		up, down int
		want     string
	}{
		{10240, 10240, "10M/10M"},
		{2048, 10240, "2M/10M"}, // asymmetric: rx(up)/tx(down)
		{0, 0, ""},
		{512, 1024, "512k/1M"},
		{25600, 25600, "25M/25M"},
	}
	for _, c := range cases {
		if got := rateString(c.up, c.down); got != c.want {
			t.Errorf("rateString(%d,%d) = %q, want %q", c.up, c.down, got, c.want)
		}
	}
}
