package radius

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

// buildMNDP assembles a minimal MNDP payload with the TLVs we parse.
func buildMNDP(tlvs map[uint16][]byte) []byte {
	b := []byte{0, 0, 0, 0} // header
	for typ, val := range tlvs {
		var hdr [4]byte
		binary.BigEndian.PutUint16(hdr[0:2], typ)
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(val)))
		b = append(b, hdr[:]...)
		b = append(b, val...)
	}
	return b
}

func TestParseMNDP(t *testing.T) {
	mac := []byte{0xAA, 0xBB, 0xCC, 0x11, 0x22, 0x33}
	payload := buildMNDP(map[uint16][]byte{
		1: mac,
		5: []byte("CoreRouter"),
		7: []byte("7.11"),
		8: []byte("RouterOS"),
	})
	nb, ok := parseMNDP(payload)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if nb.Identity != "CoreRouter" || nb.Version != "7.11" || nb.Platform != "RouterOS" {
		t.Fatalf("parsed = %+v", nb)
	}
	if nb.MAC != "aa:bb:cc:11:22:33" {
		t.Fatalf("mac = %q", nb.MAC)
	}
}

func TestParseMNDPRejectsGarbage(t *testing.T) {
	if _, ok := parseMNDP([]byte{1, 2}); ok {
		t.Fatal("short payload should not parse")
	}
	if _, ok := parseMNDP(buildMNDP(map[uint16][]byte{99: []byte("x")})); ok {
		t.Fatal("payload with no identity/mac should not parse")
	}
}

func TestHostsInPrefix(t *testing.T) {
	hosts := hostsInPrefix(netip.MustParsePrefix("192.168.1.0/29"))
	// /29 = 8 addrs, minus network+broadcast = 6 usable.
	if len(hosts) != 6 {
		t.Fatalf("got %d hosts, want 6: %v", len(hosts), hosts)
	}
	if hosts[0] != "192.168.1.1" || hosts[len(hosts)-1] != "192.168.1.6" {
		t.Fatalf("range ends wrong: %v", hosts)
	}
}

func TestScanRangeRejectsHugeCIDR(t *testing.T) {
	if _, err := scanRange(nil, "10.0.0.0/8"); err == nil {
		t.Fatal("expected error for oversized CIDR")
	}
	if _, err := scanRange(nil, "not-a-cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}
