package radius

import (
	"strings"
	"testing"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

func TestQuoteSecret(t *testing.T) {
	cases := map[string]string{
		`simple`:     `"simple"`,
		`with"quote`: `"with\"quote"`,
		`back\slash`: `"back\\slash"`,
	}
	for in, want := range cases {
		if got := quoteSecret(in); got != want {
			t.Fatalf("quoteSecret(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIntentConstantsInSync guards FR-17's vendor boundary: the radius core's
// abstract Intent strings must equal the vendor package's, since the adapter
// switches on the literals.
func TestIntentConstantsInSync(t *testing.T) {
	pairs := [][2]string{
		{string(IntentRateLimit), vendor.IntentRateLimit},
		{string(IntentAddressPool), vendor.IntentAddressPool},
		{string(IntentSessionTimeout), vendor.IntentSessionTimeout},
		{string(IntentRedirectExpired), vendor.IntentRedirectExpired},
		{string(IntentStaticIP), vendor.IntentStaticIP},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Fatalf("intent mismatch: radius %q vs vendor %q", p[0], p[1])
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"aa:bb:cc:dd:ee:ff":      "AABBCCDDEEFF",
		"AA-BB-CC-DD-EE-FF":      "AABBCCDDEEFF",
		"aabb.ccdd.eeff":         "AABBCCDDEEFF",
		"AA:BB:CC:DD:EE:FF host": "AABBCCDDEEFF",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeMAC(in); got != want {
			t.Fatalf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalIP(t *testing.T) {
	if canonicalIP("10.0.0.1") != "10.0.0.1" {
		t.Fatal("ipv4 canonical")
	}
	if !strings.Contains(canonicalIP("::1"), ":") {
		t.Fatal("ipv6 canonical")
	}
}
