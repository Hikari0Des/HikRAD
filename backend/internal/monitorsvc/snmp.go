package monitorsvc

// Minimal SNMP v2c GET client, dependency-free (BER built by hand over
// encoding/asn1 primitives we control). We only ever GET a handful of scalar
// OIDs — sysUpTime plus optional vendor CPU/mem gauges — so a full SNMP stack
// (and a new offline dependency) is unwarranted. v2c only in v1 (documented);
// v3 is out of scope. The engine talks to it through the SNMPClient interface so
// SNMP enrichment is best-effort: any failure yields no SNMP row and never
// affects the ICMP-driven up/down state machine.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

// Standard scalar OIDs we read. sysUpTime is universal; CPU/mem OIDs vary by
// vendor, so they're supplied per call (MikroTik/host-resources differ).
const (
	oidSysUpTime = "1.3.6.1.2.1.1.3.0" // TimeTicks (1/100 s)
)

// SNMPMetrics is the best-effort result of one SNMP poll.
type SNMPMetrics struct {
	OK        bool
	UptimeSec int64   // sysUpTime converted to seconds
	CPU       float64 // percent [0,100], 0 if not read
	Mem       float64 // percent [0,100], 0 if not read
}

// SNMPClient polls a target's SNMP agent. Implementations must respect ctx and
// return OK=false (not an error) when the agent is silent or the community is
// wrong — SNMP is enrichment, not reachability.
type SNMPClient interface {
	Poll(ctx context.Context, addr, community string) SNMPMetrics
}

// udpSNMP is the production v2c client.
type udpSNMP struct{}

// NewUDPSNMP returns the production SNMPClient.
func NewUDPSNMP() SNMPClient { return udpSNMP{} }

func (udpSNMP) Poll(ctx context.Context, addr, community string) SNMPMetrics {
	if community == "" {
		return SNMPMetrics{}
	}
	vals, err := snmpGet(ctx, net.JoinHostPort(addr, "161"), community, []string{oidSysUpTime})
	if err != nil {
		return SNMPMetrics{}
	}
	m := SNMPMetrics{OK: true}
	if v, ok := vals[oidSysUpTime]; ok {
		if ticks, ok := v.(int64); ok {
			m.UptimeSec = ticks / 100 // TimeTicks are hundredths of a second
		}
	}
	return m
}

// --- SNMP GET over UDP ------------------------------------------------------

// snmpGet performs one v2c GET round trip, returning OID→value. Values decode to
// int64 (INTEGER/Counter/Gauge/TimeTicks) or string (OCTET STRING/OID).
func snmpGet(ctx context.Context, addr, community string, oids []string) (map[string]any, error) {
	reqID := rand.Int31()
	req, err := encodeGetRequest(community, reqID, oids)
	if err != nil {
		return nil, err
	}

	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(snmpTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return parseResponse(buf[:n], reqID)
}

const snmpTimeout = 2 * time.Second

// BER tags we use.
const (
	tagInteger   = 0x02
	tagOctet     = 0x04
	tagNull      = 0x05
	tagOID       = 0x06
	tagSequence  = 0x30
	tagGetReq    = 0xA0
	tagGetResp   = 0xA2
	tagCounter32 = 0x41
	tagGauge32   = 0x42
	tagTimeTicks = 0x43
	tagCounter64 = 0x46
)

func encodeGetRequest(community string, reqID int32, oids []string) ([]byte, error) {
	var vbList []byte
	for _, o := range oids {
		oidBytes, err := encodeOID(o)
		if err != nil {
			return nil, err
		}
		vb := concat(tlv(tagOID, oidBytes), tlv(tagNull, nil))
		vbList = append(vbList, tlv(tagSequence, vb)...)
	}
	pdu := concat(
		tlv(tagInteger, encodeInt(int64(reqID))),
		tlv(tagInteger, encodeInt(0)), // error-status
		tlv(tagInteger, encodeInt(0)), // error-index
		tlv(tagSequence, vbList),      // variable-bindings
	)
	msg := concat(
		tlv(tagInteger, encodeInt(1)), // version 1 == v2c
		tlv(tagOctet, []byte(community)),
		tlv(tagGetReq, pdu),
	)
	return tlv(tagSequence, msg), nil
}

// parseResponse walks the response SEQUENCE → GetResponse PDU → varbinds.
func parseResponse(data []byte, wantReqID int32) (map[string]any, error) {
	body, _, err := expectTag(data, tagSequence)
	if err != nil {
		return nil, err
	}
	// version
	_, body, err = readValue(body, tagInteger)
	if err != nil {
		return nil, err
	}
	// community
	_, body, err = readValue(body, tagOctet)
	if err != nil {
		return nil, err
	}
	// PDU (GetResponse)
	pdu, _, err := expectTag(body, tagGetResp)
	if err != nil {
		return nil, err
	}
	// request-id
	ridRaw, pdu, err := readValue(pdu, tagInteger)
	if err != nil {
		return nil, err
	}
	if rid := int32(beInt(ridRaw)); rid != wantReqID {
		return nil, fmt.Errorf("snmp: request-id mismatch (got %d want %d)", rid, wantReqID)
	}
	// error-status, error-index
	esRaw, pdu, err := readValue(pdu, tagInteger)
	if err != nil {
		return nil, err
	}
	if es := beInt(esRaw); es != 0 {
		return nil, fmt.Errorf("snmp: error-status %d", es)
	}
	_, pdu, err = readValue(pdu, tagInteger) // error-index
	if err != nil {
		return nil, err
	}
	// variable-bindings SEQUENCE
	vbs, _, err := expectTag(pdu, tagSequence)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for len(vbs) > 0 {
		var vb []byte
		vb, vbs, err = expectTag(vbs, tagSequence)
		if err != nil {
			return nil, err
		}
		oidRaw, rest, err := readValue(vb, tagOID)
		if err != nil {
			return nil, err
		}
		oid, err := decodeOID(oidRaw)
		if err != nil {
			return nil, err
		}
		valTag, valContent, _, err := readAny(rest)
		if err != nil {
			return nil, err
		}
		out[oid] = decodeSNMPValue(valTag, valContent)
	}
	return out, nil
}

func decodeSNMPValue(tag byte, content []byte) any {
	switch tag {
	case tagInteger, tagCounter32, tagGauge32, tagTimeTicks, tagCounter64:
		return beInt(content)
	case tagOctet:
		return string(content)
	case tagOID:
		if s, err := decodeOID(content); err == nil {
			return s
		}
		return ""
	default:
		return nil
	}
}

// --- BER helpers ------------------------------------------------------------

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// tlv wraps content in a tag + definite-length header.
func tlv(tag byte, content []byte) []byte {
	return concat([]byte{tag}, encodeLength(len(content)), content)
}

func encodeLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte(n & 0xff)}, b...)
		n >>= 8
	}
	return append([]byte{byte(0x80 | len(b))}, b...)
}

// encodeInt encodes a signed integer in minimal two's-complement bytes.
func encodeInt(v int64) []byte {
	if v == 0 {
		return []byte{0}
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	i := 0
	if v > 0 {
		for i < 7 && b[i] == 0 && b[i+1]&0x80 == 0 {
			i++
		}
	} else {
		for i < 7 && b[i] == 0xff && b[i+1]&0x80 != 0 {
			i++
		}
	}
	return b[i:]
}

// beInt reads a big-endian two's-complement integer.
func beInt(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	var v int64
	if b[0]&0x80 != 0 {
		v = -1 // sign-extend
	}
	for _, x := range b {
		v = v<<8 | int64(x)
	}
	return v
}

func encodeOID(oid string) ([]byte, error) {
	parts := strings.Split(strings.TrimPrefix(oid, "."), ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("snmp: oid %q too short", oid)
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("snmp: bad oid arc %q", p)
		}
		nums[i] = n
	}
	out := []byte{byte(nums[0]*40 + nums[1])}
	for _, n := range nums[2:] {
		out = append(out, base128(n)...)
	}
	return out, nil
}

func base128(n uint64) []byte {
	if n == 0 {
		return []byte{0}
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte(n & 0x7f)}, b...)
		n >>= 7
	}
	for i := 0; i < len(b)-1; i++ {
		b[i] |= 0x80
	}
	return b
}

func decodeOID(b []byte) (string, error) {
	if len(b) == 0 {
		return "", errors.New("snmp: empty oid")
	}
	var sb strings.Builder
	first := b[0]
	fmt.Fprintf(&sb, "%d.%d", first/40, first%40)
	var n uint64
	for _, x := range b[1:] {
		n = n<<7 | uint64(x&0x7f)
		if x&0x80 == 0 {
			fmt.Fprintf(&sb, ".%d", n)
			n = 0
		}
	}
	return sb.String(), nil
}

// readAny reads one TLV, returning tag, content, and the remaining bytes.
func readAny(b []byte) (tag byte, content, rest []byte, err error) {
	if len(b) < 2 {
		return 0, nil, nil, errors.New("snmp: truncated tlv")
	}
	tag = b[0]
	length, hdr, err := parseLength(b[1:])
	if err != nil {
		return 0, nil, nil, err
	}
	start := 1 + hdr
	if start+length > len(b) {
		return 0, nil, nil, errors.New("snmp: tlv overruns buffer")
	}
	return tag, b[start : start+length], b[start+length:], nil
}

// expectTag reads a TLV and asserts its tag, returning content + remaining.
func expectTag(b []byte, want byte) (content, rest []byte, err error) {
	tag, content, rest, err := readAny(b)
	if err != nil {
		return nil, nil, err
	}
	if tag != want {
		return nil, nil, fmt.Errorf("snmp: expected tag 0x%02x got 0x%02x", want, tag)
	}
	return content, rest, nil
}

// readValue reads a TLV of the expected tag, returning its content + remaining.
func readValue(b []byte, want byte) (content, rest []byte, err error) {
	return expectTag(b, want)
}

func parseLength(b []byte) (length, hdrLen int, err error) {
	if len(b) == 0 {
		return 0, 0, errors.New("snmp: missing length")
	}
	if b[0]&0x80 == 0 {
		return int(b[0]), 1, nil
	}
	nb := int(b[0] & 0x7f)
	if nb == 0 || nb > 4 || 1+nb > len(b) {
		return 0, 0, errors.New("snmp: bad length header")
	}
	l := 0
	for i := 1; i <= nb; i++ {
		l = l<<8 | int(b[i])
	}
	return l, 1 + nb, nil
}
