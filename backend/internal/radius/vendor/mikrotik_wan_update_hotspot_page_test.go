package vendor

// v4-7 — vendor-level tests for the Internet tab (FR-145), Update tab
// (FR-147), and hotspot login page push (FR-148). See mikrotik_wizard_test.go
// for the fakeROSConn/no-hardware posture.

import (
	"errors"
	"testing"
)

func TestPlanWANUplink_StaticAddSkipsWhenAlreadyPresent(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/address/print"] = []map[string]string{{"interface": "ether1", "address": "203.0.113.5/30"}}
	plan, err := mikrotikAdapter{}.PlanWANUplink(conn, WizardWANInput{Interface: "ether1", Mode: "static", Address: "203.0.113.5/30"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("expected no-op for an already-present static address, got %+v", plan.Items)
	}
}

func TestPlanWANUplink_DHCPAdd(t *testing.T) {
	conn := newFakeROS()
	plan, err := mikrotikAdapter{}.PlanWANUplink(conn, WizardWANInput{Interface: "ether1", Mode: "dhcp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Path != "/ip/dhcp-client" || plan.Items[0].Action != "add" {
		t.Fatalf("expected one dhcp-client add item, got %+v", plan.Items)
	}
}

func TestPlanWANUplink_PPPoENeverLeaksPasswordInCommand(t *testing.T) {
	conn := newFakeROS()
	plan, err := mikrotikAdapter{}.PlanWANUplink(conn, WizardWANInput{
		Interface: "ether1", Mode: "pppoe", PPPoEUser: "isp-user", PPPoEPassword: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one item, got %+v", plan.Items)
	}
	if contains(plan.Items[0].Command, "s3cret") {
		t.Fatalf("the human-readable Command must never contain the plaintext password: %q", plan.Items[0].Command)
	}
	found := false
	for _, w := range plan.Items[0].Sentence {
		if w == "=password=s3cret" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the actual API Sentence to carry the real password")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestReadWANUplinks_MergesStaticDHCPPPPoE(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/ip/address/print"] = []map[string]string{{"interface": "ether1", "address": "203.0.113.5/30", "dynamic": "false"}}
	conn.rows["/ip/dhcp-client/print"] = []map[string]string{{"interface": "ether2", "address": "192.0.2.9/24", "gateway": "192.0.2.1", "status": "bound"}}
	conn.rows["/interface/pppoe-client/print"] = []map[string]string{{"name": "pppoe-out1", "running": "true"}}

	out, err := mikrotikAdapter{}.ReadWANUplinks(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 uplinks, got %d: %+v", len(out), out)
	}
}

func TestReadUpdateInfo_ReportsAvailability(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/system/package/update/print"] = []map[string]string{{"channel": "stable", "installed-version": "7.14.2", "latest-version": "7.15.1"}}
	conn.rows["/system/package/print"] = []map[string]string{{"name": "routeros", "version": "7.14.2"}}

	info, err := mikrotikAdapter{}.ReadUpdateInfo(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !info.UpdateAvailable {
		t.Fatal("expected update_available=true when latest differs from installed")
	}
	if len(conn.writes) != 0 {
		t.Fatalf("ReadUpdateInfo must never write, got %+v", conn.writes)
	}
}

func TestApplyRouterUpdate_IssuesChannelAndInstall(t *testing.T) {
	conn := newFakeROS()
	adapter := mikrotikAdapter{}
	if err := adapter.ApplyRouterUpdate(conn, "stable"); err != nil {
		t.Fatal(err)
	}
	if len(conn.writes) != 2 {
		t.Fatalf("expected exactly 2 writes (set channel, install), got %+v", conn.writes)
	}
	if conn.writes[0][0] != "/system/package/update/set" || conn.writes[1][0] != "/system/package/update/install" {
		t.Fatalf("unexpected write sequence: %+v", conn.writes)
	}
}

func TestPushHotspotPage_WritesFileAddPerFile(t *testing.T) {
	conn := newFakeROS()
	err := mikrotikAdapter{}.PushHotspotPage(conn, "hotspot1", map[string]string{"login.html": "<html></html>"})
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.writes) != 1 || conn.writes[0][0] != "/file/add" {
		t.Fatalf("expected one /file/add write, got %+v", conn.writes)
	}
	found := false
	for _, w := range conn.writes[0] {
		if w == "=name=hotspot/hotspot1/login.html" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the file to be written under hotspot/<server-name>/, got %+v", conn.writes[0])
	}
}

func TestPushHotspotPage_FallsBackToSetOnAlreadyExists(t *testing.T) {
	conn := newFakeROS()
	conn.writeErrs = map[int]error{0: errors.New("failure: already have such entry")}
	conn.rows["/file/print"] = []map[string]string{{".id": "*7", "name": "hotspot/hotspot1/login.html"}}

	err := mikrotikAdapter{}.PushHotspotPage(conn, "hotspot1", map[string]string{"login.html": "<html>v2</html>"})
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.writes) != 2 || conn.writes[1][0] != "/file/set" {
		t.Fatalf("expected a fallback /file/set on already-exists, got %+v", conn.writes)
	}
}
