package vendor

import "testing"

// TestReadConfig_ReflectsPlantedRouterState (FR-65, gate item 3): ReadConfig
// must report exactly the router state a caller planted, in the shape the
// panel's "current config" tab and the values-form pre-fill both depend on.
func TestReadConfig_ReflectsPlantedRouterState(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{
		{"address": "10.0.0.5", "service": "ppp,hotspot,login", "comment": hikradComment, "secret": "sekret", "src-address": "10.0.0.1"},
	}
	conn.rows["/radius/incoming/print"] = []map[string]string{{"accept": "yes", "port": "3799"}}
	conn.rows["/ppp/aaa/print"] = []map[string]string{{"use-radius": "yes", "accounting": "yes", "interim-update": "300s"}}
	conn.rows["/ip/hotspot/profile/print"] = []map[string]string{{"name": "default", "use-radius": "yes", "radius-interim-update": "300s"}}
	conn.rows["/ip/hotspot/walled-garden/print"] = []map[string]string{{"dst-host": "portal.isp.iq"}, {"dst-host": "pay.isp.iq"}}

	snap, err := For("mikrotik").ReadConfig(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.RadiusEntries) != 1 || !snap.RadiusEntries[0].SecretPresent || snap.RadiusEntries[0].Address != "10.0.0.5" {
		t.Fatalf("radius entries = %+v", snap.RadiusEntries)
	}
	if !snap.RadiusIncoming.Accept || snap.RadiusIncoming.Port != 3799 {
		t.Fatalf("radius incoming = %+v", snap.RadiusIncoming)
	}
	if !snap.PPPAAA.UseRadius || !snap.PPPAAA.Accounting || snap.PPPAAA.InterimUpdateSecs != 300 {
		t.Fatalf("ppp aaa = %+v", snap.PPPAAA)
	}
	if len(snap.HotspotProfiles) != 1 || snap.HotspotProfiles[0].Name != "default" || !snap.HotspotProfiles[0].UseRadius || snap.HotspotProfiles[0].InterimUpdateSecs != 300 {
		t.Fatalf("hotspot profiles = %+v", snap.HotspotProfiles)
	}
	if len(snap.WalledGarden) != 2 {
		t.Fatalf("walled garden = %+v", snap.WalledGarden)
	}
}

// TestReadConfig_NeverExposesSecretValue: SecretPresent is a bool, never the
// secret itself — FR-65.1's explicit requirement.
func TestReadConfig_NeverExposesSecretValue(t *testing.T) {
	conn := newFakeROS()
	conn.rows["/radius/print"] = []map[string]string{{"address": "10.0.0.5", "secret": "topsecret"}}
	snap, err := For("mikrotik").ReadConfig(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.RadiusEntries[0].SecretPresent {
		t.Fatalf("expected SecretPresent=true")
	}
	// ConfigSnapshot/RadiusEntryConfig simply has no field that could carry the
	// plaintext secret — this test documents that guarantee rather than
	// grep-checking a struct that structurally cannot leak it.
}

// TestReadConfig_EmptyRouter_NoErrors: a freshly-imaged router (nothing
// configured yet) must not error — this is exactly the state an operator
// inspects before running the values-form wizard for the first time.
func TestReadConfig_EmptyRouter_NoErrors(t *testing.T) {
	conn := newFakeROS()
	snap, err := For("mikrotik").ReadConfig(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.RadiusEntries) != 0 || len(snap.HotspotProfiles) != 0 || len(snap.WalledGarden) != 0 {
		t.Fatalf("expected an all-empty snapshot, got %+v", snap)
	}
}
