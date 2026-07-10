package vendor

import (
	"strings"
	"testing"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"
)

func TestMikrotikApplyMapsIntents(t *testing.T) {
	a := For("mikrotik")
	p := radius.New(radius.CodeCoARequest, []byte("s"))
	err := a.Apply(p, []Attr{
		{Intent: IntentRateLimit, Value: "5M/5M"},
		{Intent: IntentSessionTimeout, Value: "3600"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mikrotik.MikrotikRateLimit_GetString(p); got != "5M/5M" {
		t.Fatalf("rate-limit = %q", got)
	}
	if got := rfc2865.SessionTimeout_Get(p); got != 3600 {
		t.Fatalf("session-timeout = %d", got)
	}
}

func TestMikrotikApplyStaticIPAndPool(t *testing.T) {
	a := For("mikrotik")
	p := radius.New(radius.CodeCoARequest, []byte("s"))
	if err := a.Apply(p, []Attr{
		{Intent: IntentStaticIP, Value: "10.1.2.3"},
		{Intent: IntentAddressPool, Value: "active"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := rfc2865.FramedIPAddress_Get(p); got.String() != "10.1.2.3" {
		t.Fatalf("framed-ip = %v", got)
	}
	if got := rfc2869.FramedPool_GetString(p); got != "active" {
		t.Fatalf("framed-pool = %q", got)
	}
}

func TestMikrotikApplyRejectsBadStaticIP(t *testing.T) {
	a := For("mikrotik")
	p := radius.New(radius.CodeCoARequest, []byte("s"))
	if err := a.Apply(p, []Attr{{Intent: IntentStaticIP, Value: "not-an-ip"}}); err == nil {
		t.Fatal("expected error for bad static IP")
	}
}

func TestForFallsBackToMikrotik(t *testing.T) {
	if For("nonexistent").Name() != "mikrotik" {
		t.Fatal("unknown vendor should fall back to mikrotik")
	}
	if For("").Name() != "mikrotik" {
		t.Fatal("empty vendor should fall back to mikrotik")
	}
}

func TestSnippetPPPoE(t *testing.T) {
	for _, ros := range []string{"6", "7"} {
		s, err := For("mikrotik").Snippet(SnippetInput{
			ROSVersion: ros, Type: "pppoe", NASName: "core", RadiusServer: "10.0.0.5",
			Secret: "sekret", CoAPort: 3799, InterimSecs: 300,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"/radius add service=ppp", "10.0.0.5", "sekret", "/radius incoming set accept=yes port=3799", "use-radius=yes"} {
			if !strings.Contains(s, want) {
				t.Fatalf("ros %s snippet missing %q:\n%s", ros, want, s)
			}
		}
	}
}

func TestSnippetHotspotWalledGarden(t *testing.T) {
	s, err := For("mikrotik").Snippet(SnippetInput{
		ROSVersion: "7", Type: "hotspot", NASName: "hs", RadiusServer: "10.0.0.5",
		Secret: "x", WalledGarden: []string{"portal.isp.iq", "pay.isp.iq"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"service=hotspot,login", "walled-garden add dst-host=portal.isp.iq", "walled-garden add dst-host=pay.isp.iq"} {
		if !strings.Contains(s, want) {
			t.Fatalf("hotspot snippet missing %q:\n%s", want, s)
		}
	}
}

func TestSnippetNeedsServer(t *testing.T) {
	if _, err := For("mikrotik").Snippet(SnippetInput{Type: "pppoe"}); err == nil {
		t.Fatal("expected error without RADIUS server")
	}
}
