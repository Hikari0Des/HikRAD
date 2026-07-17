package vendor

import (
	"testing"
)

// fakeROSConn is an in-memory RouterOS device for auto-setup tests — no
// network, no CHR image. Read(path) looks up rows registered for that print
// path; Write records the sentence and can be scripted to fail (wrong
// credentials / mid-apply router error).
type fakeROSConn struct {
	rows      map[string][]map[string]string
	writes    [][]string
	writeErrs map[int]error // index into writes -> error to return for that call
	closed    bool
}

func newFakeROS() *fakeROSConn { return &fakeROSConn{rows: map[string][]map[string]string{}} }

func (f *fakeROSConn) Read(sentence ...string) ([]map[string]string, error) {
	return f.rows[sentence[0]], nil
}

func (f *fakeROSConn) Write(sentence ...string) (map[string]string, error) {
	idx := len(f.writes)
	f.writes = append(f.writes, sentence)
	if err, ok := f.writeErrs[idx]; ok {
		return nil, err
	}
	return map[string]string{}, nil
}

func (f *fakeROSConn) Close() error { f.closed = true; return nil }

func baseInput() SnippetInput {
	return SnippetInput{
		ROSVersion: "7", Type: "pppoe", NASName: "core", RadiusServer: "10.0.0.5",
		Secret: "sekret", CoAPort: 3799, InterimSecs: 300,
	}
}

func TestPlanAutoSetup_FreshRouter_AllAdditive(t *testing.T) {
	conn := newFakeROS()
	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("expected no conflicts on a fresh router, got %+v", plan.Conflicts)
	}
	if len(plan.Items) != 3 { // /radius add, /radius/incoming set, /ppp/aaa set
		t.Fatalf("expected 3 items (radius + incoming + ppp aaa), got %d: %+v", len(plan.Items), plan.Items)
	}
	for _, it := range plan.Items {
		if it.Action != "add" && it.Action != "set" {
			t.Fatalf("unexpected action %q", it.Action)
		}
		if len(it.Sentence) == 0 {
			t.Fatalf("item %+v missing its apply sentence", it)
		}
	}
}

func TestPlanAutoSetup_AlreadyApplied_NoItems(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{"address": "10.0.0.5", "service": "ppp", "secret": "sekret", "comment": hikradComment},
	}
	conn.rows["/radius/incoming/print"] = []map[string]string{{"accept": "yes", "port": "3799"}}
	conn.rows["/ppp/aaa/print"] = []map[string]string{{"use-radius": "yes", "accounting": "yes", "interim-update": "300s"}}

	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 || len(plan.Items) != 0 {
		t.Fatalf("expected a fully-idempotent no-op plan, got items=%+v conflicts=%+v", plan.Items, plan.Conflicts)
	}
}

func TestPlanAutoSetup_ForeignRadiusEntry_Conflicts(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{"address": "10.0.0.5", "service": "ppp", "secret": "someone-elses-secret", "comment": "manually configured"},
	}
	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected exactly one conflict for the planted foreign /radius entry, got %+v", plan.Conflicts)
	}
	if plan.Conflicts[0].Path != "/radius" {
		t.Fatalf("conflict path = %q, want /radius", plan.Conflicts[0].Path)
	}
}

func TestPlanAutoSetup_IncomingPortMismatch_Conflicts(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/incoming/print"] = []map[string]string{{"accept": "yes", "port": "1700"}}
	plan, err := For("mikrotik").PlanAutoSetup(conn, baseInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range plan.Conflicts {
		if c.Path == "/radius/incoming" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a /radius/incoming conflict for the mismatched port, got %+v", plan.Conflicts)
	}
}

func TestPlanAutoSetup_HotspotWalledGarden_Additive(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{{".id": "*1", "name": "default", "use-radius": "no"}}
	conn.rows["/ip/hotspot/walled-garden/print"] = []map[string]string{{"dst-host": "portal.isp.iq"}}

	in := baseInput()
	in.Type = "hotspot"
	in.WalledGarden = []string{"portal.isp.iq", "pay.isp.iq"}

	plan, err := For("mikrotik").PlanAutoSetup(conn, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	var gardenAdds int
	for _, it := range plan.Items {
		if it.Path == "/ip/hotspot/walled-garden" {
			gardenAdds++
		}
	}
	if gardenAdds != 1 {
		t.Fatalf("expected exactly 1 new walled-garden entry (pay.isp.iq; portal.isp.iq already present), got %d in %+v", gardenAdds, plan.Items)
	}
}

func TestApplyAutoSetup_StopsOnFirstFailure(t *testing.T) {
	conn := newFakeROS()
	conn.writeErrs = map[int]error{1: errWriteFailed}
	plan := AutoSetupPlan{Items: []PlanItem{
		{Action: "add", Path: "/radius", Sentence: []string{"/radius/add"}},
		{Action: "set", Path: "/radius/incoming", Sentence: []string{"/radius/incoming/set"}},
		{Action: "add", Path: "/ip/hotspot/walled-garden", Sentence: []string{"/ip/hotspot/walled-garden/add"}},
	}}
	results := For("mikrotik").ApplyAutoSetup(conn, plan)
	if len(results) != 2 {
		t.Fatalf("expected apply to stop after the failing 2nd item, got %d results: %+v", len(results), results)
	}
	if !results[0].OK || results[1].OK {
		t.Fatalf("expected [ok, failed], got %+v", results)
	}
	if len(conn.writes) != 2 {
		t.Fatalf("expected exactly 2 write sentences sent (never reaching the 3rd), got %d", len(conn.writes))
	}
}

var errWriteFailed = &deviceErr{"failure: simulated router error"}

type deviceErr struct{ msg string }

func (e *deviceErr) Error() string { return e.msg }

func TestSupportsInPlace_QuirkMatrix(t *testing.T) {
	a := For("mikrotik")
	cases := []struct {
		ros, nasType, intent string
		want                 bool
	}{
		{"7", "pppoe", IntentRateLimit, true},
		{"6", "pppoe", IntentRateLimit, true},
		{"7", "hotspot", IntentRateLimit, false},
		{"6", "hotspot", IntentRateLimit, false},
		{"7", "pppoe", IntentAddressPool, false},
		{"6", "hotspot", IntentAddressPool, false},
		{"7", "pppoe", IntentSessionTimeout, true},
		{"7", "hotspot", IntentRedirectExpired, true},
	}
	for _, c := range cases {
		if got := a.SupportsInPlace(c.ros, c.nasType, c.intent); got != c.want {
			t.Errorf("SupportsInPlace(%q,%q,%q) = %v, want %v", c.ros, c.nasType, c.intent, got, c.want)
		}
	}
}
