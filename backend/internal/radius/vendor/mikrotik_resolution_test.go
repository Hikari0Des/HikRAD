package vendor

import "testing"

// TestPlanAutoSetup_Resolution_Update_ProducesSetItem (FR-66.2, gate item 5):
// choosing "update" on a Resolvable conflict turns it into a /set PlanItem
// (never /add) targeting the existing router entry, and the conflict is
// dropped.
func TestPlanAutoSetup_Resolution_Update_ProducesSetItem(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{".id": "*1", "address": "10.0.0.5", "service": "ppp", "secret": "someone-elses-secret", "comment": "manually configured"},
	}
	// First: no resolution -> must still be a conflict (identical to pre-FR-66).
	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || !plan.Conflicts[0].Resolvable {
		t.Fatalf("expected one resolvable conflict with no resolution supplied, got %+v", plan.Conflicts)
	}
	key := plan.Conflicts[0].Key

	// Now resolve it "update".
	plan, err = For("mikrotik").PlanAutoSetup(conn, baseInput(), map[string]string{key: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected the resolved conflict to be dropped, got %+v", plan.Conflicts)
	}
	var found *PlanItem
	for i := range plan.Items {
		if plan.Items[i].Path == "/radius" {
			found = &plan.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a /radius update item, got %+v", plan.Items)
	}
	if found.Action != "set" {
		t.Fatalf("update resolution must produce a SET, not %q (never re-adds)", found.Action)
	}
	if len(found.Sentence) == 0 || found.Sentence[0] != "/radius/set" {
		t.Fatalf("expected the update sentence to target /radius/set with the existing .id, got %+v", found.Sentence)
	}
}

// TestPlanAutoSetup_Resolution_Keep_DropsConflictKeepsOtherItems (FR-66.2):
// "keep" drops the item from both Items and Conflicts, and every OTHER
// planned item still applies.
func TestPlanAutoSetup_Resolution_Keep_DropsConflictKeepsOtherItems(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{".id": "*1", "address": "10.0.0.5", "service": "ppp", "secret": "someone-elses-secret", "comment": "manually configured"},
	}
	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	key := plan.Conflicts[0].Key

	plan, err = For("mikrotik").PlanAutoSetup(conn, baseInput(), map[string]string{key: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Conflicts {
		if c.Key == key {
			t.Fatalf("expected the kept conflict to disappear, still present: %+v", c)
		}
	}
	for _, it := range plan.Items {
		if it.Path == "/radius" {
			t.Fatalf("keep must never produce a /radius item, got %+v", it)
		}
	}
	// The OTHER additive items (radius incoming, ppp aaa) still show up.
	var otherPaths []string
	for _, it := range plan.Items {
		otherPaths = append(otherPaths, it.Path)
	}
	if len(otherPaths) == 0 {
		t.Fatalf("expected the other additive items to still be planned, got none")
	}
}

// TestPlanAutoSetup_Resolution_UnresolvedOrAbort_MatchesPreFR66 (C1): an
// empty resolutions map, or an explicit "abort", produce byte-identical
// conflicts to the pre-FR-66 behavior (this is what keeps
// len(Conflicts) > 0 the single abort gate).
func TestPlanAutoSetup_Resolution_UnresolvedOrAbort_MatchesPreFR66(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{".id": "*1", "address": "10.0.0.5", "service": "ppp", "secret": "someone-elses-secret", "comment": "manually configured"},
	}
	planNil, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	key := planNil.Conflicts[0].Key
	planAbort, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), map[string]string{key: "abort"})
	if err != nil {
		t.Fatal(err)
	}
	planUnknown, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), map[string]string{key: "not-a-real-choice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []AutoSetupPlan{planAbort, planUnknown} {
		if len(p.Conflicts) != 1 || p.Conflicts[0].Path != "/radius" {
			t.Fatalf("expected the same single /radius conflict regardless of abort/unknown resolution, got %+v", p.Conflicts)
		}
	}
}

// TestPlanAutoSetup_Resolution_NotResolvable_UpdateFallsThroughToConflict
// (FR-66.2): the hotspot-profile custom-name conflict has no computable
// update target — an "update" choice on it must NOT be honored; it stays a
// blocking conflict exactly like abort would.
func TestPlanAutoSetup_Resolution_NotResolvable_UpdateFallsThroughToConflict(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{{".id": "*1", "name": "custom-zone-1", "use-radius": "no"}}
	in := baseInput()
	in.Type = "hotspot"

	plan, err := For("mikrotik").PlanAutoSetup(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for _, c := range plan.Conflicts {
		if c.Path == "/ip/hotspot/profile" {
			key = c.Key
			if c.Resolvable {
				t.Fatalf("the custom-profile conflict must be Resolvable=false, got true")
			}
		}
	}
	if key == "" {
		t.Fatalf("expected a /ip/hotspot/profile conflict, got %+v", plan.Conflicts)
	}

	plan2, err := For("mikrotik").PlanAutoSetup(conn, in, map[string]string{key: "update"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range plan2.Conflicts {
		if c.Path == "/ip/hotspot/profile" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'update' on a non-resolvable conflict to still block as a conflict, got %+v", plan2.Conflicts)
	}
}

// TestPlanAutoSetup_Resolution_IncomingAndPPPAAA_AreResolvable covers the
// other two Resolvable conflict sources (C4's contract lists exactly three:
// /radius, /radius/incoming, /ppp/aaa).
func TestPlanAutoSetup_Resolution_IncomingAndPPPAAA_AreResolvable(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/incoming/print"] = []map[string]string{{"accept": "yes", "port": "1700"}}
	conn.rows["/ppp/aaa/print"] = []map[string]string{{"use-radius": "yes", "accounting": "no", "interim-update": "60s"}}

	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]PlanConflict{}
	for _, c := range plan.Conflicts {
		byPath[c.Path] = c
	}
	for _, path := range []string{"/radius/incoming", "/ppp/aaa"} {
		c, ok := byPath[path]
		if !ok || !c.Resolvable || c.UpdateCommand == "" {
			t.Fatalf("expected a Resolvable conflict with a non-empty UpdateCommand for %s, got %+v (present=%v)", path, c, ok)
		}
	}

	resolutions := map[string]string{byPath["/radius/incoming"].Key: "update", byPath["/ppp/aaa"].Key: "update"}
	resolved, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), resolutions)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Conflicts) != 0 {
		t.Fatalf("expected both conflicts resolved away, got %+v", resolved.Conflicts)
	}
	var sawIncoming, sawAAA bool
	for _, it := range resolved.Items {
		if it.Path == "/radius/incoming" && it.Action == "set" {
			sawIncoming = true
		}
		if it.Path == "/ppp/aaa" && it.Action == "set" {
			sawAAA = true
		}
	}
	if !sawIncoming || !sawAAA {
		t.Fatalf("expected set items for both resolved conflicts, got %+v", resolved.Items)
	}
}
