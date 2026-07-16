package radius

// v2 phase 1 policy tests: the FR-61 service-type matrix, FR-62 multi-service
// NAS instance resolution, FR-64 NAS scoping, and the service-aware address
// pool precedence that closes the pilot's "no more free addresses in the pool"
// bug. Named for the gate legs in scripts/gate-v2-phase-1.sh.
//
// These reuse authorize_test.go's fake-seam engine, so they run with no DB or
// Redis — the behaviour under test is pure policy.

import (
	"testing"
)

// hotspotReqOn builds a hotspot Access-Request carrying a Called-Station-Id, the
// attribute a MikroTik uses to name the hotspot server (C7).
func hotspotReqOn(user, pass, calledStation string) authorizeRequest {
	r := hotspotReq(user, pass)
	r.CalledStationID = calledStation
	return r
}

func papReqOnNAS(user, pass, nasIP string) authorizeRequest {
	r := papReq(user, pass)
	r.NasIP = nasIP
	return r
}

// hotspotReqOnNAS names both halves — which router, and which of its hotspot
// servers — for the scope tests that span more than one NAS.
func hotspotReqOnNAS(user, pass, calledStation, nasIP string) authorizeRequest {
	r := hotspotReqOn(user, pass, calledStation)
	r.NasIP = nasIP
	return r
}

// --- Gate item 3: the FR-61 service-type matrix ----------------------------

func TestServiceTypeMatrix(t *testing.T) {
	cases := []struct {
		name        string
		serviceType string
		hotspot     bool
		wantAccept  bool
	}{
		{"pppoe subscriber, pppoe login", "pppoe", false, true},
		{"pppoe subscriber, hotspot login rejects", "pppoe", true, false},
		{"hotspot-only subscriber, hotspot login", "hotspot", true, true},
		{"hotspot-only subscriber, pppoe login rejects", "hotspot", false, false},
		{"dual subscriber, pppoe login", "dual", false, true},
		{"dual subscriber, hotspot login", "dual", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, "10.0.0.1")
			v := baseView("s1", "pw")
			v.ServiceType = tc.serviceType
			env.add("u", v)

			req := papReq("u", "pw")
			if tc.hotspot {
				req = hotspotReq("u", "pw")
			}
			resp := mustDecide(t, env, req)
			if tc.wantAccept {
				assertAccept(t, resp)
				return
			}
			// The reject must be service_not_allowed — the SUBSCRIBER forbids
			// this service. nas_not_allowed would mean the NAS did, which is a
			// different fix for the operator (FR-39).
			assertReject(t, resp, ReasonServiceNotAllowed)
		})
	}
}

// TestServiceTypeMatrixHotspotOnlyEnforcesQuota locks FR-61.3 against the FR-58.3
// exemption it must NOT inherit: v1 skipped quota for every hotspot request
// because hotspot was always a dual subscriber's bonus leg. For a hotspot-only
// subscriber the hotspot IS the plan, so an exhausted quota must bite.
func TestServiceTypeMatrixHotspotOnlyEnforcesQuota(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.QuotaExhausted = true
	v.QuotaBehavior = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonQuotaExhausted)
}

// A dual subscriber's hotspot leg keeps the FR-58.3 exemption (v1 behaviour).
func TestServiceTypeMatrixDualHotspotStillSkipsQuota(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "dual"
	v.QuotaExhausted = true
	v.QuotaBehavior = "block"
	env.add("u", v)
	assertAccept(t, mustDecide(t, env, hotspotReq("u", "pw")))
}

// TestServiceTypeMatrixHotspotOnlySessionLimit locks FR-61.3's session counting:
// a hotspot-only subscriber's hotspot sessions count against their OWN
// SessionLimit, not v1's flat one-hotspot-session rule (which would have made a
// multi-device hotspot plan unsellable).
func TestServiceTypeMatrixHotspotOnlySessionLimit(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.SessionLimit = 3
	env.add("u", v)

	env.live[[2]string{"s1", "hotspot"}] = 2 // under the limit
	assertAccept(t, mustDecide(t, env, hotspotReq("u", "pw")))

	env.live[[2]string{"s1", "hotspot"}] = 3 // at the limit
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonSessionLimit)
}

// Expiry applies to a hotspot-only subscriber exactly as to pppoe (C6 step 8).
func TestServiceTypeMatrixHotspotOnlyExpires(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.Status = "expired"
	v.ExpiryBehavior = "block"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonExpired)
}

// --- Gate item 4: multi-service NAS ----------------------------------------

// TestMultiServiceNAS drives the gate's item-4 shape: one NAS running two
// hotspot instances and one PPPoE instance. Each request must resolve to its
// own instance and receive that instance's pool.
func TestMultiServiceNAS(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true, IPPoolName: "ppp-pool"},
		{ID: "svc-lobby", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "lobby", IPPoolName: "lobby-pool"},
		{ID: "svc-cafe", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "cafe", IPPoolName: "cafe-pool"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "dual"
	env.add("u", v)

	// Each hotspot server name resolves to its own instance and pool.
	resp := mustDecide(t, env, hotspotReqOn("u", "pw", "lobby"))
	assertAccept(t, resp)
	if got := attrMap(resp)[string(IntentAddressPool)]; got != "lobby-pool" {
		t.Fatalf("lobby login got pool %q, want lobby-pool", got)
	}
	resp = mustDecide(t, env, hotspotReqOn("u", "pw", "cafe"))
	assertAccept(t, resp)
	if got := attrMap(resp)[string(IntentAddressPool)]; got != "cafe-pool" {
		t.Fatalf("cafe login got pool %q, want cafe-pool", got)
	}
	// The lone PPPoE instance resolves without any name to match on.
	resp = mustDecide(t, env, papReq("u", "pw"))
	assertAccept(t, resp)
	if got := attrMap(resp)[string(IntentAddressPool)]; got != "ppp-pool" {
		t.Fatalf("pppoe login got pool %q, want ppp-pool", got)
	}
}

// An unmatched server name among SEVERAL hotspot instances is ambiguous: the
// engine must reject rather than guess, since guessing hands the session
// another zone's pool.
func TestMultiServiceNASAmbiguousRejects(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-lobby", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "lobby"},
		{ID: "svc-cafe", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "cafe"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, hotspotReqOn("u", "pw", "garden")), ReasonNASNotAllowed)
}

// TestMultiServiceNASNoInstanceOfKind is the amended C6-step-2 clause: a hotspot
// login on a NAS that runs no enabled hotspot instance rejects nas_not_allowed
// — the NAS's config forbids it, not the account — even for a dual subscriber
// whose service_type would happily allow hotspot.
func TestMultiServiceNASNoInstanceOfKind(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "dual"
	env.add("u", v)
	assertReject(t, mustDecide(t, env, hotspotReq("u", "pw")), ReasonNASNotAllowed)
}

// NOTE: "a disabled instance is not a candidate" is enforced by the
// `AND s.enabled` in enabledServices' SQL, not by the engine — the engine only
// ever sees the enabled set. It is covered by the DB-gated store test rather
// than here, where the fake seam would just be asserting its own input.

// --- Gate item 5: FR-64 NAS scoping ----------------------------------------

func TestNASScoping(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1", "10.0.0.2")

	v := baseView("s1", "pw")
	v.Scopes = []NASScope{{NASID: testNASID("10.0.0.1")}}
	env.add("u", v)

	// Accepts on the assigned NAS...
	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.1")))
	// ...and rejects on any other, with the credential still perfectly valid.
	assertReject(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.2")), ReasonNASNotAllowed)
}

// An empty assignment is "any NAS" — v1's behaviour, and the default for every
// row the migration touched.
func TestNASScopingUnassignedAllowsAnyNAS(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1", "10.0.0.2")
	env.add("u", baseView("s1", "pw"))
	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.1")))
	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.2")))
}

// A service-instance scope pins the subscriber to one instance on the NAS.
func TestNASScopingServiceInstance(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-lobby", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "lobby"},
		{ID: "svc-cafe", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "cafe"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.Scopes = []NASScope{{NASID: nas, ServiceID: "svc-lobby"}}
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, hotspotReqOn("u", "pw", "lobby")))
	assertReject(t, mustDecide(t, env, hotspotReqOn("u", "pw", "cafe")), ReasonNASNotAllowed)
}

// TestNASScopingRejectsBeforeCredentials locks the frozen check-chain order
// (C6): NAS scope is evaluated before credentials, so a wrong password on a
// forbidden NAS reports nas_not_allowed. The order is what stops the reject
// reason from leaking whether the password was right to someone probing from an
// unauthorized NAS.
func TestNASScopingRejectsBeforeCredentials(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1", "10.0.0.2")
	v := baseView("s1", "pw")
	v.Scopes = []NASScope{{NASID: testNASID("10.0.0.1")}}
	env.add("u", v)
	assertReject(t, mustDecide(t, env, papReqOnNAS("u", "WRONG", "10.0.0.2")), ReasonNASNotAllowed)
}

// The point of a scope SET: an account allowed on two towers and nowhere else.
// The single (nas_id, nas_service_id) pair this replaced could not express it,
// so operators had to choose between one NAS and everywhere — and chose
// everywhere, leaving the feature unused.
func TestNASScopingAllowsAnyOfSeveralNAS(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1", "10.0.0.2", "10.0.0.3")
	v := baseView("s1", "pw")
	v.Scopes = []NASScope{{NASID: testNASID("10.0.0.1")}, {NASID: testNASID("10.0.0.2")}}
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.1")))
	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.2")))
	assertReject(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.3")), ReasonNASNotAllowed)
}

// Two zones of a three-zone router — the everyday shape of a multi-select.
func TestNASScopingAllowsSeveralServicesOnOneNAS(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-lobby", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "lobby"},
		{ID: "svc-cafe", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "cafe"},
		{ID: "svc-staff", NASID: nas, Service: "hotspot", Enabled: true, ROSServerName: "staff"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.Scopes = []NASScope{{NASID: nas, ServiceID: "svc-lobby"}, {NASID: nas, ServiceID: "svc-cafe"}}
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, hotspotReqOn("u", "pw", "lobby")))
	assertAccept(t, mustDecide(t, env, hotspotReqOn("u", "pw", "cafe")))
	assertReject(t, mustDecide(t, env, hotspotReqOn("u", "pw", "staff")), ReasonNASNotAllowed)
}

// A whole-NAS scope mixed with a per-service scope on ANOTHER NAS: every zone of
// the first, only one zone of the second.
func TestNASScopingMixesWholeNASAndServiceScopes(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1", "10.0.0.2")
	nas2 := testNASID("10.0.0.2")
	env.services[nas2] = []serviceRow{
		{ID: "svc-lobby", NASID: nas2, Service: "hotspot", Enabled: true, ROSServerName: "lobby"},
		{ID: "svc-cafe", NASID: nas2, Service: "hotspot", Enabled: true, ROSServerName: "cafe"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "dual"
	v.Scopes = []NASScope{
		{NASID: testNASID("10.0.0.1")},        // the whole first NAS
		{NASID: nas2, ServiceID: "svc-lobby"}, // only one zone of the second
	}
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, papReqOnNAS("u", "pw", "10.0.0.1")))
	assertAccept(t, mustDecide(t, env, hotspotReqOnNAS("u", "pw", "lobby", "10.0.0.2")))
	assertReject(t, mustDecide(t, env, hotspotReqOnNAS("u", "pw", "cafe", "10.0.0.2")), ReasonNASNotAllowed)
}

// scopeAllows' empty case is the one that must never regress: an empty set is
// "any NAS" (v1's behaviour and every migrated row's default), NOT "nowhere".
// Getting this backwards would lock out every subscriber in the database at
// once, so it is asserted directly rather than only through the engine.
func TestScopeAllowsEmptySetMeansAnyNAS(t *testing.T) {
	if !scopeAllows(nil, "nas-1", "svc-1") {
		t.Error("a nil scope set denied; it must mean any NAS")
	}
	if !scopeAllows([]NASScope{}, "nas-1", "svc-1") {
		t.Error("an empty scope set denied; it must mean any NAS")
	}
}

// A voucher is not scoped to a NAS: it bypasses the FR-64 check as it does the
// service-type matrix (C6 steps 4-5 both skip for voucherAuthed).
func TestNASScopingVoucherBypasses(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	fv := &fakeVoucherAuth{code: "GOLD-1111", view: AuthView{
		SubscriberID: "vsub", Status: "active", ServiceType: "dual", RateLimit: "5M/5M",
		Scopes: []NASScope{{NASID: "some-other-nas"}},
	}}
	SetVoucherAuthenticator(fv)
	t.Cleanup(func() { SetVoucherAuthenticator(nil) })
	assertAccept(t, mustDecide(t, env, hotspotReq("GOLD-1111", "")))
}

// --- Gate item 6: service-aware pools (the pilot-bug lock) -----------------

// TestNoPoolOmitsAddressPool is the regression lock for the pilot's "no more
// free addresses in the pool" failure. v1 emitted the PPPoE profile's pool on
// every hotspot accept, so the router tried to allocate from a named pool it
// usually did not have. Each leg below is one clause of C6's corrected
// precedence.
func TestNoPoolOmitsAddressPool(t *testing.T) {
	nasIP := "10.0.0.1"
	nas := testNASID(nasIP)

	// (a) dual subscriber, profile HAS a PPPoE pool, hotspot instance has none
	//     => the hotspot accept must carry NO address_pool at all.
	t.Run("hotspot omits the profile's pppoe pool", func(t *testing.T) {
		env := newTestEnv(t, nasIP)
		env.services[nas] = []serviceRow{
			{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true},
			{ID: "svc-hs", NASID: nas, Service: "hotspot", Enabled: true}, // no pool
		}
		v := baseView("s1", "pw")
		v.ServiceType = "dual"
		v.PoolName = "pppoe-customers" // the profile's PPPoE pool
		env.add("u", v)

		resp := mustDecide(t, env, hotspotReq("u", "pw"))
		assertAccept(t, resp)
		if got, ok := attrMap(resp)[string(IntentAddressPool)]; ok {
			t.Fatalf("hotspot reply carried address_pool %q; it must be omitted so the "+
				"router uses its own hotspot pool (pilot bug)", got)
		}
	})

	// (b) the same subscriber's PPPoE login still gets the profile pool.
	t.Run("pppoe still gets the profile pool", func(t *testing.T) {
		env := newTestEnv(t, nasIP)
		env.services[nas] = []serviceRow{
			{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true},
			{ID: "svc-hs", NASID: nas, Service: "hotspot", Enabled: true},
		}
		v := baseView("s1", "pw")
		v.ServiceType = "dual"
		v.PoolName = "pppoe-customers"
		env.add("u", v)

		resp := mustDecide(t, env, papReq("u", "pw"))
		assertAccept(t, resp)
		if got := attrMap(resp)[string(IntentAddressPool)]; got != "pppoe-customers" {
			t.Fatalf("pppoe reply pool = %q, want pppoe-customers", got)
		}
	})

	// (c) setting a hotspot-service pool makes hotspot emit that pool.
	t.Run("hotspot service pool is emitted when set", func(t *testing.T) {
		env := newTestEnv(t, nasIP)
		env.services[nas] = []serviceRow{
			{ID: "svc-hs", NASID: nas, Service: "hotspot", Enabled: true, IPPoolName: "hs-guests"},
		}
		v := baseView("s1", "pw")
		v.ServiceType = "hotspot"
		v.PoolName = "pppoe-customers"
		env.add("u", v)

		resp := mustDecide(t, env, hotspotReq("u", "pw"))
		assertAccept(t, resp)
		if got := attrMap(resp)[string(IntentAddressPool)]; got != "hs-guests" {
			t.Fatalf("hotspot reply pool = %q, want the hotspot service's own hs-guests", got)
		}
	})

	// (d) no pool anywhere on a pppoe login omits the intent too.
	t.Run("pppoe with no pool anywhere omits it", func(t *testing.T) {
		env := newTestEnv(t, nasIP)
		env.services[nas] = []serviceRow{
			{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true},
		}
		v := baseView("s1", "pw")
		v.PoolName = ""
		env.add("u", v)

		resp := mustDecide(t, env, papReq("u", "pw"))
		assertAccept(t, resp)
		if got, ok := attrMap(resp)[string(IntentAddressPool)]; ok {
			t.Fatalf("pppoe reply carried address_pool %q with no pool configured", got)
		}
	})

	// (e) a static IP still beats every pool, including the instance's.
	t.Run("static ip wins over the service pool", func(t *testing.T) {
		env := newTestEnv(t, nasIP)
		env.services[nas] = []serviceRow{
			{ID: "svc-ppp", NASID: nas, Service: "pppoe", Enabled: true, IPPoolName: "ppp-pool"},
		}
		v := baseView("s1", "pw")
		v.StaticIP = "10.9.9.9"
		v.PoolName = "pppoe-customers"
		env.add("u", v)

		resp := mustDecide(t, env, papReq("u", "pw"))
		assertAccept(t, resp)
		a := attrMap(resp)
		if a[string(IntentStaticIP)] != "10.9.9.9" {
			t.Fatalf("expected static_ip, got %+v", resp.Attributes)
		}
		if got, ok := a[string(IntentAddressPool)]; ok {
			t.Fatalf("static-ip reply also carried address_pool %q", got)
		}
	})
}

// The expired walled-garden pool is deliberately NOT service-aware: it is an
// explicitly named pool the operator configured for the redirect, and key flow
// 2 depends on it being emitted for hotspot sessions too.
func TestExpiredPoolStillEmittedForHotspot(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.Status = "expired"
	v.ExpiryBehavior = "expired_pool"
	v.ExpiredPoolName = "walled"
	env.add("u", v)

	resp := mustDecide(t, env, hotspotReq("u", "pw"))
	assertAccept(t, resp)
	if got := attrMap(resp)[string(IntentAddressPool)]; got != "walled" {
		t.Fatalf("expired hotspot reply pool = %q, want walled", got)
	}
}
