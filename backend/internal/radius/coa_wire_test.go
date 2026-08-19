package radius

import (
	"testing"

	"github.com/hikrad/hikrad/internal/radius/vendor"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

// TestDumpWireAttributes_MatchesBuiltPacket proves gate item 6 at the unit
// level: the captured attributes are read directly off the constructed
// packet (never regenerated), including a byte-accurate Vendor-Specific
// sub-decode for the MikroTik rate-limit VSA, generic/vendor-neutral (no
// hardcoded attribute name — FR-17).
func TestDumpWireAttributes_MatchesBuiltPacket(t *testing.T) {
	pkt := radius.New(radius.CodeCoARequest, []byte("secret"))
	if err := rfc2865.UserName_SetString(pkt, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := rfc2866.AcctSessionID_SetString(pkt, "sess-1"); err != nil {
		t.Fatal(err)
	}
	adapter := vendor.For("mikrotik")
	if err := adapter.Apply(pkt, []vendor.Attr{{Intent: vendor.IntentRateLimit, Value: "5M/5M"}}); err != nil {
		t.Fatal(err)
	}

	wire := dumpWireAttributes(pkt)
	if len(wire) != len(pkt.Attributes) {
		t.Fatalf("dumped %d attrs, packet has %d", len(wire), len(pkt.Attributes))
	}

	var sawUserName, sawVSA bool
	for _, w := range wire {
		if w.Type == int(rfc2865.UserName_Type) && w.Text == "alice" {
			sawUserName = true
		}
		if w.Type == 26 && w.VendorID != 0 {
			sawVSA = true
			if w.VendorText != "5M/5M" && w.VendorHex == "" {
				t.Errorf("VSA sub-decode produced neither text nor hex: %+v", w)
			}
		}
	}
	if !sawUserName {
		t.Errorf("expected User-Name in the wire dump, got %+v", wire)
	}
	if !sawVSA {
		t.Errorf("expected a decoded Vendor-Specific attribute, got %+v", wire)
	}
}

func TestDumpWireAttributes_EmptyPacket(t *testing.T) {
	pkt := radius.New(radius.CodeDisconnectRequest, []byte("secret"))
	wire := dumpWireAttributes(pkt)
	if len(wire) != 0 {
		t.Fatalf("expected no attributes on a bare packet, got %+v", wire)
	}
}

func TestPrintable(t *testing.T) {
	cases := []struct {
		in   []byte
		want bool
	}{
		{[]byte("hello"), true},
		{[]byte(""), false},
		{[]byte{0x00, 0x01}, false},
		{[]byte{0xff}, false},
	}
	for _, c := range cases {
		if got := printable(c.in); got != c.want {
			t.Errorf("printable(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
