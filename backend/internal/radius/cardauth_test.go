package radius

// Card-hotspot-login policy tests (v4-5, FR-136). Mirrors voucher_test.go's
// exact shape: a fake CardAuthenticator stands in for internal/cards' real
// DB-backed one, so these tests prove the RADIUS-side wiring (a card code is
// tried after a voucher miss, on hotspot only, bypassing password/service/
// scope checks) and — the binding rule this phase must satisfy — that a
// card-sourced AuthView is evaluated by the IDENTICAL evaluateLimits/decide()
// code path a subscriber's view already is: nothing in this file or in
// policy.go's card branch contains an accept/reject decision of its own.
// UptimeBlock/QuotaBlock below use fields already proven correct by
// TestUptimeBlock/TestQuotaSwitchPlan etc. in limits_test.go — this suite
// only proves a card-shaped view reaches that same machinery.

import (
	"context"
	"testing"
	"time"
)

type fakeCardAuth struct {
	code string
	view AuthView
	err  error
}

func (f *fakeCardAuth) AuthenticateCard(_ context.Context, code string) (AuthView, bool, error) {
	if f.err != nil {
		return AuthView{}, false, f.err
	}
	if code == f.code {
		return f.view, true, nil
	}
	return AuthView{}, false, nil
}

func TestCardLogin_Accepts(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fc := &fakeCardAuth{code: "CARD-0001", view: AuthView{
		Status: "active", ServiceType: "hotspot", RateLimit: "2M/2M",
	}}
	SetCardAuthenticator(fc)
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	resp := mustDecide(t, env, hotspotReq("CARD-0001", ""))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "2M/2M" {
		t.Fatalf("card accept rate = %+v", resp.Attributes)
	}
}

func TestCardLogin_UnknownCodeRejects(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	SetCardAuthenticator(&fakeCardAuth{code: "CARD-0001", view: AuthView{Status: "active"}})
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, hotspotReq("WRONG-9999", "")), ReasonUnknownUser)
}

func TestCardLogin_QuotaExhaustedBlocks(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	// A card group's transfer limit crossed, exhaust action is always "block"
	// (cards have no configurable switch_plan/throttle/expired_pool target —
	// phase-brief scope) — evaluateLimits' fail-safe-reject on an unrecognized
	// action string is exactly what an empty QuotaBehavior would also hit, but
	// cards/auth.go always sets it explicitly to "block".
	fc := &fakeCardAuth{code: "CARD-QUOTA", view: AuthView{
		Status: "active", ServiceType: "hotspot", RateLimit: "1M/1M",
		QuotaBehavior: "block", QuotaExhausted: true,
	}}
	SetCardAuthenticator(fc)
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, hotspotReq("CARD-QUOTA", "")), ReasonQuotaExhausted)
}

func TestCardLogin_UptimeExhaustedBlocks(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fc := &fakeCardAuth{code: "CARD-UPTIME", view: AuthView{
		Status: "active", ServiceType: "hotspot", RateLimit: "1M/1M",
		UptimeLimitSeconds: 3600, UptimeUsedSeconds: 3600, UptimeExhaustAction: "block",
	}}
	SetCardAuthenticator(fc)
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, hotspotReq("CARD-UPTIME", "")), ReasonUptimeExhausted)
}

func TestCardLogin_ExpiredBlocks(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fc := &fakeCardAuth{code: "CARD-EXPIRED", view: AuthView{
		Status: "active", ServiceType: "hotspot", RateLimit: "1M/1M",
		ExpiryBehavior: "block", ExpiresAt: fixedNow.Add(-time.Hour),
	}}
	SetCardAuthenticator(fc)
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, hotspotReq("CARD-EXPIRED", "")), ReasonExpired)
}

func TestCardLogin_TriedAfterVoucherMiss(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	// Same code registered as a voucher AND (hypothetically) a card: voucher
	// wins, exactly as the file doc comment states — this only matters if the
	// two hash spaces ever collided, which they structurally cannot (distinct
	// tables/columns), but the precedence is still deterministic either way.
	SetVoucherAuthenticator(&fakeVoucherAuth{code: "SHARED", view: AuthView{Status: "active", ServiceType: "dual", RateLimit: "9M/9M"}})
	SetCardAuthenticator(&fakeCardAuth{code: "SHARED", view: AuthView{Status: "active", ServiceType: "hotspot", RateLimit: "1M/1M"}})
	t.Cleanup(func() { SetVoucherAuthenticator(nil); SetCardAuthenticator(nil) })

	resp := mustDecide(t, env, hotspotReq("SHARED", ""))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "9M/9M" {
		t.Fatalf("expected the voucher's rate to win, got %+v", resp.Attributes)
	}
}

func TestCardLogin_PPPoEServiceIgnoresCards(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	SetCardAuthenticator(&fakeCardAuth{code: "CARD-0001", view: AuthView{Status: "active"}})
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, papReq("CARD-0001", "x")), ReasonUnknownUser)
}

func TestCardLogin_RealUsernameUnaffected(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "secret")
	v.ServiceType = "dual"
	env.add("realuser", v)
	SetCardAuthenticator(&fakeCardAuth{code: "realuser", view: AuthView{Status: "active"}})
	t.Cleanup(func() { SetCardAuthenticator(nil) })

	assertReject(t, mustDecide(t, env, authorizeRequest{
		Username: "realuser", Password: "wrong", NasIP: "10.0.0.1", Service: "hotspot",
	}), ReasonBadPassword)
}
