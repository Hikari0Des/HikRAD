package main

import (
	"encoding/binary"
	"net"
	"testing"
)

// decodeMNDP mirrors backend/internal/radius parseMNDP so this test fails if
// the harness's wire format drifts from what discovery parses.
func decodeMNDP(b []byte) map[uint16]string {
	out := map[uint16]string{}
	if len(b) < 4 {
		return out
	}
	p := 4
	for p+4 <= len(b) {
		typ := binary.BigEndian.Uint16(b[p : p+2])
		n := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		p += 4
		if p+n > len(b) {
			break
		}
		val := b[p : p+n]
		p += n
		if typ == 1 && n == 6 {
			out[typ] = net.HardwareAddr(val).String()
		} else {
			out[typ] = string(val)
		}
	}
	return out
}

func TestBuildMNDPRoundTrips(t *testing.T) {
	mac, _ := net.ParseMAC("AA:BB:CC:DD:EE:FF")
	payload := buildMNDP("CoreRouter", "7.11", "MikroTik", mac)
	got := decodeMNDP(payload)
	if got[5] != "CoreRouter" {
		t.Fatalf("identity = %q", got[5])
	}
	if got[7] != "7.11" {
		t.Fatalf("version = %q", got[7])
	}
	if got[1] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("mac = %q", got[1])
	}
	if got[8] != "MikroTik" {
		t.Fatalf("platform = %q", got[8])
	}
}
