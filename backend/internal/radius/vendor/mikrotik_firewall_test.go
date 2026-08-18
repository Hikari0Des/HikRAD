package vendor

// v4-7 (Router manager II, FR-146) — Firewall tab vendor-level tests. See
// mikrotik_wizard_test.go's package doc for the fakeROSConn/no-hardware
// posture. These are gate items 6 (preview/apply hash gate — the plan step
// only ever reads), 8 (nothing is ever deleted), and 9's firewall-specific
// analog: an un-tagged (router-adopted) rule refuses edit until adopted,
// exactly like a router-managed service.

import "testing"

func TestReadFirewallRules_TagsOwnershipByComment(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{
		{".id": "*1", "chain": "input", "action": "accept", "comment": hikradComment + ": allow radius"},
		{".id": "*2", "chain": "input", "action": "drop", "comment": "someone else's rule"},
	}
	conn.rows["/ip/firewall/nat/print"] = []map[string]string{}

	rules, err := mikrotikAdapter{}.ReadFirewallRules(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Ownership != "system" {
		t.Errorf("expected the hikrad-tagged rule to badge system, got %q", rules[0].Ownership)
	}
	if rules[1].Ownership != "router" {
		t.Errorf("expected the untagged rule to badge router, got %q", rules[1].Ownership)
	}
	if len(conn.writes) != 0 {
		t.Fatalf("ReadFirewallRules must never write, got %+v", conn.writes)
	}
}

func TestPlanFirewallChange_NewRuleIsAdditive(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{}
	plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{
		Table: "filter", Fields: map[string]string{"chain": "input", "action": "accept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != "add" {
		t.Fatalf("expected one additive item, got %+v", plan.Items)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", plan.Conflicts)
	}
}

func TestPlanFirewallChange_EditingUntaggedRuleRefusesUntilAdopted(t *testing.T) {
	// Gate item 9's firewall analog: a router-managed (untagged) rule cannot
	// be edited from HikRAD — the conflict is not Resolvable (no forced
	// guess), mirroring how an un-adopted nas_services row refuses editing.
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{
		{".id": "*1", "chain": "input", "action": "drop", "comment": "operator's own rule"},
	}
	plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{
		RuleID: "*1", Table: "filter", Fields: map[string]string{"action": "accept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("expected no items for an edit to a router-managed rule, got %+v", plan.Items)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected exactly one conflict, got %+v", plan.Conflicts)
	}
	if plan.Conflicts[0].Resolvable {
		t.Fatal("editing an un-adopted rule must not be Resolvable — no forced guess")
	}
}

func TestPlanFirewallChange_EditingHikradTaggedRuleIsAllowed(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{
		{".id": "*1", "chain": "input", "action": "drop", "comment": hikradComment + ": block-legacy"},
	}
	plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{
		RuleID: "*1", Table: "filter", Fields: map[string]string{"action": "accept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected editing a HikRAD-tagged rule to be allowed, got conflicts=%+v", plan.Conflicts)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != "set" {
		t.Fatalf("expected one set item, got %+v", plan.Items)
	}
}

func TestPlanFirewallChange_FieldOrderIsDeterministic(t *testing.T) {
	// Two identical requests must render the byte-identical Command/Sentence
	// regardless of Go's randomized map iteration order — otherwise a
	// legitimate apply would spuriously fail preview_stale (see
	// sortedFieldKeys' doc).
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{}
	fields := map[string]string{"chain": "input", "action": "accept", "protocol": "tcp", "dst-port": "8291"}

	var commands []string
	for i := 0; i < 5; i++ {
		plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{Table: "filter", Fields: fields})
		if err != nil {
			t.Fatal(err)
		}
		commands = append(commands, plan.Items[0].Command)
	}
	for _, c := range commands[1:] {
		if c != commands[0] {
			t.Fatalf("Command rendering is not deterministic: %q vs %q", commands[0], c)
		}
	}
}

func TestSnapshotFirewallRules_CapturesPriorStateAndRevertSentences(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{
		{".id": "*1", "chain": "input", "action": "drop", "comment": hikradComment},
	}
	plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{
		RuleID: "*1", Table: "filter", Fields: map[string]string{"action": "accept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := mikrotikAdapter{}.SnapshotFirewallRules(conn, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rules) != 1 || snap.Rules[0].Action != "drop" {
		t.Fatalf("expected the snapshot to capture the PRE-change action=drop, got %+v", snap.Rules)
	}
	if len(snap.RevertSentences) != 1 {
		t.Fatalf("expected exactly one revert sentence, got %+v", snap.RevertSentences)
	}
	found := false
	for _, w := range snap.RevertSentences[0] {
		if w == "=action=drop" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the revert sentence to restore action=drop, got %+v", snap.RevertSentences[0])
	}
	// Snapshotting itself must never write.
	if len(conn.writes) != 0 {
		t.Fatalf("SnapshotFirewallRules must never write, got %+v", conn.writes)
	}
}

func TestSnapshotFirewallRules_AddItemsContributeNoRevertSentence(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/firewall/filter/print"] = []map[string]string{}
	plan, err := mikrotikAdapter{}.PlanFirewallChange(conn, FirewallChangeInput{
		Table: "filter", Fields: map[string]string{"chain": "input", "action": "accept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := mikrotikAdapter{}.SnapshotFirewallRules(conn, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.RevertSentences) != 0 {
		t.Fatalf("a brand-new rule has no prior state to revert to, got %+v", snap.RevertSentences)
	}
}
