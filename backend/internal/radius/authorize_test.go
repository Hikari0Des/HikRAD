package radius

// Policy-engine unit tests: every branch of the authorize decision (FR-5/9/10/58,
// key flow 1) plus the C4 HTTP validation surface. The engine's external seams
// (D's AuthView provider, C's live counter, the NAS registry, password decrypt)
// are all faked here so the suite runs with no DB/Redis.

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeProvider is an in-memory PolicyProvider (D's C4 read-model).
type fakeProvider struct {
	views   map[string]AuthView
	learned map[string]string
}

func (f *fakeProvider) GetAuthView(_ context.Context, username string) (AuthView, error) {
	v, ok := f.views[username]
	if !ok {
		return AuthView{}, ErrNoSubscriber
	}
	return v, nil
}

func (f *fakeProvider) LearnMac(_ context.Context, subscriberID, mac string) error {
	if f.learned == nil {
		f.learned = map[string]string{}
	}
	f.learned[subscriberID] = mac
	return nil
}

// testEnv wires the global seams for a test and returns an engine using an
// identity decrypt (PasswordEnc holds cleartext) and a fixed clock.
type testEnv struct {
	eng      *engine
	provider *fakeProvider
	live     map[[2]string]int // (subID, service) -> count
}

func newTestEnv(t *testing.T, knownIPs ...string) *testEnv {
	t.Helper()
	env := &testEnv{
		provider: &fakeProvider{views: map[string]AuthView{}, learned: map[string]string{}},
		live:     map[[2]string]int{},
	}
	SetPolicyProvider(env.provider)
	SetLiveCounter(func(sub, svc string) int { return env.live[[2]string{sub, svc}] })
	t.Cleanup(func() {
		SetPolicyProvider(nil)
		SetLiveCounter(func(string, string) int { return 0 })
	})
	known := map[string]bool{}
	for _, ip := range knownIPs {
		known[canonicalIP(ip)] = true
	}
	env.eng = &engine{
		rdb:      nil,
		log:      discardLogger(),
		now:      func() time.Time { return fixedNow },
		decrypt:  func(b []byte) ([]byte, error) { return b, nil },
		nasKnown: func(_ context.Context, ip string) (bool, error) { return known[canonicalIP(ip)], nil },
	}
	return env
}

func (env *testEnv) add(username string, v AuthView) { env.provider.views[username] = v }

func baseView(sub, pass string) AuthView {
	return AuthView{
		SubscriberID: sub,
		PasswordEnc:  []byte(pass),
		Status:       "active",
		RateLimit:    "10M/10M",
		SessionLimit: 1,
		MacLockMode:  "off",
	}
}

func mustDecide(t *testing.T, env *testEnv, req authorizeRequest) authorizeResponse {
	t.Helper()
	resp, err := env.eng.decide(context.Background(), req)
	if err != nil {
		t.Fatalf("decide error: %v", err)
	}
	return resp
}

func papReq(user, pass string) authorizeRequest {
	return authorizeRequest{Username: user, Password: pass, NasIP: "10.0.0.1", Service: "pppoe"}
}

func attrMap(resp authorizeResponse) map[string]string {
	out := map[string]string{}
	for _, a := range resp.Attributes {
		out[a.Intent] = a.Value
	}
	return out
}

func assertReject(t *testing.T, resp authorizeResponse, reason string) {
	t.Helper()
	if resp.Action != "reject" || resp.Reason != reason {
		t.Fatalf("got %+v, want reject/%s", resp, reason)
	}
	if len(resp.Attributes) != 0 {
		t.Fatalf("reject carried attributes: %+v", resp.Attributes)
	}
}

func assertAccept(t *testing.T, resp authorizeResponse) {
	t.Helper()
	if resp.Action != "accept" || resp.Reason != ReasonOK {
		t.Fatalf("got %+v, want accept/ok", resp)
	}
}

func TestUnknownNAS(t *testing.T) {
	env := newTestEnv(t) // no known NAS
	env.add("u", baseView("s1", "pw"))
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonUnknownNAS)
}

func TestUnknownUser(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	assertReject(t, mustDecide(t, env, papReq("ghost", "pw")), ReasonUnknownUser)
}

func TestPAPAcceptAndRate(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	env.add("u", baseView("s1", "secret"))
	resp := mustDecide(t, env, papReq("u", "secret"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "10M/10M" {
		t.Fatalf("rate = %+v", resp.Attributes)
	}
}

func TestPAPBadPassword(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	env.add("u", baseView("s1", "secret"))
	assertReject(t, mustDecide(t, env, papReq("u", "wrong")), ReasonBadPassword)
}

func TestCHAP(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	env.add("u", baseView("s1", "secret"))

	challenge := []byte("0123456789abcdef")
	const id = 9
	h := md5.New()
	h.Write([]byte{id})
	h.Write([]byte("secret"))
	h.Write(challenge)
	resp := append([]byte{id}, h.Sum(nil)...)
	req := authorizeRequest{
		Username: "u", NasIP: "10.0.0.1", Service: "pppoe",
		ChapChallenge: hex.EncodeToString(challenge), ChapResponse: hex.EncodeToString(resp),
	}
	assertAccept(t, mustDecide(t, env, req))

	// Wrong CHAP digest rejects.
	bad := req
	bad.ChapResponse = hex.EncodeToString(append([]byte{id}, make([]byte, 16)...))
	assertReject(t, mustDecide(t, env, bad), ReasonBadPassword)
}

func TestDisabled(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.Status = "disabled"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonDisabled)
}

func TestExpiredBlock(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ExpiresAt = fixedNow.Add(-time.Hour)
	v.ExpiryBehavior = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonExpired)
}

func TestExpiredPoolAccept(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.Status = "expired"
	v.ExpiryBehavior = "expired_pool"
	v.ExpiredPoolName = "walled"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentAddressPool)] != "walled" {
		t.Fatalf("expected expired pool, got %+v", resp.Attributes)
	}
	if a[string(IntentRedirectExpired)] == "" {
		t.Fatalf("expected redirect_expired intent, got %+v", resp.Attributes)
	}
	if a[string(IntentRateLimit)] != expiredPoolFallbackRate {
		t.Fatalf("expected minimal rate, got %q", a[string(IntentRateLimit)])
	}
}

func TestQuotaBlock(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonQuotaExhausted)
}

func TestQuotaThrottle(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.QuotaExhausted = true
	v.QuotaBehavior = "throttle"
	v.ThrottleRate = "1M/1M"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "1M/1M" {
		t.Fatalf("expected throttle rate, got %+v", resp.Attributes)
	}
}

func TestSessionLimitPPPoE(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.SessionLimit = 1
	env.add("u", v)
	env.live[[2]string{"s1", "pppoe"}] = 1 // already at the limit
	assertReject(t, mustDecide(t, env, papReq("u", "pw")), ReasonSessionLimit)
}

func TestMacLearnThenMismatch(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.MacLockMode = "learn"
	env.add("u", v)

	// First login with no learned MAC: accept and learn.
	req := papReq("u", "pw")
	req.CallingStationID = "AA:BB:CC:DD:EE:FF"
	assertAccept(t, mustDecide(t, env, req))
	if env.provider.learned["s1"] != "AABBCCDDEEFF" {
		t.Fatalf("mac not learned: %v", env.provider.learned)
	}

	// Now a locked MAC: a different one mismatches.
	v.LearnedMac = "AA:BB:CC:DD:EE:FF"
	env.add("u", v)
	req.CallingStationID = "11:22:33:44:55:66"
	assertReject(t, mustDecide(t, env, req), ReasonMACMismatch)

	// Same MAC in a different format still matches.
	req.CallingStationID = "aabb-ccdd-eeff"
	assertAccept(t, mustDecide(t, env, req))
}

func TestStaticIPPrecedence(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.PoolName = "active"
	v.StaticIP = "10.20.30.40"
	env.add("u", v)
	resp := mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	a := attrMap(resp)
	if a[string(IntentStaticIP)] != "10.20.30.40" {
		t.Fatalf("expected static_ip, got %+v", resp.Attributes)
	}
	if _, ok := a[string(IntentAddressPool)]; ok {
		t.Fatalf("static IP must suppress address_pool, got %+v", resp.Attributes)
	}
}

// --- FR-58 dual-service matrix --------------------------------------------

func hotspotReq(user, pass string) authorizeRequest {
	return authorizeRequest{Username: user, Password: pass, NasIP: "10.0.0.1", Service: "hotspot"}
}

func TestFR58FlagOffRejects(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = false
	env.add("u", v)
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonServiceNotAllowed)
}

func TestFR58HotspotAcceptsAtPPPoELimitWithHotspotRate(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = true
	v.SessionLimit = 1
	v.RateLimit = "10M/10M"
	v.HotspotRateLimit = "2M/2M"
	env.add("u", v)
	// PPPoE is at its limit, but Hotspot is a separate allowance (FR-58.2).
	env.live[[2]string{"s1", "pppoe"}] = 5
	env.live[[2]string{"s1", "hotspot"}] = 0
	resp := mustDecide(t, env, hotspotReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "2M/2M" {
		t.Fatalf("expected hotspot rate, got %+v", resp.Attributes)
	}
}

func TestFR58SecondHotspotRejects(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = true
	env.add("u", v)
	env.live[[2]string{"s1", "hotspot"}] = 1 // one already online
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonSessionLimit)
}

func TestFR58HotspotRateFallback(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = true
	v.RateLimit = "10M/10M"
	v.HotspotRateLimit = "" // fall back to the main rate
	env.add("u", v)
	resp := mustDecide(t, env, hotspotReq("u", "pw"))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "10M/10M" {
		t.Fatalf("expected fallback rate, got %+v", resp.Attributes)
	}
}

func TestFR58HotspotSkipsQuota(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = true
	v.QuotaExhausted = true
	v.QuotaBehavior = "block" // would block PPPoE, but Hotspot skips quota (FR-58.3)
	env.add("u", v)
	assertAccept(t, mustDecide(t, env, hotspotReq("u", "pw")))
}

func TestHotspotSkipsMacLock(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.AllowHotspot = true
	v.MacLockMode = "fixed"
	v.LearnedMac = "AA:BB:CC:DD:EE:FF"
	env.add("u", v)
	// A Hotspot device has a different MAC; the lock must not apply.
	req := hotspotReq("u", "pw")
	req.CallingStationID = "99:88:77:66:55:44"
	assertAccept(t, mustDecide(t, env, req))
}

// --- C4 HTTP validation surface -------------------------------------------

func postAuthorize(t *testing.T, env *testEnv, body string) *httptest.ResponseRecorder {
	t.Helper()
	m := &module{eng: env.eng, log: discardLogger()}
	h := m.authorizeHandler()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/internal/radius/authorize", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, r)
	return rec
}

func TestAuthorizeHTTPAcceptShape(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	env.add("testuser", baseView("s1", "testpass"))
	rec := postAuthorize(t, env, `{"username":"testuser","password":"testpass","nas_ip":"10.0.0.1","service":"pppoe"}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp authorizeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	assertAccept(t, resp)
}

func TestAuthorizeValidation(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	if rec := postAuthorize(t, env, `{"username":"x"}`); rec.Code != 422 {
		t.Fatalf("missing fields: status %d", rec.Code)
	}
	if rec := postAuthorize(t, env, `{"username":"x","nas_ip":"10.0.0.1","service":"carrier-pigeon"}`); rec.Code != 422 {
		t.Fatalf("bad service: status %d", rec.Code)
	}
}
