package vendor

// C8 multi-service snippet + additive auto-setup tests (FR-62.4 / gate item 4).
// The pre-v2 single-service snippet tests in vendor_test.go still pass through
// the legacy Type fallback — that is deliberate: an upgraded install's
// one-service NAS must render exactly what it always did.

import (
	"strings"
	"testing"
)

func multiInput() SnippetInput {
	return SnippetInput{
		ROSVersion: "7", NASName: "tower-1", RadiusServer: "10.0.0.5",
		Secret: "sekret", CoAPort: 3799, InterimSecs: 300,
		WalledGarden: []string{"portal.isp.iq"},
		Services: []ServiceSnippet{
			{Service: "pppoe", Label: "Subscribers"},
			{Service: "hotspot", Label: "Lobby", ROSServerName: "lobby", PoolName: "lobby-pool"},
			{Service: "hotspot", Label: "Cafe", ROSServerName: "cafe"},
		},
	}
}

// The gate's item-4 shape: 1 pppoe + 2 hotspot instances, one snippet covering
// all three.
func TestSnippetMultiServiceCoversEveryInstance(t *testing.T) {
	s, err := For("mikrotik").Snippet(multiInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// One /radius client listing every kind — not one client per instance.
		"/radius add service=ppp,hotspot,login",
		"/ppp aaa set use-radius=yes",
		"# Hotspot: Lobby",
		"# Hotspot: Cafe",
		`[find name="lobby"]`,
		`[find name="cafe"]`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("multi-service snippet missing %q:\n%s", want, s)
		}
	}
	if n := strings.Count(s, "/radius add "); n != 1 {
		t.Fatalf("expected exactly one /radius client, got %d:\n%s", n, s)
	}
}

// A named hotspot server must be addressed by name: with two zones on one box,
// `[find]` would re-point whichever profile RouterOS returned first.
func TestSnippetHotspotAddressesProfileByServerName(t *testing.T) {
	s, err := For("mikrotik").Snippet(multiInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "/ip hotspot profile set [find] ") {
		t.Fatalf("multi-hotspot snippet used the ambiguous [find] selector:\n%s", s)
	}
}

// The pool note tells the operator where a zone's addresses come from — the
// pilot bug was invisible precisely because nothing said this out loud.
func TestSnippetHotspotStatesPoolOrigin(t *testing.T) {
	s, err := For("mikrotik").Snippet(multiInput())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `per-service pool "lobby-pool"`) {
		t.Fatalf("snippet does not name the lobby pool:\n%s", s)
	}
	if !strings.Contains(s, "own address-pool (HikRAD sends none)") {
		t.Fatalf("snippet does not explain the pool-less hotspot's addressing:\n%s", s)
	}
}

// A pppoe-only NAS must not get hotspot/login in its /radius service list.
func TestSnippetPPPoEOnlyServiceList(t *testing.T) {
	in := multiInput()
	in.Services = []ServiceSnippet{{Service: "pppoe"}}
	s, err := For("mikrotik").Snippet(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "/radius add service=ppp ") {
		t.Fatalf("pppoe-only snippet should list service=ppp alone:\n%s", s)
	}
	if strings.Contains(s, "/ip hotspot") {
		t.Fatalf("pppoe-only snippet emitted a hotspot block:\n%s", s)
	}
}

// A hotspot-only NAS must not get the PPP AAA block.
func TestSnippetHotspotOnlyOmitsPPPAAA(t *testing.T) {
	in := multiInput()
	in.Services = []ServiceSnippet{{Service: "hotspot", ROSServerName: "hs1"}}
	s, err := For("mikrotik").Snippet(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "/ppp aaa") {
		t.Fatalf("hotspot-only snippet emitted the PPP AAA block:\n%s", s)
	}
	if !strings.Contains(s, "/radius add service=hotspot,login") {
		t.Fatalf("hotspot-only snippet service list wrong:\n%s", s)
	}
}

func TestSnippetNoEnabledServiceIsAnError(t *testing.T) {
	in := multiInput()
	in.Services = []ServiceSnippet{{Service: "wat"}}
	if _, err := For("mikrotik").Snippet(in); err == nil {
		t.Fatal("expected an error when no known service is enabled")
	}
}

// --- additive auto-setup (C8) ----------------------------------------------

// A multi-service NAS plans BOTH the PPP AAA entry and the hotspot profile +
// walled garden: additive, not either/or.
func TestPlanAutoSetup_MultiService_PlansBothKinds(t *testing.T) {
	conn := newFakeROS()
	// The router has the stock hotspot profile auto-setup targets.
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{{"name": "default", ".id": "*1"}}
	in := multiInput()
	in.WalledGarden = []string{"portal.isp.iq"}
	plan, err := For("mikrotik").PlanAutoSetup(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("fresh router should have no conflicts, got %+v", plan.Conflicts)
	}
	paths := map[string]int{}
	for _, it := range plan.Items {
		paths[it.Path]++
	}
	for _, want := range []string{"/ppp/aaa", "/ip/hotspot/profile", "/radius", "/radius/incoming"} {
		if paths[want] == 0 {
			t.Fatalf("multi-service plan missing a %s item; got %+v", want, plan.Items)
		}
	}
	// /ppp aaa is global on RouterOS: N pppoe instances still mean one item.
	if paths["/ppp/aaa"] != 1 {
		t.Fatalf("expected exactly one /ppp/aaa item, got %d", paths["/ppp/aaa"])
	}
}

// TestPlanAutoSetup_MultiHotspot_ProfileLimitation pins a known boundary of
// this phase: the SNIPPET fully configures N hotspot zones (each addressed by
// its own server name), but AUTO-SETUP's hotspot half still only targets the
// stock profile named "default". A router whose zones each carry their own
// profile therefore conflicts out — safely, with a message pointing at the
// copy-paste path — rather than silently configuring the wrong zone.
//
// Making auto-setup read the router's real profile layout and modify-or-create
// per zone is v2-2's job (docs/v2/03-nas-autosetup-config-manager.md), which
// the execution plan already sequences directly after this phase. Documented in
// known-issues.md rather than half-fixed here.
func TestPlanAutoSetup_MultiHotspot_ProfileLimitation(t *testing.T) {
	conn := newFakeROS()
	// Zones with their own profiles and no stock "default".
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{
		{"name": "lobby-profile", ".id": "*1"},
		{"name": "cafe-profile", ".id": "*2"},
	}
	plan, err := For("mikrotik").PlanAutoSetup(conn, multiInput())
	if err != nil {
		t.Fatal(err)
	}
	var got *PlanConflict
	for i := range plan.Conflicts {
		if plan.Conflicts[i].Path == "/ip/hotspot/profile" {
			got = &plan.Conflicts[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a hotspot-profile conflict when no default profile exists; got %+v", plan.Conflicts)
	}
	if !strings.Contains(got.Reason, "copy-paste") {
		t.Fatalf("conflict should point the operator at the snippet path, got %q", got.Reason)
	}
}

// The /radius entry auto-setup plans must list the same services the snippet's
// shared block does — the two paths describe one desired state.
func TestPlanAutoSetup_RadiusServiceListMatchesSnippet(t *testing.T) {
	conn := newFakeROS()
	plan, err := For("mikrotik").PlanAutoSetup(conn, multiInput())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range plan.Items {
		if it.Path != "/radius" {
			continue
		}
		found = true
		if !strings.Contains(it.Command, "service=ppp,hotspot,login") {
			t.Fatalf("/radius item service list = %q, want ppp,hotspot,login", it.Command)
		}
	}
	if !found {
		t.Fatalf("no /radius item planned: %+v", plan.Items)
	}
}
