package vendor

// Hotspot/PPPoE server provisioning for the MikroTik adapter (v2 phase 2,
// FR-67.3/67.4, contract C8). PlanService/ApplyService are deliberately
// separate from PlanAutoSetup/ApplyAutoSetup: they describe ONE server
// object's own existence (interface binding, address pool, profile) rather
// than the shared RADIUS/AAA wiring FR-66 already covers — a create/edit here
// still calls the same RADIUS-wiring helpers so a newly provisioned server is
// never left unwired.
//
// Conflicts here are abort-only (never Resolvable): "another service already
// claims this identity" has no safe "update" meaning — you cannot update your
// way into two servers sharing one name. Never issues a /remove sentence
// (FR-67.6): edit only ever /sets fields on the object PlanService itself
// matched by name; nothing is ever deleted from the router.

import (
	"fmt"
)

// PlanService implements Adapter.PlanService for MikroTik.
func (a mikrotikAdapter) PlanService(conn ROSConn, in ServiceProvisionInput) (AutoSetupPlan, error) {
	switch in.Kind {
	case "pppoe":
		return a.planPPPoEServer(conn, in)
	case "hotspot":
		return a.planHotspotServer(conn, in)
	default:
		return AutoSetupPlan{}, fmt.Errorf("mikrotik: unknown service kind %q", in.Kind)
	}
}

// ApplyService implements Adapter.ApplyService for MikroTik — identical
// whole-apply-abort-on-first-failure contract as ApplyAutoSetup, sharing the
// exact same executor so the two never drift.
func (a mikrotikAdapter) ApplyService(conn ROSConn, plan AutoSetupPlan) []ApplyResult {
	return a.ApplyAutoSetup(conn, plan)
}

// --- PPPoE server ------------------------------------------------------

func (mikrotikAdapter) planPPPoEServer(conn ROSConn, in ServiceProvisionInput) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	rows, err := conn.Read("/interface/pppoe-server/server/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /interface/pppoe-server/server: %w", err)
	}
	var existing map[string]string
	for _, row := range rows {
		if row["service-name"] == in.ROSServerName {
			existing = row
			break
		}
	}

	switch {
	case existing == nil && in.Editing:
		// The panel only lets an operator edit a service HikRAD already knows
		// about; the router not having it anymore is a real conflict, not a
		// silent create — creating instead would surprise an operator who
		// thought they were editing.
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: "/interface/pppoe-server/server", Existing: "not found on router",
			Reason: fmt.Sprintf("no PPPoE server named %q exists on this router anymore; nothing was changed", in.ROSServerName),
		})
	case existing != nil && !in.Editing:
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: "/interface/pppoe-server/server", Existing: fmt.Sprintf("service-name=%s interface=%s", existing["service-name"], existing["interface"]),
			Reason: fmt.Sprintf("a PPPoE server named %q already exists on this router", in.ROSServerName),
		})
	case existing != nil: // editing an existing, matched object
		if existing["interface"] != in.Interface {
			plan.Items = append(plan.Items, PlanItem{
				Action: "set", Path: "/interface/pppoe-server/server",
				Command:      fmt.Sprintf("/interface pppoe-server server set [find service-name=%s] interface=%s", in.ROSServerName, in.Interface),
				CurrentState: fmt.Sprintf("interface=%s", existing["interface"]),
				Sentence:     []string{"/interface/pppoe-server/server/set", "=.id=" + existing[".id"], "=interface=" + in.Interface},
			})
		}
	default: // create
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/interface/pppoe-server/server",
			Command:      fmt.Sprintf("/interface pppoe-server server add service-name=%s interface=%s disabled=no", in.ROSServerName, in.Interface),
			CurrentState: "not present",
			Sentence:     []string{"/interface/pppoe-server/server/add", "=service-name=" + in.ROSServerName, "=interface=" + in.Interface, "=disabled=no"},
		})
	}

	// The shared PPP AAA wiring (C6) applies regardless of create/edit — a new
	// PPPoE server is useless without it, and an edited one may predate it.
	if len(plan.Conflicts) == 0 {
		aaaItem, aaaConflict, err := mikrotikAdapter{}.planPPPAAA(conn, in.Values)
		if err != nil {
			return plan, err
		}
		resolveConflict(&plan, nil, aaaItem, aaaConflict) // create/edit is abort-only (C8) — no resolutions map
	}
	return plan, nil
}

// --- Hotspot server ------------------------------------------------------

func (mikrotikAdapter) planHotspotServer(conn ROSConn, in ServiceProvisionInput) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	rows, err := conn.Read("/ip/hotspot/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ip/hotspot: %w", err)
	}
	var existing map[string]string
	for _, row := range rows {
		if row["name"] == in.ROSServerName {
			existing = row
			break
		}
	}
	if existing == nil && in.Editing {
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: "/ip/hotspot", Existing: "not found on router",
			Reason: fmt.Sprintf("no Hotspot server named %q exists on this router anymore; nothing was changed", in.ROSServerName),
		})
		return plan, nil
	}
	if existing != nil && !in.Editing {
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: "/ip/hotspot", Existing: fmt.Sprintf("name=%s interface=%s", existing["name"], existing["interface"]),
			Reason: fmt.Sprintf("a Hotspot server named %q already exists on this router", in.ROSServerName),
		})
		return plan, nil
	}

	// A dedicated profile per zone (named after the server, never "default")
	// is what actually escapes the multi-hotspot limitation FR-56.2 hit: every
	// server HikRAD provisions gets its own /ip hotspot profile from the
	// start, so there is never a "which profile is this zone's?" guess later.
	profileName := in.ROSServerName + "-hikrad"
	interim := in.Values.InterimSecs
	if interim == 0 {
		interim = 300
	}
	ros7 := in.Values.ROSVersion != "6"

	profRows, err := conn.Read("/ip/hotspot/profile/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ip/hotspot/profile: %w", err)
	}
	profileExists := false
	for _, row := range profRows {
		if row["name"] == profileName {
			profileExists = true
			break
		}
	}
	if !profileExists {
		sentence := []string{"/ip/hotspot/profile/add", "=name=" + profileName, "=use-radius=yes"}
		cmd := fmt.Sprintf("/ip hotspot profile add name=%s use-radius=yes", profileName)
		if ros7 {
			sentence = append(sentence, "=radius-interim-update="+secs(interim))
			cmd += " radius-interim-update=" + secs(interim)
		} else {
			sentence = append(sentence, "=radius-accounting=yes", "=interim-update="+secs(interim))
			cmd += " radius-accounting=yes interim-update=" + secs(interim)
		}
		plan.Items = append(plan.Items, PlanItem{Action: "add", Path: "/ip/hotspot/profile", Command: cmd, CurrentState: "not present", Sentence: sentence})

		// FR-62.7's guard, applied to this NEW profile from day one — this
		// server is never left in the trap the pilot hit on "default".
		plan.Items = append(plan.Items, PlanItem{
			Action: "set", Path: "/ip/hotspot/user/profile",
			Command:      fmt.Sprintf("/ip hotspot user profile set [find name=%s] address-pool=none", profileName),
			CurrentState: "created by this plan",
			Sentence:     []string{"/ip/hotspot/user/profile/set", "=numbers=" + profileName, "=address-pool=none"},
		})
	}

	poolAttr := "none"
	if in.PoolName != "" {
		poolAttr = in.PoolName
	}
	if existing == nil {
		if in.Interface == "" {
			return plan, fmt.Errorf("mikrotik: hotspot server create needs an interface")
		}
		sentence := []string{"/ip/hotspot/add", "=name=" + in.ROSServerName, "=interface=" + in.Interface,
			"=profile=" + profileName, "=address-pool=" + poolAttr, "=disabled=no"}
		cmd := fmt.Sprintf("/ip hotspot add name=%s interface=%s profile=%s address-pool=%s disabled=no",
			in.ROSServerName, in.Interface, profileName, poolAttr)
		plan.Items = append(plan.Items, PlanItem{Action: "add", Path: "/ip/hotspot", Command: cmd, CurrentState: "not present", Sentence: sentence})
	} else if existing["profile"] != profileName || existing["address-pool"] != poolAttr || (in.Interface != "" && existing["interface"] != in.Interface) {
		sentence := []string{"/ip/hotspot/set", "=.id=" + existing[".id"], "=profile=" + profileName, "=address-pool=" + poolAttr}
		cmd := fmt.Sprintf("/ip hotspot set [find name=%s] profile=%s address-pool=%s", in.ROSServerName, profileName, poolAttr)
		if in.Interface != "" {
			sentence = append(sentence, "=interface="+in.Interface)
			cmd += " interface=" + in.Interface
		}
		plan.Items = append(plan.Items, PlanItem{
			Action: "set", Path: "/ip/hotspot", Command: cmd,
			CurrentState: fmt.Sprintf("profile=%s address-pool=%s interface=%s", existing["profile"], existing["address-pool"], existing["interface"]),
			Sentence:     sentence,
		})
	}

	gardenItems, err := mikrotikAdapter{}.planWalledGarden(conn, in.Values)
	if err != nil {
		return plan, err
	}
	plan.Items = append(plan.Items, gardenItems...)

	return plan, nil
}
