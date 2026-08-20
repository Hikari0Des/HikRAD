package portalapi

// Pure unit tests (no DB/Redis) for FR-167's lowest-level, highest-stakes
// primitives: the fail-closed CIDR matcher, the trusted-proxy-aware source-IP
// resolver, MAC normalization, and the token hard-cap arithmetic. These run
// for real in every sandbox, including one with no reachable Postgres/Redis
// (docs/history/gates records every prior v4 phase hit exactly that
// constraint in this environment) — so this file is the part of the threat
// model that gets exercised unconditionally, not just written and skipped.

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIPInNetworks_EmptyListTrustsNothing(t *testing.T) {
	// The inverted default vs internal/auth's ipAllowed (deliberate — see
	// ipInNetworks' doc comment): an unconfigured allowlist here must refuse
	// everything, not permit everything. This IS gate item 4 ("off by
	// default") at the primitive level.
	if ipInNetworks("10.0.0.5", nil) {
		t.Fatal("empty trusted-network list must trust nothing")
	}
	if ipInNetworks("10.0.0.5", []string{}) {
		t.Fatal("empty trusted-network list must trust nothing")
	}
}

func TestIPInNetworks_MatchAndMiss(t *testing.T) {
	nets := []string{"10.0.0.0/8", "203.0.113.0/24"}
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.1.2.3", true},
		{"203.0.113.9", true},
		{"192.168.1.1", false},
		{"", false},
		{"not-an-ip", false},
	}
	for _, c := range cases {
		if got := ipInNetworks(c.ip, nets); got != c.want {
			t.Errorf("ipInNetworks(%q) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestIPInNetworks_MalformedCIDRFailsClosedForThatEntry(t *testing.T) {
	// A garbage entry must never make its own network match everything, and
	// a good entry alongside it must still work.
	nets := []string{"not-a-cidr", "10.0.0.0/8"}
	if ipInNetworks("203.0.113.1", nets) {
		t.Fatal("malformed CIDR entry must not widen the match")
	}
	if !ipInNetworks("10.5.5.5", nets) {
		t.Fatal("a valid CIDR alongside a malformed one must still match")
	}
}

func TestResolveSourceIP_XFFIgnoredFromUntrustedPeer(t *testing.T) {
	// The whole point of condition 3: a request whose IMMEDIATE peer is not
	// a configured trusted proxy must never have its XFF header honored —
	// otherwise anyone on the internet can claim to be a subscriber's IP.
	r := &http.Request{RemoteAddr: "198.51.100.7:54321", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "10.0.0.5") // attacker-claimed subscriber IP
	got := resolveSourceIP(r, []string{"172.16.0.0/12"})
	if got != "198.51.100.7" {
		t.Fatalf("resolveSourceIP from untrusted peer = %q, want the raw peer address (XFF must be ignored)", got)
	}
}

func TestResolveSourceIP_XFFHonoredFromTrustedProxy(t *testing.T) {
	r := &http.Request{RemoteAddr: "172.16.0.9:443", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "10.0.0.5, 172.16.0.9")
	got := resolveSourceIP(r, []string{"172.16.0.0/12"})
	if got != "10.0.0.5" {
		t.Fatalf("resolveSourceIP from trusted proxy = %q, want first XFF hop 10.0.0.5", got)
	}
}

func TestResolveSourceIP_NoTrustedProxiesConfigured(t *testing.T) {
	// Off-by-default at this layer too: an empty trusted-proxy list must
	// never honor XFF from anyone, even the loopback/deployment's own Caddy.
	r := &http.Request{RemoteAddr: "127.0.0.1:8080", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "10.0.0.5")
	got := resolveSourceIP(r, nil)
	if got != "127.0.0.1" {
		t.Fatalf("resolveSourceIP with no trusted proxies = %q, want raw peer 127.0.0.1", got)
	}
}

func TestNormalizeMAC_FormatsCompareEqual(t *testing.T) {
	forms := []string{"aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff", "AABBCCDDEEFF", "aa:bb:cc:dd:ee:ff host"}
	want := "AABBCCDDEEFF"
	for _, f := range forms {
		if got := normalizeMAC(f); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", f, got, want)
		}
	}
}

func TestIssueAccess_HardCapNeverExceeded(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long!!")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hardCap := now.Add(5 * time.Minute) // shorter than accessTTL (15m)

	tok, err := issueAccess(secret, "sub-1", "sess-1", scopeReduced, now, &hardCap)
	if err != nil {
		t.Fatal(err)
	}
	sub, sid, scope, err := parseAccess(secret, tok)
	if err != nil {
		t.Fatal(err)
	}
	if sub != "sub-1" || sid != "sess-1" || scope != scopeReduced {
		t.Fatalf("roundtrip mismatch: sub=%q sid=%q scope=%q", sub, sid, scope)
	}

	// The token's own exp claim must equal the hard cap, not now+accessTTL —
	// gate item 11's "does not silently refresh", enforced at the token
	// itself, not just checked again at the next refresh call.
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok, claims); err != nil {
		t.Fatal(err)
	}
	expUnix, _ := claims["exp"].(float64)
	if int64(expUnix) != hardCap.Unix() {
		t.Fatalf("token exp = %d, want hard cap %d (now+accessTTL would be %d)",
			int64(expUnix), hardCap.Unix(), now.Add(accessTTL).Unix())
	}

	// A cap LATER than now+accessTTL must not stretch the token past the
	// normal accessTTL either — the cap only ever shortens, never extends.
	farCap := now.Add(2 * time.Hour)
	tok2, err := issueAccess(secret, "sub-1", "sess-1", scopeFull, now, &farCap)
	if err != nil {
		t.Fatal(err)
	}
	claims2 := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok2, claims2); err != nil {
		t.Fatal(err)
	}
	exp2, _ := claims2["exp"].(float64)
	if int64(exp2) != now.Add(accessTTL).Unix() {
		t.Fatalf("token exp with a distant cap = %d, want now+accessTTL = %d", int64(exp2), now.Add(accessTTL).Unix())
	}
}

func TestParseAccess_LegacyTokenNoScopeClaimDefaultsFull(t *testing.T) {
	// A token minted before v4-11 (or by any other future caller that omits
	// "scp") must decode as full scope — the only scope those tokens could
	// ever have meant.
	secret := []byte("test-secret-at-least-32-bytes-long!!")
	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "sub-1", "sid": "sess-1", "typ": tokenTypePortalAccess,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok, err := legacy.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	_, _, scope, err := parseAccess(secret, tok)
	if err != nil {
		t.Fatal(err)
	}
	if scope != scopeFull {
		t.Fatalf("legacy token (no scp claim) decoded scope = %q, want %q", scope, scopeFull)
	}
}
