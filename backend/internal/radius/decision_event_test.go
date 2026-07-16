package radius

// The FR-39 debug tail is the operator's only view of what HikRAD told a router
// to do. It used to record the outcome and reason alone, which is not enough to
// explain the failure it exists to explain: RADIUS accepts, and the router then
// refuses the login on its own ("no address from ip pool") because the reply
// named an address pool that router does not have. These tests pin the reply and
// the resolved instance into the recorded event.

import (
	"encoding/json"
	"testing"
)

func TestDecisionRecordsReplyAttributes(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	nas := testNASID("10.0.0.1")
	env.services[nas] = []serviceRow{
		{ID: "svc-hs", NASID: nas, Service: "hotspot", Enabled: true,
			ROSServerName: "Students-Wifi", Label: "Students WiFi", IPPoolName: "Students-WiFi_Pool"},
	}
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, hotspotReqOn("u", "pw", "Students-Wifi")))

	ev := env.lastDecision(t)
	got := map[string]string{}
	for _, a := range ev.Attributes {
		got[a.Intent] = a.Value
	}
	// The pool name is the whole point: an operator compares this string against
	// the router's own `/ip pool print` to find a mismatch.
	if got[string(IntentAddressPool)] != "Students-WiFi_Pool" {
		t.Errorf("recorded address_pool = %q, want the reply's Students-WiFi_Pool: %+v", got[string(IntentAddressPool)], ev.Attributes)
	}
	if got[string(IntentRateLimit)] != "10M/10M" {
		t.Errorf("recorded rate_limit = %q, want 10M/10M", got[string(IntentRateLimit)])
	}
	// And which of the NAS's zones it landed on (FR-62).
	if ev.Instance != "Students WiFi" {
		t.Errorf("recorded instance = %q, want the label Students WiFi", ev.Instance)
	}
}

// A hotspot accept that carries no pool is the fixed pilot bug's shape: the
// router keeps the address it already assigned. The debug tail must show the
// absence, since "no address_pool" is the correct state an operator is checking
// for after applying the fix.
func TestDecisionRecordsHotspotAcceptWithNoPool(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	v := baseView("s1", "pw")
	v.ServiceType = "hotspot"
	v.PoolName = "ppp-only-pool" // the profile's PPPoE pool: must not be sent
	env.add("u", v)

	assertAccept(t, mustDecide(t, env, hotspotReq("u", "pw")))

	for _, a := range env.lastDecision(t).Attributes {
		if a.Intent == string(IntentAddressPool) {
			t.Fatalf("hotspot accept recorded an address_pool of %q; it must send none", a.Value)
		}
	}
}

// A reject has no reply at all, and `omitempty` must keep the field out of the
// JSON rather than send `"attributes": null` to the panel.
func TestDecisionRejectCarriesNoReply(t *testing.T) {
	env := newTestEnv(t, "10.0.0.1")
	env.add("u", baseView("s1", "pw"))

	assertReject(t, mustDecide(t, env, papReq("u", "wrong")), ReasonBadPassword)

	ev := env.lastDecision(t)
	if len(ev.Attributes) != 0 {
		t.Fatalf("reject recorded a reply: %+v", ev.Attributes)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["attributes"]; present {
		t.Errorf("reject event serialized an attributes field: %s", payload)
	}
}

// An unnamed sole PPPoE server (RouterOS allows one) still names its kind, so
// the debug tail never shows a blank service cell.
func TestInstanceNameFallsBackThroughLabelThenServerNameThenKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  serviceRow
		want string
	}{
		{"label wins", serviceRow{Service: "hotspot", Label: "Lobby", ROSServerName: "hs1"}, "Lobby"},
		{"server name when unlabelled", serviceRow{Service: "hotspot", ROSServerName: "hs1"}, "hs1"},
		{"kind when neither is set", serviceRow{Service: "pppoe"}, "pppoe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := instanceName(tc.row); got != tc.want {
				t.Errorf("instanceName = %q, want %q", got, tc.want)
			}
		})
	}
}
