package vendor

// v4-7 (Router manager II, FR-144) — vendor-level tests for the setup
// wizard's detect + combined plan. No network, no CHR image: fakeROSConn
// (mikrotik_autosetup_test.go) stands in for a router — the code-verified
// half of the gate (docs/ops/ros-matrix.md's terminology), proving the
// read/parse/classify logic and the "nothing is ever deleted" contract
// independent of real RouterOS wire behavior. Gate item 15 (real hardware)
// is the other half, not run this session.

import (
	"strings"
	"testing"
)

func TestDetectWizard_ReportsAlreadyCorrectPerStep(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/interface/bridge/print"] = []map[string]string{{"name": "bridge1"}}
	conn.rows["/ip/dns/print"] = []map[string]string{{"servers": "8.8.8.8"}}
	conn.rows["/system/ntp/client/print"] = []map[string]string{{"enabled": "yes"}}
	conn.rows["/radius/print"] = []map[string]string{{"address": "10.0.0.5", "service": "ppp", "comment": hikradComment, "secret": "sekret"}}
	conn.rows["/interface/pppoe-server/server/print"] = []map[string]string{{"service-name": "hikrad-pppoe"}}

	d, err := mikrotikAdapter{}.DetectWizard(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Bridge.AlreadyCorrect || !d.DNS.AlreadyCorrect || !d.NTP.AlreadyCorrect || !d.RADIUS.AlreadyCorrect || !d.Services.AlreadyCorrect {
		t.Fatalf("expected every step already-correct given a fully configured router, got %+v", d)
	}
	if len(conn.writes) != 0 {
		t.Fatalf("DetectWizard must never write, got %+v", conn.writes)
	}
}

func TestDetectWizard_ReportsMissingOnFactoryRouter(t *testing.T) {
	conn := newFakeROS() // nothing registered: a factory-reset router
	d, err := mikrotikAdapter{}.DetectWizard(conn)
	if err != nil {
		t.Fatal(err)
	}
	if d.Bridge.AlreadyCorrect || d.DNS.AlreadyCorrect || d.NTP.AlreadyCorrect || d.RADIUS.AlreadyCorrect || d.Services.AlreadyCorrect {
		t.Fatalf("expected nothing already-correct on a factory router, got %+v", d)
	}
}

func TestPlanWizard_BridgeAlreadyPresent_SkipsNotRepushes(t *testing.T) {
	// Gate item 2: a re-run on an already-configured router must skip, not
	// re-push, a step that's already correct.
	conn := newFakeROS()
	conn.rows["/interface/bridge/print"] = []map[string]string{{"name": "bridge1"}}
	conn.rows["/interface/bridge/port/print"] = []map[string]string{{"bridge": "bridge1", "interface": "ether2"}}

	in := WizardInput{Bridge: &WizardBridgeInput{Name: "bridge1", Ports: []string{"ether2"}}}
	plan, err := mikrotikAdapter{}.PlanWizard(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("expected no-op for an already-correct bridge setup, got items=%+v conflicts=%+v", plan.Items, plan.Conflicts)
	}
}

func TestPlanWizard_FactoryRouter_PlansEveryStep(t *testing.T) {
	conn := newFakeROS() // factory reset: every print returns nothing
	in := WizardInput{
		Bridge:       &WizardBridgeInput{Name: "bridge1", Ports: []string{"ether2", "ether3"}},
		DNS:          &WizardDNSInput{Servers: []string{"8.8.8.8", "1.1.1.1"}, AllowRemoteRequests: true},
		NTP:          &WizardNTPInput{Servers: []string{"pool.ntp.org"}},
		WAN:          []WizardWANInput{{Interface: "ether1", Mode: "dhcp"}},
		RadiusValues: baseInput(),
		Firewall: &WizardFirewallInput{
			NATOutInterfaces: []string{"ether1"},
			ExpiredPool:      &WizardExpiredPoolInput{PoolName: "expired-pool", Ranges: []string{"10.10.0.0/24"}},
		},
	}
	plan, err := mikrotikAdapter{}.PlanWizard(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) == 0 {
		t.Fatal("expected a factory router to produce plan items for every step")
	}
	wantPaths := map[string]bool{
		"/interface/bridge": false, "/interface/bridge/port": false, "/ip/dns": false,
		"/system/ntp/client": false, "/ip/dhcp-client": false, "/radius": false,
		"/ip/firewall/nat": false, "/ip/pool": false,
	}
	for _, item := range plan.Items {
		if _, ok := wantPaths[item.Path]; ok {
			wantPaths[item.Path] = true
		}
		if item.Action == "remove" {
			t.Fatalf("PlanWizard must never emit a remove action, got %+v", item)
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("expected a plan item touching %s, got none (items=%+v)", path, plan.Items)
		}
	}

	// Gate item 5: every item's Command must be the literal, human-readable
	// command that Sentence would actually send — never empty.
	for _, item := range plan.Items {
		if item.Command == "" {
			t.Errorf("item %+v has no literal command to show the operator", item)
		}
	}
}

func TestPlanWizard_ApplyNeverIssuesRemove(t *testing.T) {
	// Gate item 8: full API capture across every write path shows no
	// removal command — proven here for the wizard's combined apply.
	conn := newFakeROS()
	in := WizardInput{
		Bridge:       &WizardBridgeInput{Name: "bridge1", Ports: []string{"ether2"}},
		DNS:          &WizardDNSInput{Servers: []string{"8.8.8.8"}},
		NTP:          &WizardNTPInput{Servers: []string{"pool.ntp.org"}},
		RadiusValues: baseInput(),
	}
	plan, err := mikrotikAdapter{}.PlanWizard(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts on a factory router: %+v", plan.Conflicts)
	}
	results := mikrotikAdapter{}.ApplyAutoSetup(conn, plan)
	if len(results) == 0 {
		t.Fatal("expected at least one applied item")
	}
	for _, w := range conn.writes {
		if len(w) == 0 {
			continue
		}
		if strings.Contains(w[0], "remove") {
			t.Fatalf("found a remove sentence: %+v", w)
		}
	}
}

func TestPlanBridgeSetup_ConflictingPortResolvesKeepUpdateAbort(t *testing.T) {
	// Gate item 7: keep / update / abort each verified for one representative
	// conflict (a port already belonging to a different bridge is not
	// Resolvable — there is no safe "update" target — so only keep/abort are
	// meaningful here, and the default absence of a resolution must abort).
	newConn := func() *fakeROSConn {
		conn := newFakeROS()
		conn.rows["/interface/bridge/print"] = []map[string]string{{"name": "bridge1"}}
		conn.rows["/interface/bridge/port/print"] = []map[string]string{{"bridge": "other-bridge", "interface": "ether5"}}
		return conn
	}
	in := WizardBridgeInput{Name: "bridge1", Ports: []string{"ether5"}}

	// Default (no resolution) -> abort: reported as a conflict, nothing planned.
	plan, err := mikrotikAdapter{}.planBridgeSetup(newConn(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected 1 unresolved conflict by default, got %+v", plan.Conflicts)
	}

	// "keep" -> dropped entirely, no item, no conflict.
	key := plan.Conflicts[0].Key
	plan, err = mikrotikAdapter{}.planBridgeSetup(newConn(), in, map[string]string{key: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("expected keep to drop the item entirely, got items=%+v conflicts=%+v", plan.Items, plan.Conflicts)
	}

	// "update" on a non-Resolvable conflict falls through to abort (never
	// forces a guessed write).
	plan, err = mikrotikAdapter{}.planBridgeSetup(newConn(), in, map[string]string{key: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected update on a non-resolvable conflict to still abort, got items=%+v conflicts=%+v", plan.Items, plan.Conflicts)
	}
}

func TestPlanDNSSetup_ConflictResolvesUpdate(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/dns/print"] = []map[string]string{{"servers": "192.168.1.1", "allow-remote-requests": "no"}}
	in := WizardDNSInput{Servers: []string{"8.8.8.8"}, AllowRemoteRequests: true}

	plan, err := mikrotikAdapter{}.planDNSSetup(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected a conflict when DNS is already set to something else, got items=%+v conflicts=%+v", plan.Items, plan.Conflicts)
	}
	key := plan.Conflicts[0].Key

	plan, err = mikrotikAdapter{}.planDNSSetup(conn, in, map[string]string{key: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != "set" {
		t.Fatalf("expected update to become one set item, got %+v", plan.Items)
	}
}

func TestPlanIPv6_PoolAndProfileAttachment(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ppp/profile/print"] = []map[string]string{{".id": "*1", "name": "hikrad-pppoe", "dhcpv6-pd-pool": ""}}
	in := WizardIPv6Input{Enabled: true, ROSServerName: "hikrad-pppoe", Prefix: "2001:db8::/32", PrefixLen: 56, PoolName: "hikrad-ipv6"}

	plan, err := mikrotikAdapter{}.PlanIPv6(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("expected a pool add + profile attach, got %+v", plan.Items)
	}
	if plan.Items[0].Path != "/ipv6/pool" || plan.Items[0].Action != "add" {
		t.Fatalf("expected first item to add the ipv6 pool, got %+v", plan.Items[0])
	}
	if plan.Items[1].Path != "/ppp/profile" || plan.Items[1].Action != "set" {
		t.Fatalf("expected second item to attach the pool to the ppp profile, got %+v", plan.Items[1])
	}
}
