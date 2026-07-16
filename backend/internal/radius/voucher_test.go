package radius

import (
	"context"
	"testing"
)

func TestPlausibleVoucher(t *testing.T) {
	cases := []struct {
		code, prefix string
		want         bool
	}{
		{"ABCD-1234", "", true},
		{"abcd1234", "", true},
		{"AB", "", false},                    // too short
		{"has space", "", false},             // whitespace
		{"good$one", "", false},              // bad char
		{"V-ABCD-1234", "V-", true},          // prefix match
		{"v-abcd-1234", "V-", true},          // case-insensitive prefix
		{"ABCD-1234", "V-", false},           // prefix required, absent
		{"", "", false},                      // empty
	}
	for _, c := range cases {
		if got := plausibleVoucher(c.code, c.prefix); got != c.want {
			t.Errorf("plausibleVoucher(%q, %q) = %v, want %v", c.code, c.prefix, got, c.want)
		}
	}
}

// fakeVoucherAuth authorizes exactly one code.
type fakeVoucherAuth struct {
	code string
	view AuthView
	err  error
}

func (f *fakeVoucherAuth) AuthenticateVoucher(_ context.Context, code string) (AuthView, bool, error) {
	if f.err != nil {
		return AuthView{}, false, f.err
	}
	if code == f.code {
		return f.view, true, nil
	}
	return AuthView{}, false, nil
}

func TestVoucherLogin_Accepts(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fv := &fakeVoucherAuth{code: "GOLD-2222", view: AuthView{
		SubscriberID: "vsub", Status: "active", ServiceType: "dual", RateLimit: "5M/5M", PoolName: "hotspot-pool",
	}}
	SetVoucherAuthenticator(fv)
	t.Cleanup(func() { SetVoucherAuthenticator(nil) })

	resp := mustDecide(t, env, hotspotReq("GOLD-2222", ""))
	assertAccept(t, resp)
	if attrMap(resp)[string(IntentRateLimit)] != "5M/5M" {
		t.Fatalf("voucher accept rate = %+v", resp.Attributes)
	}
}

func TestVoucherLogin_InvalidCodeRejects(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fv := &fakeVoucherAuth{code: "GOLD-2222", view: AuthView{Status: "active", ServiceType: "dual"}}
	SetVoucherAuthenticator(fv)
	t.Cleanup(func() { SetVoucherAuthenticator(nil) })

	// Voucher-shaped but unknown code → unknown_user (redeemer returned ok=false).
	assertReject(t, mustDecide(t, env, hotspotReq("WRONG-9999", "")), ReasonUnknownUser)
}

func TestVoucherLogin_NotAttemptedForNonVoucherShape(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	// Prefix required, so a plain username is not treated as a voucher.
	SetVoucherAuthenticator(&fakeVoucherAuth{code: "x"})
	SetVoucherPrefix("V-")
	t.Cleanup(func() { SetVoucherAuthenticator(nil); SetVoucherPrefix("") })

	assertReject(t, mustDecide(t, env, hotspotReq("ab", "")), ReasonUnknownUser)
}

func TestVoucherLogin_RealUsernameUnaffected(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "secret")
	v.ServiceType = "dual"
	env.add("realuser", v)
	SetVoucherAuthenticator(&fakeVoucherAuth{code: "realuser", view: AuthView{Status: "active"}})
	t.Cleanup(func() { SetVoucherAuthenticator(nil) })

	// A known subscriber authenticates via the normal path (password checked),
	// never the voucher path — so a wrong password still rejects.
	assertReject(t, mustDecide(t, env, authorizeRequest{
		Username: "realuser", Password: "wrong", NasIP: "10.0.0.1", Service: "hotspot",
	}), ReasonBadPassword)
}

func TestVoucherLogin_PPPoEServiceIgnoresVouchers(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	SetVoucherAuthenticator(&fakeVoucherAuth{code: "GOLD-2222", view: AuthView{Status: "active"}})
	t.Cleanup(func() { SetVoucherAuthenticator(nil) })

	// Voucher login is a Hotspot-only flow; a PPPoE request with a voucher-shaped
	// username is a plain unknown user.
	assertReject(t, mustDecide(t, env, papReq("GOLD-2222", "x")), ReasonUnknownUser)
}
