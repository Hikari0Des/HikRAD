package vendor

// Service discovery (FR-62.6): read a router's existing PPPoE/Hotspot service
// instances over the RouterOS API so an operator imports them instead of
// retyping their names.
//
// This is not a convenience. `ros_server_name` must match the router's own
// spelling exactly or an Access-Request cannot be attributed to its instance
// (C7), and a service's `address-pool` name must match a real `/ip pool` on the
// router or the login fails with "no address from ip pool" — both are
// hand-typed strings that must agree with a box the operator is reading off a
// terminal. Reading them removes the transcription step entirely.
//
// Strictly READ-ONLY: print sentences only, never add/set. The RouterOS paths
// and field names below are MikroTik's, which is exactly why this lives in the
// vendor package (FR-17).

import (
	"fmt"
	"strings"
)

// DiscoveredService is one service instance found on a router, in the shape the
// caller turns into a nas_services row.
type DiscoveredService struct {
	Service       string // "pppoe" | "hotspot"
	ROSServerName string // the router's own name for the instance
	Label         string // a human-friendly starting label (the operator can edit)
	Interface     string // interface note, for the operator's benefit
	// PoolName is the address pool the ROUTER has configured for this instance
	// (hotspot only — PPPoE pools live on the PPP profile, not the server). It
	// is reported so the operator can see the router's real pool name; HikRAD
	// does not create pools from it.
	PoolName string
	Disabled bool
}

// DiscoverServices reads the router's PPPoE and Hotspot server instances.
//
// A missing path is not an error: a PPPoE-only box has no /ip hotspot at all,
// and a hotspot-only box has no pppoe-server. Returning an error there would
// make discovery fail on exactly the single-service routers it should handle
// most easily — so each half degrades to "found none".
func (mikrotikAdapter) DiscoverServices(conn ROSConn) ([]DiscoveredService, error) {
	var out []DiscoveredService

	// PPPoE servers. The instance identity is the service-name: it is what a
	// client sends and what a PPPoE Access-Request can be matched on. An unnamed
	// server (RouterOS allows it) still resolves as the NAS's sole PPPoE
	// instance, so it is imported with an empty name rather than skipped.
	if rows, err := conn.Read("/interface/pppoe-server/server/print"); err == nil {
		for _, r := range rows {
			name := r["service-name"]
			out = append(out, DiscoveredService{
				Service:       "pppoe",
				ROSServerName: name,
				Label:         labelOr(name, r["interface"], "PPPoE"),
				Interface:     r["interface"],
				Disabled:      isTrue(r["disabled"]),
			})
		}
	}

	// Hotspot servers. `name` is what MikroTik puts in Called-Station-Id, which
	// is what C7 matches on — so it is the field that matters most here.
	if rows, err := conn.Read("/ip/hotspot/print"); err == nil {
		for _, r := range rows {
			name := r["name"]
			out = append(out, DiscoveredService{
				Service:       "hotspot",
				ROSServerName: name,
				Label:         labelOr(name, r["interface"], "Hotspot"),
				Interface:     r["interface"],
				PoolName:      r["address-pool"],
				Disabled:      isTrue(r["disabled"]),
			})
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("mikrotik: the router reports no PPPoE or Hotspot servers")
	}
	return out, nil
}

// CheckHealth reports router config that breaks HikRAD logins while HikRAD
// itself behaves correctly. Only the hotspot user-profile pool for now — the
// condition that caused the pilot outage and defeated three rounds of
// HikRAD-side debugging.
//
// Read-only, and it never fails the caller: a router that won't answer a probe
// yields no findings rather than an error, because "we couldn't check" must not
// look like "your router is broken".
func (mikrotikAdapter) CheckHealth(conn ROSConn) ([]HealthFinding, error) {
	var out []HealthFinding

	pool, exists, err := defaultHotspotUserProfilePool(conn)
	if err != nil {
		// No /ip hotspot at all (a PPPoE-only box) reads as an error here; that
		// is not a finding.
		return out, nil
	}
	switch {
	case pool == "":
		// Healthy: the client keeps the address the hotspot server assigned.
	case !exists:
		out = append(out, HealthFinding{
			Code: HealthHotspotUserProfilePoolMissing,
			Detail: fmt.Sprintf(
				"the default hotspot user profile assigns address-pool %q, which does not exist on this router; "+
					"every HikRAD hotspot login fails with \"no address from ip pool\" even though RADIUS accepts", pool),
			Fix: "/ip hotspot user profile set [find default=yes] address-pool=none",
		})
	default:
		out = append(out, HealthFinding{
			Code: HealthHotspotUserProfilePool,
			Detail: fmt.Sprintf(
				"the default hotspot user profile re-assigns addresses from pool %q at login, overriding each hotspot's own address-pool; "+
					"logins fail if it ever empties or is deleted", pool),
			Fix: "/ip hotspot user profile set [find default=yes] address-pool=none",
		})
	}
	return out, nil
}

// defaultHotspotUserProfilePool reads the address-pool of the router's DEFAULT
// hotspot user profile, and whether that pool actually exists.
//
// This is the login-time address source for every RADIUS-authenticated hotspot
// user (HikRAD sends no Mikrotik-Group, so they all land on `default`), and it
// OVERRIDES the hotspot server's own address-pool. It is invisible from
// everything an operator would naturally check: the server's pool looks healthy,
// the client already holds a DHCP address from it, `/ip pool print` shows plenty
// free, and RADIUS reports a clean accept — while every login fails with "no
// address from ip pool". That is exactly how the 2026-07-16 pilot outage hid
// (docs/ops/known-issues.md): the default profile referenced a deleted pool by
// its internal id.
//
// pool is "" / "none" when the profile assigns nothing, which is the healthy
// state HikRAD's own snippet configures.
func defaultHotspotUserProfilePool(conn ROSConn) (pool string, exists bool, err error) {
	rows, err := conn.Read("/ip/hotspot/user/profile/print")
	if err != nil {
		return "", true, err
	}
	for _, r := range rows {
		if !isTrue(r["default"]) {
			continue
		}
		pool = strings.TrimSpace(r["address-pool"])
		if pool == "" || strings.EqualFold(pool, "none") {
			return "", true, nil
		}
		// The router may report the pool by NAME or by internal id ("*1D") — the
		// latter is what a dangling reference to a deleted pool looks like. Ask
		// for the pool list and decide by membership rather than trusting either
		// spelling.
		pools, perr := conn.Read("/ip/pool/print")
		if perr != nil {
			// Can't prove it's broken; don't cry wolf.
			return pool, true, nil
		}
		for _, p := range pools {
			if p["name"] == pool || p[".id"] == pool {
				return pool, true, nil
			}
		}
		return pool, false, nil
	}
	return "", true, nil
}

// labelOr picks the friendliest available starting label.
func labelOr(name, iface, fallback string) string {
	if name != "" {
		return name
	}
	if iface != "" {
		return fallback + " (" + iface + ")"
	}
	return fallback
}

// isTrue reads RouterOS's boolean rendering. Its API returns "true"/"false";
// some paths render "yes"/"no".
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes":
		return true
	}
	return false
}
