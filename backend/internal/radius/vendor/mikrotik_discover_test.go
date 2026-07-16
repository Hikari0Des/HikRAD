package vendor

// FR-62.6 service-discovery tests. The point of discovery is that
// `ros_server_name` and the router's pool names stop being hand-typed, so these
// pin down that what the router reports is what comes back — verbatim.

import "testing"

func routerWith(rows map[string][]map[string]string) *fakeROSConn {
	c := newFakeROS()
	c.rows = rows
	return c
}

func TestDiscoverServices_MixedRouter(t *testing.T) {
	conn := routerWith(map[string][]map[string]string{
		"/interface/pppoe-server/server/print": {
			{"service-name": "hikrad-pppoe", "interface": "ether1", "disabled": "false"},
		},
		"/ip/hotspot/print": {
			{"name": "lobby", "interface": "bridge-lobby", "address-pool": "hs-lobby-pool", "disabled": "false"},
			{"name": "cafe", "interface": "bridge-cafe", "address-pool": "hs-cafe-pool", "disabled": "true"},
		},
	})
	got, err := For("mikrotik").DiscoverServices(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("found %d services, want 3: %+v", len(got), got)
	}

	ppp := got[0]
	if ppp.Service != "pppoe" || ppp.ROSServerName != "hikrad-pppoe" || ppp.Interface != "ether1" {
		t.Fatalf("pppoe instance = %+v", ppp)
	}
	if !containsService(got, "hotspot", "lobby") || !containsService(got, "hotspot", "cafe") {
		t.Fatalf("hotspot instances missing: %+v", got)
	}

	// The router's own pool name is what an operator otherwise retypes — and
	// mistyping it is the "no address from ip pool" login failure.
	lobby := find(t, got, "hotspot", "lobby")
	if lobby.PoolName != "hs-lobby-pool" {
		t.Fatalf("lobby pool = %q, want the router's hs-lobby-pool", lobby.PoolName)
	}
	// A disabled server is reported, not hidden: the operator should see it
	// exists and decide, rather than wonder why a zone went missing.
	cafe := find(t, got, "hotspot", "cafe")
	if !cafe.Disabled {
		t.Fatal("cafe is disabled on the router but was not reported as such")
	}
}

// A PPPoE-only box has no /ip hotspot at all. Discovery must not fail on the
// single-service router it should handle most easily.
func TestDiscoverServices_PPPoEOnlyRouter(t *testing.T) {
	conn := routerWith(map[string][]map[string]string{
		"/interface/pppoe-server/server/print": {
			{"service-name": "", "interface": "ether1", "disabled": "false"},
		},
	})
	got, err := For("mikrotik").DiscoverServices(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "pppoe" {
		t.Fatalf("got %+v, want one pppoe instance", got)
	}
	// An unnamed PPPoE server still resolves as the NAS's sole pppoe instance
	// (C7's fallback), so it is imported with an empty name, labelled by its
	// interface rather than skipped.
	if got[0].ROSServerName != "" {
		t.Fatalf("expected an empty server name, got %q", got[0].ROSServerName)
	}
	if got[0].Label != "PPPoE (ether1)" {
		t.Fatalf("label = %q, want it to fall back to the interface", got[0].Label)
	}
}

func TestDiscoverServices_HotspotOnlyRouter(t *testing.T) {
	conn := routerWith(map[string][]map[string]string{
		"/ip/hotspot/print": {{"name": "hotspot1", "interface": "bridge1", "address-pool": "dhcp"}},
	})
	got, err := For("mikrotik").DiscoverServices(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "hotspot" || got[0].ROSServerName != "hotspot1" {
		t.Fatalf("got %+v, want one hotspot instance named hotspot1", got)
	}
}

// A router reporting nothing is an error the operator must see — silently
// importing zero services would look like discovery "worked".
func TestDiscoverServices_NoServicesIsAnError(t *testing.T) {
	if _, err := For("mikrotik").DiscoverServices(newFakeROS()); err == nil {
		t.Fatal("expected an error when the router reports no servers")
	}
}

// RouterOS renders booleans as true/false on the API and yes/no on some paths.
func TestDiscoverServices_BooleanRenderings(t *testing.T) {
	for _, v := range []string{"true", "yes", "TRUE"} {
		conn := routerWith(map[string][]map[string]string{
			"/ip/hotspot/print": {{"name": "h", "disabled": v}},
		})
		got, err := For("mikrotik").DiscoverServices(conn)
		if err != nil {
			t.Fatal(err)
		}
		if !got[0].Disabled {
			t.Fatalf("disabled=%q was not read as true", v)
		}
	}
	for _, v := range []string{"false", "no", ""} {
		conn := routerWith(map[string][]map[string]string{
			"/ip/hotspot/print": {{"name": "h", "disabled": v}},
		})
		got, _ := For("mikrotik").DiscoverServices(conn)
		if got[0].Disabled {
			t.Fatalf("disabled=%q was not read as false", v)
		}
	}
}

// Discovery must never write to the router.
func TestDiscoverServices_IsReadOnly(t *testing.T) {
	conn := routerWith(map[string][]map[string]string{
		"/ip/hotspot/print": {{"name": "h"}},
	})
	if _, err := For("mikrotik").DiscoverServices(conn); err != nil {
		t.Fatal(err)
	}
	if len(conn.writes) != 0 {
		t.Fatalf("discovery wrote to the router: %+v", conn.writes)
	}
}

func containsService(list []DiscoveredService, service, name string) bool {
	for _, s := range list {
		if s.Service == service && s.ROSServerName == name {
			return true
		}
	}
	return false
}

func find(t *testing.T, list []DiscoveredService, service, name string) DiscoveredService {
	t.Helper()
	for _, s := range list {
		if s.Service == service && s.ROSServerName == name {
			return s
		}
	}
	t.Fatalf("no %s instance named %q in %+v", service, name, list)
	return DiscoveredService{}
}
