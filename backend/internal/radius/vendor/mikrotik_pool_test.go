package vendor

import "testing"

// FR-108: PlanPool is the /ip pool read-modify-write planner behind "push a
// HikRAD-owned pool to the router". These exercise the three outcomes against
// the in-memory fake router — no hardware, no network.

func TestPlanPoolAddsWhenAbsent(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	ros.rows["/ip/pool/print"] = []map[string]string{
		{".id": "*1", "name": "other", "ranges": "10.9.0.2-10.9.0.99"},
	}
	plan, err := a.PlanPool(ros, PoolPushInput{Name: "hikrad-active", Ranges: []string{"10.0.0.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("want 1 item, got %+v", plan.Items)
	}
	it := plan.Items[0]
	if it.Action != "add" || it.Path != "/ip/pool" {
		t.Fatalf("unexpected item: %+v", it)
	}
	want := []string{"/ip/pool/add", "=name=hikrad-active", "=ranges=10.0.0.0/24"}
	if len(it.Sentence) != len(want) {
		t.Fatalf("sentence = %v, want %v", it.Sentence, want)
	}
	for i := range want {
		if it.Sentence[i] != want[i] {
			t.Fatalf("sentence = %v, want %v", it.Sentence, want)
		}
	}
	// Planning must never write.
	if len(ros.writes) != 0 {
		t.Fatalf("plan wrote to the router: %v", ros.writes)
	}
}

func TestPlanPoolSetsWhenRangesDiffer(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	ros.rows["/ip/pool/print"] = []map[string]string{
		{".id": "*7", "name": "hikrad-active", "ranges": "10.0.0.2-10.0.0.50"},
	}
	plan, err := a.PlanPool(ros, PoolPushInput{Name: "hikrad-active", Ranges: []string{"10.0.0.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != "set" {
		t.Fatalf("want one set item, got %+v", plan.Items)
	}
	it := plan.Items[0]
	if it.Sentence[0] != "/ip/pool/set" || it.Sentence[1] != "=.id=*7" {
		t.Fatalf("set must target the existing id: %v", it.Sentence)
	}
	if it.CurrentState != "ranges=10.0.0.2-10.0.0.50" {
		t.Fatalf("current state should show what the router has: %q", it.CurrentState)
	}
	if len(ros.writes) != 0 {
		t.Fatalf("plan wrote to the router: %v", ros.writes)
	}
}

func TestPlanPoolNoOpWhenAlreadyInSync(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	ros.rows["/ip/pool/print"] = []map[string]string{
		{".id": "*7", "name": "hikrad-active", "ranges": "10.0.0.0/24"},
	}
	plan, err := a.PlanPool(ros, PoolPushInput{Name: "hikrad-active", Ranges: []string{"10.0.0.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("an in-sync pool must plan nothing, got %+v", plan.Items)
	}
}

func TestPlanPoolJoinsMultipleRanges(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	plan, err := a.PlanPool(ros, PoolPushInput{
		Name: "p", Ranges: []string{"10.0.0.0/24", "10.1.0.0/24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Sentence[2] != "=ranges=10.0.0.0/24,10.1.0.0/24" {
		t.Fatalf("ranges must be comma-joined: %+v", plan.Items)
	}
}

func TestPlanPoolRejectsEmptyInput(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	if _, err := a.PlanPool(ros, PoolPushInput{Name: "", Ranges: []string{"10.0.0.0/24"}}); err == nil {
		t.Fatal("expected an error for an empty pool name")
	}
	if _, err := a.PlanPool(ros, PoolPushInput{Name: "p"}); err == nil {
		t.Fatal("expected an error for a pool with no ranges")
	}
}

func TestReadPoolsIsReadOnlyAndSkipsUnnamed(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	ros.rows["/ip/pool/print"] = []map[string]string{
		{".id": "*1", "name": "dhcp", "ranges": "10.0.0.2-10.0.0.254"},
		{".id": "*2", "name": "", "ranges": "junk"}, // unnamed rows are not pools we can reference
		{".id": "*3", "name": "hotspot", "ranges": "192.168.88.10-192.168.88.200"},
	}
	pools, err := a.ReadPools(ros)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 || pools[0].Name != "dhcp" || pools[1].Name != "hotspot" {
		t.Fatalf("unexpected pools: %+v", pools)
	}
	if pools[0].Ranges != "10.0.0.2-10.0.0.254" {
		t.Fatalf("router ranges must be kept verbatim: %q", pools[0].Ranges)
	}
	if len(ros.writes) != 0 {
		t.Fatalf("ReadPools must never write: %v", ros.writes)
	}
}

// FR-108: adopt must only import pools the PPPoE service references. This
// verifies the vendor half — the set of pool names named by PPP profiles
// (remote-/local-address), read-only, ignoring bare-IP addresses.
func TestPPPoEPoolNamesFromProfiles(t *testing.T) {
	a := For("mikrotik")
	ros := newFakeROS()
	ros.rows["/ppp/profile/print"] = []map[string]string{
		{".id": "*1", "name": "default", "remote-address": "pppoe-pool", "local-address": "10.0.0.1"},
		{".id": "*2", "name": "vip", "remote-address": "pppoe-vip"},
		{".id": "*3", "name": "static", "remote-address": "203.0.113.5"}, // a bare IP, not a pool
	}
	got, err := a.PPPoEPoolNames(ros)
	if err != nil {
		t.Fatal(err)
	}
	if !got["pppoe-pool"] || !got["pppoe-vip"] {
		t.Fatalf("expected the profile-referenced pools, got %v", got)
	}
	// A DHCP/hotspot pool no profile references must be absent so adopt skips it.
	if got["dhcp"] || got["hotspot"] {
		t.Fatalf("unreferenced pools must not appear: %v", got)
	}
	if len(ros.writes) != 0 {
		t.Fatalf("PPPoEPoolNames must never write: %v", ros.writes)
	}
}
