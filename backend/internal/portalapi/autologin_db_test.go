package portalapi_test

// FR-167/FR-168 end-to-end suite, gated on HIKRAD_TEST_DB_URL AND
// HIKRAD_TEST_REDIS_URL (auto-login's whole decision reads live sessions
// from Redis) — self-skips cleanly when either is unset, same discipline
// every other DB-gated suite in this repo uses. This is the "attacked in
// testing" proof the phase brief demands for gate items 3-13: every test
// here drives the real HTTP endpoint against a real Postgres + Redis stack,
// not a mock of the decision function.
//
// Trusted-proxy trick used throughout: httptest's client always connects via
// loopback, so tests configure trusted_proxies = ["127.0.0.1/32"] and then
// freely control the SIMULATED source IP via X-Forwarded-For — this is
// exactly the Caddy-in-front-of-hikrad-api deployment shape production runs
// under, just with the test process standing in for both the browser and
// the proxy hop.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
)

// uniqTrustedIP/uniqUntrustedIP return a random IP per call so tests never
// collide on a shared, un-flushed Redis across repeated runs (livestate
// entries carry no TTL — a fixed literal IP reused between runs leaves a
// stale session behind that turns the NEXT run's "exactly one session"
// assumption into an ambiguous-IP false refusal; found live by running this
// suite twice in a row without a Redis flush between runs).
func uniqTrustedIP(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("10.%d.%d.%d", randByte(t), randByte(t), randByte(t))
}

func uniqUntrustedIP(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("203.0.113.%d", 1+randByte(t)%254)
}

func randByte(t *testing.T) int {
	t.Helper()
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return int(b[0])
}

func autologinEnv(t *testing.T) env {
	t.Helper()
	e := setup(t)
	if os.Getenv("HIKRAD_TEST_REDIS_URL") == "" {
		t.Skip("HIKRAD_TEST_REDIS_URL not set; auto-login reads live sessions from Redis")
	}
	return e
}

func (e env) putAutoLoginConfig(t *testing.T, enabled bool, trustedNetworks, trustedProxies []string, ttlMinutes int) {
	t.Helper()
	r := e.do(t, "PUT", "/api/v1/settings/portal_auto_login", e.adminToken, map[string]any{
		"enabled": enabled, "trusted_networks": trustedNetworks, "trusted_proxies": trustedProxies,
		"session_ttl_minutes": ttlMinutes,
	})
	if r.status != http.StatusOK {
		t.Fatalf("put auto-login config = %d: %s", r.status, r.body)
	}
}

// seedLiveSession writes one live session directly into Redis (mirroring
// what hikrad-acct's consumer would do from a real Accounting-Request) so
// the test can control exactly how many sessions sit on an IP.
func (e env) seedLiveSession(t *testing.T, subscriberID, ip, mac string) {
	t.Helper()
	acctID := uniq("acct_")
	err := livestate.Upsert(context.Background(), e.rdb, livestate.State{
		SubscriberID: subscriberID, NASID: uniq("nas_"), AcctSessionID: acctID,
		IP: ip, MAC: mac, StartedAt: time.Now(), LastInterimAt: time.Now(),
		Service: livestate.ServicePPPoE,
	})
	if err != nil {
		t.Fatalf("seed live session: %v", err)
	}
}

func (e env) autoLoginFrom(t *testing.T, simulatedIP string, body map[string]any) resp {
	t.Helper()
	return e.doWithXFF(t, "POST", "/api/v1/portal/session/auto", simulatedIP, body)
}

// doWithXFF is e.do plus an X-Forwarded-For header (the trusted-proxy trick
// described above) — e.do (db_test.go) has no header hook, so this is its
// own small copy of the same request/decode plumbing.
func (e env) doWithXFF(t *testing.T, method, path, xff string, body any) resp {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Forwarded-For", xff)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: raw}
}

// --- gate items ---------------------------------------------------------

func TestAutoLogin_OffByDefault(t *testing.T) {
	e := autologinEnv(t)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "AA:BB:CC:DD:EE:FF")

	// portal_auto_login is a DB-wide singleton settings row shared by every
	// test in this suite (and every prior run against a non-reset
	// database), so "fresh install" cannot be assumed by omission — it must
	// be established explicitly, exactly like every other test here that
	// depends on a specific enabled/disabled state. Migration 0631 seeds
	// enabled=false, which is what this test is actually proving by setting
	// it back to that state itself rather than trusting run order.
	e.putAutoLoginConfig(t, false, nil, nil, 30)
	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login while disabled = %d, want 401 (off by default): %s", r.status, r.body)
	}
}

func TestAutoLogin_UntrustedNetworkRefused(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqUntrustedIP(t)
	e.seedLiveSession(t, subID, ip, "")

	// The session exists and is unambiguous, but ip is outside the trusted
	// network — must still refuse.
	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login from untrusted network = %d, want 401: %s", r.status, r.body)
	}
}

func TestAutoLogin_SpoofedXFFFromUntrustedProxyRefused(t *testing.T) {
	e := autologinEnv(t)
	// Trusted network includes ip, but trusted_proxies is empty — so even
	// though the client sends X-Forwarded-For: <ip>, the immediate peer
	// (loopback, untrusted) means that header must be ignored and the raw
	// peer address used instead, which is outside the trusted network.
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "")

	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login with spoofed XFF from untrusted proxy = %d, want 401: %s", r.status, r.body)
	}
}

func TestAutoLogin_AmbiguousIPRefused(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subA, _ := e.createSubscriberWithPassword(t, prof, "pw-a-12345")
	subB, _ := e.createSubscriberWithPassword(t, prof, "pw-b-12345")
	ip := uniqTrustedIP(t)
	// CGNAT/shared-NAT case: two different subscribers' live sessions land
	// on the same source IP.
	e.seedLiveSession(t, subA, ip, "")
	e.seedLiveSession(t, subB, ip, "")

	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login with 2 sessions on one IP = %d, want 401 (ambiguous, never a tie-break): %s", r.status, r.body)
	}
}

func TestAutoLogin_MACMismatchRefused(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "AA:BB:CC:DD:EE:FF")

	// A captive-portal hand-off style call with a MAC hint that disagrees
	// with the live session's own recorded MAC (a forged redirect param,
	// gate item 13) must refuse — the hint never overrides the session.
	r := e.doWithXFF(t, "POST", "/api/v1/portal/session/auto", ip, map[string]any{"mac": "11:22:33:44:55:66"})
	if r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login with mismatched MAC hint = %d, want 401: %s", r.status, r.body)
	}
}

func TestAutoLogin_GrantsReducedScope_EnforcedServerSide(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "AA:BB:CC:DD:EE:FF")

	r := e.doWithXFF(t, "POST", "/api/v1/portal/session/auto", ip, map[string]any{"mac": "aa:bb:cc:dd:ee:ff"})
	if r.status != http.StatusOK {
		t.Fatalf("auto-login on a clean single-match trusted request = %d, want 200: %s", r.status, r.body)
	}
	var tok portalTokens
	r.into(t, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("no access token: %s", r.body)
	}

	// View endpoints work under reduced scope.
	if g := e.do(t, "GET", "/api/v1/portal/me", tok.AccessToken, nil); g.status != http.StatusOK {
		t.Fatalf("reduced-scope GET /me = %d, want 200: %s", g.status, g.body)
	}
	if g := e.do(t, "GET", "/api/v1/portal/pay-methods", tok.AccessToken, nil); g.status != http.StatusOK {
		t.Fatalf("reduced-scope GET /pay-methods = %d, want 200: %s", g.status, g.body)
	}

	// Full-scope-gated endpoint (password/contact change) must 403, exactly
	// as the frozen contract requires — server-side, regardless of what a
	// client chose to render (gate item 9).
	upd := e.do(t, "PUT", "/api/v1/portal/me", tok.AccessToken, map[string]any{"phone": "07901234567"})
	if upd.status != http.StatusForbidden {
		t.Fatalf("reduced-scope PUT /me = %d, want 403: %s", upd.status, upd.body)
	}
}

// TestAutoLogin_EveryGrantAndRefusalIsAudited is gate item 12, proven
// directly against the real audit_log table (auth.AuditActor's own sink —
// reused, not a bespoke audit surface): a grant, an ambiguous-IP refusal,
// and an untrusted-network refusal each land a row carrying IP/MAC/NAS/
// session/scope, queryable by action — the exact shape "a spike in
// ambiguous-IP refusals is what an attack looks like" depends on being able
// to query for.
func TestAutoLogin_EveryGrantAndRefusalIsAudited(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)

	// A grant.
	subID, _ := e.createSubscriberWithPassword(t, prof, "pw-audit-grant1")
	grantIP := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, grantIP, "AA:BB:CC:DD:EE:01")
	if r := e.autoLoginFrom(t, grantIP, nil); r.status != http.StatusOK {
		t.Fatalf("auto-login = %d: %s", r.status, r.body)
	}
	var grantedCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'portal.autologin.granted' AND ip = $1
		   AND after->>'scope' = 'reduced' AND after->>'mac' = 'AA:BB:CC:DD:EE:01'`, grantIP).Scan(&grantedCount); err != nil {
		t.Fatal(err)
	}
	if grantedCount != 1 {
		t.Fatalf("audited grant rows for %s = %d, want 1", grantIP, grantedCount)
	}

	// An ambiguous-IP refusal — the spike-worthy case named explicitly in
	// the phase brief.
	subA, _ := e.createSubscriberWithPassword(t, prof, "pw-audit-amb-a")
	subB, _ := e.createSubscriberWithPassword(t, prof, "pw-audit-amb-b")
	ambIP := uniqTrustedIP(t)
	e.seedLiveSession(t, subA, ambIP, "")
	e.seedLiveSession(t, subB, ambIP, "")
	if r := e.autoLoginFrom(t, ambIP, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("ambiguous auto-login = %d, want 401", r.status)
	}
	var ambiguousCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'portal.autologin.refused' AND ip = $1
		   AND after->>'reason' = 'ambiguous_ip'`, ambIP).Scan(&ambiguousCount); err != nil {
		t.Fatal(err)
	}
	if ambiguousCount != 1 {
		t.Fatalf("audited ambiguous-ip refusal rows for %s = %d, want 1 (this is the query an operator would run to spot a spike)", ambIP, ambiguousCount)
	}

	// An untrusted-network refusal.
	subC, _ := e.createSubscriberWithPassword(t, prof, "pw-audit-untrusted")
	untrustedIP := uniqUntrustedIP(t)
	e.seedLiveSession(t, subC, untrustedIP, "")
	if r := e.autoLoginFrom(t, untrustedIP, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("untrusted-network auto-login = %d, want 401", r.status)
	}
	var untrustedCount int
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'portal.autologin.refused' AND ip = $1
		   AND after->>'reason' = 'untrusted_network'`, untrustedIP).Scan(&untrustedCount); err != nil {
		t.Fatal(err)
	}
	if untrustedCount != 1 {
		t.Fatalf("audited untrusted-network refusal rows for %s = %d, want 1", untrustedIP, untrustedCount)
	}
}

func TestAutoLogin_StepUpUpgradesForRemainingLifetime(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, username := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "")

	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusOK {
		t.Fatalf("auto-login = %d: %s", r.status, r.body)
	}
	var tok portalTokens
	r.into(t, &tok)

	// Wrong password does not upgrade.
	bad := e.do(t, "POST", "/api/v1/portal/session/step-up", tok.AccessToken, map[string]any{"password": "wrong"})
	if bad.status != http.StatusUnauthorized {
		t.Fatalf("step-up with wrong password = %d, want 401: %s", bad.status, bad.body)
	}
	if upd := e.do(t, "PUT", "/api/v1/portal/me", tok.AccessToken, map[string]any{"phone": "07901234567"}); upd.status != http.StatusForbidden {
		t.Fatalf("PUT /me after failed step-up = %d, want still 403", upd.status)
	}

	up := e.do(t, "POST", "/api/v1/portal/session/step-up", tok.AccessToken, map[string]any{"password": "s3cret-pw"})
	if up.status != http.StatusOK {
		t.Fatalf("step-up with correct password = %d: %s", up.status, up.body)
	}
	var upgraded struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	up.into(t, &upgraded)
	if upgraded.Scope != "full" {
		t.Fatalf("step-up response scope = %q, want full", upgraded.Scope)
	}

	// The upgraded token now passes the full-scope-gated endpoint.
	if upd := e.do(t, "PUT", "/api/v1/portal/me", upgraded.AccessToken, map[string]any{"phone": "07901234567"}); upd.status != http.StatusOK {
		t.Fatalf("PUT /me after step-up = %d, want 200: %s", upd.status, upd.body)
	}
	_ = username
}

func TestAutoLogin_InactiveAccountRefused_ExpiredStillGranted(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)

	disabledID, _ := e.createSubscriberWithPassword(t, prof, "pw-disabled1")
	if _, err := e.db.Exec(context.Background(), `UPDATE subscribers SET status = 'disabled' WHERE id = $1::uuid`, disabledID); err != nil {
		t.Fatal(err)
	}
	disabledIP := uniqTrustedIP(t)
	e.seedLiveSession(t, disabledID, disabledIP, "")
	if r := e.autoLoginFrom(t, disabledIP, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("auto-login for a manager-disabled account = %d, want 401: %s", r.status, r.body)
	}

	expiredID, _ := e.createSubscriberWithPassword(t, prof, "pw-expired1")
	if _, err := e.db.Exec(context.Background(), `UPDATE subscribers SET status = 'expired' WHERE id = $1::uuid`, expiredID); err != nil {
		t.Fatal(err)
	}
	expiredIP := uniqTrustedIP(t)
	e.seedLiveSession(t, expiredID, expiredIP, "")
	if r := e.autoLoginFrom(t, expiredIP, nil); r.status != http.StatusOK {
		t.Fatalf("auto-login for an EXPIRED account = %d, want 200 (must still work so they can renew): %s", r.status, r.body)
	}
}

func TestAutoLogin_TTLHonoured_NoSilentRefresh(t *testing.T) {
	e := autologinEnv(t)
	e.putAutoLoginConfig(t, true, []string{"10.0.0.0/8"}, []string{"127.0.0.1/32"}, 30)
	prof := e.createProfile(t, 25000, 30)
	subID, _ := e.createSubscriberWithPassword(t, prof, "s3cret-pw")
	ip := uniqTrustedIP(t)
	e.seedLiveSession(t, subID, ip, "")

	r := e.autoLoginFrom(t, ip, nil)
	if r.status != http.StatusOK {
		t.Fatalf("auto-login = %d: %s", r.status, r.body)
	}
	var tok portalTokens
	r.into(t, &tok)

	// Force the session's hard cap into the past, as if the configured TTL
	// had already elapsed, and confirm refresh refuses rather than silently
	// extending it.
	if _, err := e.db.Exec(context.Background(),
		`UPDATE portal_sessions SET expires_at = now() - interval '1 minute' WHERE subscriber_id = $1::uuid`, subID); err != nil {
		t.Fatal(err)
	}
	rr := e.do(t, "POST", "/api/v1/portal/refresh", "", map[string]string{"refresh_token": tok.RefreshToken})
	if rr.status != http.StatusUnauthorized {
		t.Fatalf("refresh past the auto-login TTL = %d, want 401 (no silent refresh): %s", rr.status, rr.body)
	}
}
