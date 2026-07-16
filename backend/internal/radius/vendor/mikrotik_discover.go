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
