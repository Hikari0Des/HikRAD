package monitorsvc

import "testing"

// The hand-rolled BER codec is the risky part of the dependency-free SNMP
// client, so encode/decode is round-tripped without a live agent.

func TestEncodeDecodeInt(t *testing.T) {
	for _, v := range []int64{0, 1, 127, 128, 255, 256, -1, -128, 65535, 100000, 2147483647} {
		if got := beInt(encodeInt(v)); got != v {
			t.Fatalf("int round-trip %d -> %d", v, got)
		}
	}
}

func TestEncodeDecodeOID(t *testing.T) {
	for _, oid := range []string{
		"1.3.6.1.2.1.1.3.0", // sysUpTime
		"1.3.6.1.4.1.14988.1.1.1.3.1.1", // MikroTik-ish, large arc
		"1.3.6.1.2.1.1.1.0",
	} {
		enc, err := encodeOID(oid)
		if err != nil {
			t.Fatalf("encodeOID(%s): %v", oid, err)
		}
		got, err := decodeOID(enc)
		if err != nil {
			t.Fatalf("decodeOID: %v", err)
		}
		if got != oid {
			t.Fatalf("oid round-trip %s -> %s", oid, got)
		}
	}
}

func TestEncodeLengthShortLong(t *testing.T) {
	// Short form (< 128) is a single byte equal to the length.
	if b := encodeLength(5); len(b) != 1 || b[0] != 5 {
		t.Fatalf("short length wrong: %v", b)
	}
	// Long form sets the high bit + length-of-length.
	b := encodeLength(200)
	if b[0] != 0x81 || b[1] != 200 {
		t.Fatalf("long length wrong: %v", b)
	}
}

// Build a GET request, then a matching GetResponse for sysUpTime and confirm the
// parser walks the SEQUENCE → PDU → varbind and decodes the TimeTicks value.
func TestGetRequestResponseRoundTrip(t *testing.T) {
	oid := oidSysUpTime
	req, err := encodeGetRequest("public", 42, []string{oid})
	if err != nil {
		t.Fatalf("encodeGetRequest: %v", err)
	}
	if req[0] != tagSequence {
		t.Fatalf("request not a SEQUENCE: 0x%02x", req[0])
	}

	// Craft a GetResponse: version, community, PDU(reqID=42, es=0, ei=0,
	// varbinds[{oid, TimeTicks(12345)}]).
	oidBytes, _ := encodeOID(oid)
	vb := concat(tlv(tagOID, oidBytes), tlv(tagTimeTicks, encodeInt(12345)))
	varbinds := tlv(tagSequence, tlv(tagSequence, vb))
	pdu := concat(
		tlv(tagInteger, encodeInt(42)),
		tlv(tagInteger, encodeInt(0)),
		tlv(tagInteger, encodeInt(0)),
		varbinds,
	)
	msg := concat(
		tlv(tagInteger, encodeInt(1)),
		tlv(tagOctet, []byte("public")),
		tlv(tagGetResp, pdu),
	)
	resp := tlv(tagSequence, msg)

	vals, err := parseResponse(resp, 42)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	v, ok := vals[oid]
	if !ok {
		t.Fatalf("oid %s missing from response %v", oid, vals)
	}
	if got, ok := v.(int64); !ok || got != 12345 {
		t.Fatalf("value = %v (%T), want int64 12345", v, v)
	}
}

func TestParseResponse_RequestIDMismatch(t *testing.T) {
	oidBytes, _ := encodeOID(oidSysUpTime)
	vb := concat(tlv(tagOID, oidBytes), tlv(tagNull, nil))
	pdu := concat(tlv(tagInteger, encodeInt(7)), tlv(tagInteger, encodeInt(0)),
		tlv(tagInteger, encodeInt(0)), tlv(tagSequence, tlv(tagSequence, vb)))
	msg := concat(tlv(tagInteger, encodeInt(1)), tlv(tagOctet, []byte("public")), tlv(tagGetResp, pdu))
	resp := tlv(tagSequence, msg)
	if _, err := parseResponse(resp, 999); err == nil {
		t.Fatal("expected request-id mismatch error")
	}
}
