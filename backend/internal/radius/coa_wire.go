package radius

// Wire-level attribute capture (FR-150 §4 frozen contract, gate item 6): the
// CoA Outbox inspect view must show exactly what went on the wire, proven
// against a packet capture, and never regenerate that view from the abstract
// intent at display time. dumpWireAttributes reads it straight off the
// layeh radius.Packet the dispatcher is about to send — generic byte-level
// decode only (attribute type, raw value, and a Vendor-Specific (type 26)
// sub-decode into vendor-id/vendor-type/vendor-value). It never names a
// concrete vendor attribute (that stays confined to internal/radius/vendor/,
// FR-17) — this is wire bytes, not semantics.

import (
	"encoding/binary"
	"encoding/hex"

	"layeh.com/radius"
)

// WireAttr is one RADIUS attribute exactly as it appeared on an outgoing
// packet, captured at send time and stored verbatim (coa_attempts.wire_attributes).
type WireAttr struct {
	Type       int    `json:"type"`
	Hex        string `json:"hex"`
	Text       string `json:"text,omitempty"`
	VendorID   uint32 `json:"vendor_id,omitempty"`
	VendorType int    `json:"vendor_type,omitempty"`
	VendorHex  string `json:"vendor_hex,omitempty"`
	VendorText string `json:"vendor_text,omitempty"`
}

// dumpWireAttributes captures every attribute on p in wire order. Called
// immediately after the packet is fully built (session-identity attributes +
// vendor.Apply's encoding) and before the UDP exchange, so what is stored is
// exactly what is transmitted regardless of the attempt's outcome.
func dumpWireAttributes(p *radius.Packet) []WireAttr {
	out := make([]WireAttr, 0, len(p.Attributes))
	for _, avp := range p.Attributes {
		wa := WireAttr{Type: int(avp.Type), Hex: hex.EncodeToString(avp.Attribute)}
		if printable(avp.Attribute) {
			wa.Text = string(avp.Attribute)
		}
		// RFC 2865 §5.26 Vendor-Specific: 4-byte vendor id, then one or more
		// (vendor-type byte, vendor-length byte, vendor-value) sub-TLVs. Only
		// the first sub-TLV is decoded — every intent this dispatcher sends
		// carries exactly one.
		if avp.Type == 26 && len(avp.Attribute) >= 6 {
			vendorID := binary.BigEndian.Uint32(avp.Attribute[0:4])
			rest := avp.Attribute[4:]
			vType := int(rest[0])
			vLen := int(rest[1])
			if vLen >= 2 && vLen <= len(rest) {
				vVal := rest[2:vLen]
				wa.VendorID = vendorID
				wa.VendorType = vType
				wa.VendorHex = hex.EncodeToString(vVal)
				if printable(vVal) {
					wa.VendorText = string(vVal)
				}
			}
		}
		out = append(out, wa)
	}
	return out
}

// printable reports whether b is entirely printable ASCII, worth showing as
// text alongside the hex dump (most RADIUS attribute values are).
func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
