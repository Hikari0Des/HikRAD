package vendor

import "testing"

func baseServiceValues() SnippetInput {
	v := baseInput()
	v.RadiusServer = "10.0.0.5"
	v.Secret = "sekret"
	return v
}

// TestPlanService_CreateHotspot_IncludesFR627Guard (FR-67.3, gate item 8): a
// brand-new hotspot server's plan includes the FR-62.7 address-pool=none
// guard on its OWN dedicated profile from the first plan — never a follow-up
// fix, and never touching "default".
func TestPlanService_CreateHotspot_IncludesFR627Guard(t *testing.T) {
	conn := newFakeROS()
	in := ServiceProvisionInput{
		Kind: "hotspot", ROSServerName: "zone2", Label: "Zone 2", Interface: "bridge-zone2",
		Values: baseServiceValues(),
	}
	plan, err := For("mikrotik").PlanService(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected a clean create plan, got conflicts %+v", plan.Conflicts)
	}
	var sawProfile, sawGuard, sawServer bool
	for _, it := range plan.Items {
		switch it.Path {
		case "/ip/hotspot/profile":
			sawProfile = true
			if it.Action != "add" {
				t.Fatalf("expected the new profile to be an add, got %q", it.Action)
			}
		case "/ip/hotspot/user/profile":
			sawGuard = true
			guardHasNone := false
			for _, w := range it.Sentence {
				if w == "=address-pool=none" {
					guardHasNone = true
				}
			}
			if !guardHasNone {
				t.Fatalf("expected the guard sentence to set address-pool=none, got %+v", it.Sentence)
			}
		case "/ip/hotspot":
			sawServer = true
		}
	}
	if !sawProfile || !sawGuard || !sawServer {
		t.Fatalf("expected profile+guard+server items, got %+v", plan.Items)
	}
	// The guard must target the NEW dedicated profile, never "default".
	for _, it := range plan.Items {
		if it.Path == "/ip/hotspot/user/profile" {
			for _, w := range it.Sentence {
				if w == "=numbers=default" {
					t.Fatalf("guard must never target the shared default profile: %+v", it.Sentence)
				}
			}
		}
	}
}

// TestPlanService_CreatePPPoE_Basic (FR-67.3).
func TestPlanService_CreatePPPoE_Basic(t *testing.T) {
	conn := newFakeROS()
	in := ServiceProvisionInput{Kind: "pppoe", ROSServerName: "pppoe-2", Interface: "ether2", Values: baseServiceValues()}
	plan, err := For("mikrotik").PlanService(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected a clean create plan, got %+v", plan.Conflicts)
	}
	var sawServer, sawAAA bool
	for _, it := range plan.Items {
		if it.Path == "/interface/pppoe-server/server" && it.Action == "add" {
			sawServer = true
		}
		if it.Path == "/ppp/aaa" {
			sawAAA = true
		}
	}
	if !sawServer || !sawAAA {
		t.Fatalf("expected a pppoe-server add + shared ppp/aaa wiring, got %+v", plan.Items)
	}
}

// TestPlanService_Create_NameCollision_Conflicts (C8: create-abort-only — no
// Resolvable/update semantics for "another service already has this
// identity").
func TestPlanService_Create_NameCollision_Conflicts(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/hotspot/print"] = []map[string]string{{".id": "*1", "name": "zone1", "interface": "bridge1", "profile": "default", "address-pool": "none"}}
	in := ServiceProvisionInput{Kind: "hotspot", ROSServerName: "zone1", Interface: "bridge1", Values: baseServiceValues()}
	plan, err := For("mikrotik").PlanService(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected exactly one abort-only conflict for the name collision, got %+v", plan.Conflicts)
	}
	if plan.Conflicts[0].Resolvable {
		t.Fatalf("service-provisioning conflicts must never be Resolvable")
	}
}

// TestPlanService_Edit_RequiresExistingMatch: editing (Editing:true) an
// instance the router no longer reports is itself a conflict — it must never
// silently fall back to creating a new one.
func TestPlanService_Edit_RequiresExistingMatch(t *testing.T) {
	conn := newFakeROS()
	in := ServiceProvisionInput{Kind: "hotspot", ROSServerName: "gone", Interface: "bridge1", Values: baseServiceValues(), Editing: true}
	plan, err := For("mikrotik").PlanService(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected an edit-target-not-found conflict, got %+v", plan.Conflicts)
	}
	for _, it := range plan.Items {
		if it.Action == "add" {
			t.Fatalf("edit must never add a new object when the target is missing, got %+v", it)
		}
	}
}

// TestPlanService_Edit_ExistingMatch_ProducesSetNotAdd.
func TestPlanService_Edit_ExistingMatch_ProducesSetNotAdd(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/hotspot/print"] = []map[string]string{{".id": "*1", "name": "zone1", "interface": "bridge1", "profile": "zone1-hikrad", "address-pool": "none"}}
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{{".id": "*2", "name": "zone1-hikrad", "use-radius": "yes"}}
	in := ServiceProvisionInput{
		Kind: "hotspot", ROSServerName: "zone1", Interface: "bridge1", PoolName: "guest-pool",
		Values: baseServiceValues(), Editing: true,
	}
	plan, err := For("mikrotik").PlanService(conn, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected a clean edit plan, got %+v", plan.Conflicts)
	}
	var sawSet bool
	for _, it := range plan.Items {
		if it.Path == "/ip/hotspot" {
			sawSet = true
			if it.Action != "set" {
				t.Fatalf("editing an existing hotspot server must /set, not %q", it.Action)
			}
		}
	}
	if !sawSet {
		t.Fatalf("expected a /ip/hotspot set item for the pool change, got %+v", plan.Items)
	}
}

// TestApplyService_DelegatesToApplyAutoSetup: same executor, same
// whole-apply-abort-on-first-failure contract — proven once already by
// TestApplyAutoSetup_StopsOnFirstFailure, this just confirms the delegation.
func TestApplyService_DelegatesToApplyAutoSetup(t *testing.T) {
	conn := newFakeROS()
	plan := AutoSetupPlan{Items: []PlanItem{{Action: "add", Path: "/ip/hotspot", Sentence: []string{"/ip/hotspot/add", "=name=x"}}}}
	results := For("mikrotik").ApplyService(conn, plan)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected the single item to apply cleanly, got %+v", results)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("expected exactly one write, got %d", len(conn.writes))
	}
}
