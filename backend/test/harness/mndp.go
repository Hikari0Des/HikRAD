package main

// MNDP-announce mode (gate item 7): the harness impersonates a MikroTik
// broadcasting a Neighbor Discovery packet on UDP 5678 so hikrad-api's
// POST /api/v1/nas/discover picks it up and pre-fills the wizard. Read-only on
// both sides — nothing is ever sent to a real router.

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// buildMNDP assembles an MNDP payload: a 4-byte header followed by TLVs of
// {u16 type, u16 length, value}. Types match the backend parser
// (1=MAC, 5=identity, 7=version, 8=platform).
func buildMNDP(identity, version, platform string, mac net.HardwareAddr) []byte {
	b := []byte{0, 0, 0, 0} // header
	add := func(typ uint16, val []byte) {
		var hdr [4]byte
		binary.BigEndian.PutUint16(hdr[0:2], typ)
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(val)))
		b = append(b, hdr[:]...)
		b = append(b, val...)
	}
	if len(mac) == 6 {
		add(1, mac)
	}
	add(5, []byte(identity))
	add(7, []byte(version))
	add(8, []byte(platform))
	return b
}

// runMNDPAnnounce broadcasts an MNDP packet to target (host:port) every second
// for duration, so a discovery scan running concurrently sees it.
func runMNDPAnnounce(target, identity, version string, duration time.Duration) int {
	if target == "" {
		target = "255.255.255.255:5678"
	}
	dst, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		fmt.Printf("resolve %q: %v\n", target, err)
		return 2
	}
	conn, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		fmt.Printf("dial %q: %v\n", target, err)
		return 2
	}
	defer conn.Close()

	mac, _ := net.ParseMAC("AA:BB:CC:DD:EE:FF")
	payload := buildMNDP(identity, version, "MikroTik", mac)

	deadline := time.Now().Add(duration)
	sent := 0
	for time.Now().Before(deadline) {
		if _, err := conn.Write(payload); err != nil {
			fmt.Printf("send: %v\n", err)
			return 1
		}
		sent++
		fmt.Printf("MNDP announce -> %s (identity=%q version=%q)\n", target, identity, version)
		time.Sleep(time.Second)
	}
	fmt.Printf("done: %d announcements sent\n", sent)
	return 0
}
