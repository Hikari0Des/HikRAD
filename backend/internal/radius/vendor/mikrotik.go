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
//
// A NAS may run several service instances (FR-62 / C8), so the output is: one
// shared /radius block whose service list covers every enabled kind, one PPPoE
// AAA block when any pppoe instance is enabled, and one /ip hotspot block per
// hotspot instance (each addressed by its own server name, so a two-zone router
// gets both configured in one paste).
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
	services := in.services()

	var b strings.Builder
	fmt.Fprintf(&b, "# HikRAD RouterOS %s bootstrap for NAS %q\n",
		map[bool]string{true: "7.x", false: "6.49+"}[ros7], in.NASName)
	fmt.Fprintf(&b, "# Paste into the router terminal. Additive only — review before running.\n\n")

	// One /radius client covers every service this NAS terminates: service= is a
	// list, so a router doing PPPoE and hotspot needs ppp,hotspot,login — not one
	// client per instance (RouterOS would query the same server repeatedly).
	var kinds []string
	if anyOfKind(services, "pppoe") {
		kinds = append(kinds, "ppp")
	}
	if anyOfKind(services, "hotspot") {
		kinds = append(kinds, "hotspot", "login")
	}
	if len(kinds) == 0 {
		return "", fmt.Errorf("mikrotik: snippet needs at least one enabled service")
	}
	src := ""
	if in.SrcAddress != "" {
		src = " src-address=" + in.SrcAddress
	}
	fmt.Fprintf(&b, "/radius add service=%s address=%s secret=\"%s\"%s timeout=3s\n",
		strings.Join(kinds, ","), in.RadiusServer, in.Secret, src)
	fmt.Fprintf(&b, "/radius incoming set accept=yes port=%d\n\n", coaPort)

	if anyOfKind(services, "pppoe") {
		b.WriteString("# PPPoE\n")
		fmt.Fprintf(&b, "/ppp aaa set use-radius=yes accounting=yes interim-update=%s\n\n", secs(interim))
	}

	for _, s := range services {
		if s.Service != "hotspot" {
			continue
		}
		writeHotspotBlock(&b, s, in.WalledGarden, interim, ros7)
	}
	return b.String(), nil
}

// writeHotspotBlock renders one hotspot instance. The server profile is
// addressed by the instance's own ROS server name where known, so configuring a
// second zone cannot silently re-point the first; only a NAS with no named
// server falls back to [find] (v1's single-hotspot behaviour).
func writeHotspotBlock(b *strings.Builder, s ServiceSnippet, walledGarden []string, interim int, ros7 bool) {
	name := strings.TrimSpace(s.ROSServerName)
	title := name
	if s.Label != "" {
		title = s.Label
	}
	if title == "" {
		title = "hotspot"
	}
	fmt.Fprintf(b, "# Hotspot: %s\n", title)
	if s.Interface != "" {
		fmt.Fprintf(b, "#   interface: %s\n", s.Interface)
	}

	profileSel := "[find]"
	if name != "" {
		// Resolve the profile via the named server rather than assuming there is
		// only one hotspot profile on the box.
		profileSel = fmt.Sprintf("[/ip hotspot get [find name=%q] profile]", name)
	}
	fmt.Fprintf(b, "/ip hotspot profile set %s use-radius=yes ", profileSel)
	if ros7 {
		b.WriteString("radius-interim-update=" + secs(interim) + "\n")
	} else {
		// ROS 6 spells the interim knob differently on the hotspot profile.
		b.WriteString("radius-accounting=yes interim-update=" + secs(interim) + "\n")
	}
	// The hotspot USER profile decides the login-time address, and it OVERRIDES
	// the hotspot server's own address-pool. A RADIUS-authenticated user lands on
	// the `default` user profile (HikRAD sends no Mikrotik-Group), so if that
	// profile carries an address-pool, every HikRAD login on this router is
	// re-assigned from it — and if it names a pool that no longer exists, every
	// login fails with "no address from ip pool" while RADIUS reports a clean
	// accept. That is a real pilot outage (2026-07-16, docs/ops/known-issues.md):
	// the router's default profile pointed at a deleted pool, and the DHCP
	// address the client already held looked like proof the pool was fine.
	//
	// Setting it to none is correct either way: with no HikRAD pool the client
	// keeps the address the hotspot server's pool already gave it, and with one
	// the Framed-Pool reply takes precedence regardless.
	b.WriteString("/ip hotspot user profile set [find default=yes] address-pool=none\n")
	if s.PoolName != "" {
		fmt.Fprintf(b, "#   addresses come from HikRAD's per-service pool %q (reply attribute)\n", s.PoolName)
	} else {
		b.WriteString("#   addresses come from this hotspot's own address-pool (HikRAD sends none)\n")
	}
	for _, host := range walledGarden {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		fmt.Fprintf(b, "/ip hotspot walled-garden add dst-host=%s action=allow\n", host)
	}
	b.WriteString("\n")
}

// services normalizes the input to the C8 instance list, falling back to the
// legacy single Type for callers that predate FR-62.
func (in SnippetInput) services() []ServiceSnippet {
	if len(in.Services) > 0 {
		return in.Services
	}
	kind := in.Type
	if kind == "" {
		kind = "pppoe"
	}
	return []ServiceSnippet{{Service: kind}}
}

func anyOfKind(services []ServiceSnippet, kind string) bool {
	for _, s := range services {
		if s.Service == kind {
			return true
		}
	}
	return false
}

func secs(n int) string { return strconv.Itoa(n) + "s" }

// ComposeRate renders the MikroTik-Rate-Limit string (FR-11). The MikroTik
// grammar is strictly positional:
//
//	rx/tx [rx-burst/tx-burst rx-thr/tx-thr rx-time/tx-time [priority [rx-min/tx-min]]]
//
// so burst needs the full rate/threshold/time triple, and priority/min-rate are
// only valid once burst is present. Segments beyond what the spec provides are
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

func init() { Register(mikrotikAdapter{}) }
