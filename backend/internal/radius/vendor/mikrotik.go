package vendor

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"
)

// mikrotikAdapter is the certified v1 vendor (FR-17.2). Every Mikrotik-* VSA
// name in the codebase lives here (and in the FreeRADIUS dictionaries) — the
// vendor-isolation lint fails if one appears anywhere else.
type mikrotikAdapter struct{}

func (mikrotikAdapter) Name() string { return "mikrotik" }

// Apply encodes CoA/Disconnect reply intents onto p. Intents MikroTik cannot
// change in place (e.g. moving pools mid-session on some ROS builds) are left
// unset so the CoA caller falls back to Disconnect (FR-15.4).
func (mikrotikAdapter) Apply(p *radius.Packet, attrs []Attr) error {
	for _, a := range attrs {
		switch a.Intent {
		case IntentRateLimit:
			// Value is the full Mikrotik-Rate-Limit string. Phase 3 extends the
			// engine to fill the burst-rate/threshold/time slots MikroTik allows
			// after the base "rx/tx" pair; the adapter passes whatever it builds
			// through unchanged.
			if err := mikrotik.MikrotikRateLimit_SetString(p, a.Value); err != nil {
				return fmt.Errorf("mikrotik: set rate-limit: %w", err)
			}
		case IntentAddressPool:
			if err := rfc2869.FramedPool_SetString(p, a.Value); err != nil {
				return fmt.Errorf("mikrotik: set framed-pool: %w", err)
			}
		case IntentStaticIP:
			ip := net.ParseIP(a.Value)
			if ip == nil {
				return fmt.Errorf("mikrotik: static_ip %q is not a valid IP", a.Value)
			}
			if err := rfc2865.FramedIPAddress_Set(p, ip); err != nil {
				return fmt.Errorf("mikrotik: set framed-ip-address: %w", err)
			}
		case IntentSessionTimeout:
			secs, err := strconv.ParseUint(a.Value, 10, 32)
			if err != nil {
				return fmt.Errorf("mikrotik: session_timeout %q is not an integer: %w", a.Value, err)
			}
			if err := rfc2865.SessionTimeout_Set(p, rfc2865.SessionTimeout(secs)); err != nil {
				return fmt.Errorf("mikrotik: set session-timeout: %w", err)
			}
		case IntentRedirectExpired:
			// Adds the client to a MikroTik address-list the router's
			// walled-garden/expired-redirect rules match on.
			if err := mikrotik.MikrotikAddressList_SetString(p, a.Value); err != nil {
				return fmt.Errorf("mikrotik: set address-list: %w", err)
			}
		}
	}
	return nil
}

// Snippet renders the FR-14.2 copy-paste RouterOS bootstrap config. ROS 6.49+
// and 7.x share almost all of this syntax; the version only changes small
// details (noted inline) — real per-version validation against a router/CHR is
// the manual step in the Definition of Done.
func (mikrotikAdapter) Snippet(in SnippetInput) (string, error) {
	if in.RadiusServer == "" {
		return "", fmt.Errorf("mikrotik: snippet needs a RADIUS server address")
	}
	coaPort := in.CoAPort
	if coaPort == 0 {
		coaPort = 3799
	}
	interim := in.InterimSecs
	if interim == 0 {
		interim = 300
	}
	ros7 := in.ROSVersion != "6"

	var b strings.Builder
	fmt.Fprintf(&b, "# HikRAD RouterOS %s bootstrap for NAS %q\n",
		map[bool]string{true: "7.x", false: "6.49+"}[ros7], in.NASName)
	fmt.Fprintf(&b, "# Paste into the router terminal. Additive only — review before running.\n\n")

	// service=ppp for PPPoE NAS, service=hotspot,login for Hotspot NAS. A NAS
	// that also accepts Hotspot logins for flagged subscribers (FR-58) still
	// terminates its primary service here; the login service covers Hotspot.
	service := "ppp"
	if in.Type == "hotspot" {
		service = "hotspot,login"
	}
	src := ""
	if in.SrcAddress != "" {
		src = " src-address=" + in.SrcAddress
	}
	fmt.Fprintf(&b, "/radius add service=%s address=%s secret=\"%s\"%s timeout=3s\n",
		service, in.RadiusServer, in.Secret, src)
	fmt.Fprintf(&b, "/radius incoming set accept=yes port=%d\n\n", coaPort)

	switch in.Type {
	case "hotspot":
		b.WriteString("/ip hotspot profile set [find] use-radius=yes ")
		if ros7 {
			b.WriteString("radius-interim-update=" + secs(interim) + "\n")
		} else {
			// ROS 6 spells the interim knob differently on the hotspot profile.
			b.WriteString("radius-accounting=yes interim-update=" + secs(interim) + "\n")
		}
		for _, host := range in.WalledGarden {
			host = strings.TrimSpace(host)
			if host == "" {
				continue
			}
			fmt.Fprintf(&b, "/ip hotspot walled-garden add dst-host=%s action=allow\n", host)
		}
	default: // pppoe
		fmt.Fprintf(&b, "/ppp aaa set use-radius=yes accounting=yes interim-update=%s\n", secs(interim))
	}
	return b.String(), nil
}

func secs(n int) string { return strconv.Itoa(n) + "s" }

// ComposeRate renders the MikroTik-Rate-Limit string (FR-11). The MikroTik
// grammar is strictly positional:
//
//	rx/tx [rx-burst/tx-burst rx-thr/tx-thr rx-time/tx-time [priority [rx-min/tx-min]]]
//
// so burst needs the full rate/threshold/time triple, and priority/min-rate are
// only valid once burst is present. Segments beyond what the spec provides are
// ResolveService picks the nas_services instance an Access-Request belongs to
// (C7). This is the only place MikroTik's request-identification quirks live.
//
// How a MikroTik identifies the instance:
//   - Hotspot: Called-Station-Id carries the hotspot SERVER NAME (e.g.
//     "hotspot1"), which is what nas_services.ros_server_name stores. Some
//     builds append the AP MAC after a colon/space, so match on the first token.
//   - PPPoE: Called-Station-Id carries the PPPoE service-name when one is
//     configured, and is often empty/a MAC when it isn't.
//
// Resolution order, deliberately: an exact server-name match wins; failing
// that, a NAS running exactly ONE enabled instance of the requested kind is
// unambiguous, so it resolves (this is the single-service NAS every v1 install
// upgrades into, and the reason a v1 install's behaviour is unchanged).
// Anything else is ambiguous and returns false — the engine rejects rather than
// guess, because guessing hands the session the wrong zone's address pool.
func (mikrotikAdapter) ResolveService(q ServiceQuery, candidates []ServiceInstance) (ServiceInstance, bool) {
	kind := q.Service
	if kind != "hotspot" {
		kind = "pppoe" // anything not explicitly hotspot is a PPPoE-style request
	}
	var ofKind []ServiceInstance
	for _, c := range candidates {
		if c.Service == kind {
			ofKind = append(ofKind, c)
		}
	}
	if len(ofKind) == 0 {
		return ServiceInstance{}, false // C6 step 2: no instance of this kind
	}

	if name := mikrotikStationName(q.CalledStationID); name != "" {
		for _, c := range ofKind {
			if strings.EqualFold(c.ROSServerName, name) {
				return c, true
			}
		}
	}
	if len(ofKind) == 1 {
		return ofKind[0], true
	}
	return ServiceInstance{}, false // ambiguous: several instances, none matched
}

// mikrotikStationName extracts the server name from a Called-Station-Id.
// MikroTik renders it as "<server-name>" or "<server-name>:<ap-mac>" (and some
// builds use a space); a bare MAC means the router sent no server name, which
// is not a name — return "" so the caller falls back rather than trying to
// match a MAC against a server name.
func mikrotikStationName(called string) string {
	s := strings.TrimSpace(called)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ":;, \t"); i >= 0 {
		// A colon could be the MAC separator OR the name/MAC delimiter. Take the
		// first token either way: for "AA:BB:CC:.." that yields "AA", which no
		// server name matches, and looksLikeMAC below discards it anyway.
		s = s[:i]
	}
	if looksLikeMAC(called) {
		return ""
	}
	return s
}

// looksLikeMAC reports whether s is a bare MAC address (the Called-Station-Id a
// router sends when no service/server name is configured).
func looksLikeMAC(s string) bool {
	s = strings.TrimSpace(s)
	r := strings.NewReplacer(":", "", "-", "", ".", "")
	hex := r.Replace(s)
	if len(hex) != 12 {
		return false
	}
	for _, c := range hex {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// omitted (MikroTik defaults them). This is the ONLY place the burst syntax is
// assembled — the engine and CoA path stay vendor-neutral (FR-17).
func (mikrotikAdapter) ComposeRate(spec RateSpec) string {
	if spec.Rate == "" {
		return ""
	}
	parts := []string{spec.Rate}
	hasBurst := spec.BurstRate != "" && spec.BurstThreshold != "" && spec.BurstTime != ""
	if hasBurst {
		parts = append(parts, spec.BurstRate, spec.BurstThreshold, spec.BurstTime)
		switch {
		case spec.Priority != "" && spec.MinRate != "":
			parts = append(parts, spec.Priority, spec.MinRate)
		case spec.Priority != "":
			parts = append(parts, spec.Priority)
		case spec.MinRate != "":
			// min-rate is positionally after priority, so emit the MikroTik
			// default priority (8, lowest) to keep the string valid.
			parts = append(parts, "8", spec.MinRate)
		}
	}
	return strings.Join(parts, " ")
}

func init() { Register(mikrotikAdapter{}) }
