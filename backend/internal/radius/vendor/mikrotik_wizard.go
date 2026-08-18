package vendor

// The FR-144 setup wizard's detect + combined plan for the MikroTik adapter.
// Every write here follows the exact same rule as mikrotik_autosetup.go:
// print first, classify into no-op/additive/conflict, and an "update"
// resolution only ever /sets an object HikRAD already found — nothing here
// is /remove, ever. RADIUS wiring and per-server creation are NOT
// reimplemented in this file; PlanWizard calls PlanAutoSetup/PlanService and
// folds their results in, so the wizard and the standalone auto-setup/
// service-provision flows can never drift into describing two different
// "desired state"s for the same router object.

import (
	"fmt"
	"strconv"
	"strings"
)

// DetectWizard implements Adapter.DetectWizard (gate item 2: "already-correct
// steps are skipped, not re-pushed"). Coarse, router-wide checks — "does this
// step's object exist at all" — run once at wizard open, before any per-field
// form value is known; the review step's PlanWizard is what does the
// precise, value-aware diff once the operator has actually filled in a step.
func (a mikrotikAdapter) DetectWizard(conn ROSConn) (WizardDetection, error) {
	var d WizardDetection

	if rows, err := conn.Read("/interface/bridge/print"); err == nil {
		if len(rows) > 0 {
			d.Bridge = WizardStepStatus{true, fmt.Sprintf("%d bridge(s) already configured: %s", len(rows), bridgeNames(rows))}
		} else {
			d.Bridge = WizardStepStatus{false, "no bridge configured"}
		}
	}

	if rows, err := conn.Read("/ip/dns/print"); err == nil && len(rows) > 0 {
		servers := rows[0]["servers"]
		if servers != "" {
			d.DNS = WizardStepStatus{true, "DNS servers already set: " + servers}
		} else {
			d.DNS = WizardStepStatus{false, "no DNS servers configured"}
		}
	}

	if rows, err := conn.Read("/system/ntp/client/print"); err == nil && len(rows) > 0 {
		if rows[0]["enabled"] == "yes" {
			d.NTP = WizardStepStatus{true, "NTP client already enabled"}
		} else {
			d.NTP = WizardStepStatus{false, "NTP client disabled"}
		}
	}

	if uplinks, err := a.ReadWANUplinks(conn); err == nil {
		if len(uplinks) > 0 {
			d.WAN = WizardStepStatus{true, fmt.Sprintf("%d WAN uplink(s) already configured", len(uplinks))}
		} else {
			d.WAN = WizardStepStatus{false, "no WAN uplink configured"}
		}
	}

	if cfg, err := a.ReadConfig(conn); err == nil {
		ours := false
		for _, e := range cfg.RadiusEntries {
			if strings.HasPrefix(e.Comment, hikradComment) {
				ours = true
				break
			}
		}
		if ours {
			d.RADIUS = WizardStepStatus{true, "a HikRAD-tagged /radius entry already exists"}
		} else {
			d.RADIUS = WizardStepStatus{false, "no HikRAD RADIUS entry found"}
		}
	}

	if found, err := a.DiscoverServices(conn); err == nil {
		if len(found) > 0 {
			d.Services = WizardStepStatus{true, fmt.Sprintf("%d service(s) already present on the router", len(found))}
		} else {
			d.Services = WizardStepStatus{false, "no PPPoE/Hotspot server found on the router"}
		}
	}

	return d, nil
}

func bridgeNames(rows []map[string]string) string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r["name"])
	}
	return strings.Join(names, ", ")
}

// PlanWizard implements Adapter.PlanWizard — see this file's package doc.
func (a mikrotikAdapter) PlanWizard(conn ROSConn, in WizardInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	fold := func(p AutoSetupPlan) {
		plan.Items = append(plan.Items, p.Items...)
		plan.Conflicts = append(plan.Conflicts, p.Conflicts...)
	}

	if in.Bridge != nil {
		p, err := a.planBridgeSetup(conn, *in.Bridge, resolutions)
		if err != nil {
			return plan, err
		}
		fold(p)
	}
	if in.DNS != nil {
		p, err := a.planDNSSetup(conn, *in.DNS, resolutions)
		if err != nil {
			return plan, err
		}
		fold(p)
	}
	if in.NTP != nil {
		p, err := a.planNTPSetup(conn, *in.NTP, resolutions)
		if err != nil {
			return plan, err
		}
		fold(p)
	}
	for _, w := range in.WAN {
		p, err := a.PlanWANUplink(conn, w)
		if err != nil {
			return plan, err
		}
		fold(p)
	}

	// RADIUS wiring (steps 7-8's shared /radius block) — identical logic to
	// standalone auto-setup, called here rather than duplicated.
	if in.RadiusValues.RadiusServer != "" {
		p, err := a.PlanAutoSetup(conn, in.RadiusValues, resolutions)
		if err != nil {
			return plan, err
		}
		fold(p)
	}

	// Per-server creation (step 8's actual PPPoE/Hotspot server objects) —
	// identical logic to the standalone FR-67.3 create flow, called here
	// rather than duplicated.
	for _, svc := range in.Services {
		p, err := a.PlanService(conn, svc)
		if err != nil {
			return plan, err
		}
		fold(p)
	}

	if in.Firewall != nil {
		p, err := a.planFirewallWizard(conn, *in.Firewall, resolutions)
		if err != nil {
			return plan, err
		}
		fold(p)
	}

	for _, ipv6 := range in.IPv6 {
		if !ipv6.Enabled {
			continue
		}
		p, err := a.PlanIPv6(conn, ipv6)
		if err != nil {
			return plan, err
		}
		fold(p)
	}

	return plan, nil
}

// --- bridge + bridge ports ---------------------------------------------------

func (a mikrotikAdapter) planBridgeSetup(conn ROSConn, in WizardBridgeInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	if in.Name == "" {
		return plan, nil
	}
	rows, err := conn.Read("/interface/bridge/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /interface/bridge: %w", err)
	}
	exists := false
	for _, r := range rows {
		if r["name"] == in.Name {
			exists = true
			break
		}
	}
	if !exists {
		sentence := []string{"/interface/bridge/add", "=name=" + in.Name, "=comment=" + hikradComment}
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/interface/bridge",
			Command:      fmt.Sprintf("/interface bridge add name=%s", in.Name),
			CurrentState: "not present", Sentence: sentence,
		})
	}

	portRows, err := conn.Read("/interface/bridge/port/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /interface/bridge/port: %w", err)
	}
	memberOf := map[string]string{} // interface -> bridge it's already a member of
	for _, r := range portRows {
		memberOf[r["interface"]] = r["bridge"]
	}
	for _, port := range in.Ports {
		port = strings.TrimSpace(port)
		if port == "" {
			continue
		}
		current, isMember := memberOf[port]
		switch {
		case isMember && current == in.Name:
			// already a member of the right bridge — no-op
		case isMember:
			conflict := PlanConflict{
				Path: "/interface/bridge/port", Existing: fmt.Sprintf("%s is a member of bridge %s", port, current),
				Key: "/interface/bridge/port:" + port,
				Reason: fmt.Sprintf("interface %s already belongs to a different bridge (%s); an interface can only be a member of one bridge", port, current),
			}
			resolveConflict(&plan, resolutions, nil, &conflict)
		default:
			sentence := []string{"/interface/bridge/port/add", "=bridge=" + in.Name, "=interface=" + port, "=comment=" + hikradComment}
			item := PlanItem{
				Action: "add", Path: "/interface/bridge/port",
				Command:      fmt.Sprintf("/interface bridge port add bridge=%s interface=%s", in.Name, port),
				CurrentState: "not a bridge member", Sentence: sentence,
			}
			resolveConflict(&plan, resolutions, &item, nil)
		}
	}
	return plan, nil
}

// --- DNS (/ip/dns, a router-wide singleton) ----------------------------------

func (a mikrotikAdapter) planDNSSetup(conn ROSConn, in WizardDNSInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	rows, err := conn.Read("/ip/dns/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ip/dns: %w", err)
	}
	var current map[string]string
	if len(rows) > 0 {
		current = rows[0]
	}
	wantServers := strings.Join(in.Servers, ",")
	wantAllow := yesNo(in.AllowRemoteRequests)
	wantCache := strconv.Itoa(in.CacheSize)
	if in.CacheSize == 0 {
		wantCache = "" // unset = leave router default
	}

	curServers, curAllow := current["servers"], current["allow-remote-requests"]
	summary := fmt.Sprintf("servers=%s allow-remote-requests=%s", orDash(curServers), orDash(curAllow))

	sentence := []string{"/ip/dns/set", "=servers=" + wantServers, "=allow-remote-requests=" + wantAllow}
	cmd := fmt.Sprintf("/ip dns set servers=%s allow-remote-requests=%s", wantServers, wantAllow)
	if wantCache != "" {
		sentence = append(sentence, "=cache-size="+wantCache+"KiB")
		cmd += " cache-size=" + wantCache + "KiB"
	}
	item := &PlanItem{Action: "set", Path: "/ip/dns", Command: cmd, CurrentState: summary, Sentence: sentence}

	switch {
	case curServers == wantServers && curAllow == wantAllow:
		return plan, nil // already exactly what the wizard wants
	case curServers == "" || curServers == wantServers:
		// unset, or same servers with only allow-remote-requests differing:
		// safe additive/tightening change, no existing deliberate value lost.
		resolveConflict(&plan, resolutions, item, nil)
	default:
		conflict := &PlanConflict{
			Path: "/ip/dns", Existing: summary, Key: "/ip/dns",
			Reason:         "DNS servers are already configured to something other than the wizard's choice",
			Resolvable:     true,
			UpdateCommand:  cmd,
			updateSentence: sentence,
		}
		resolveConflict(&plan, resolutions, nil, conflict)
	}
	return plan, nil
}

// --- NTP (/system/ntp/client, ROS6/7 syntax differs) -------------------------

func (a mikrotikAdapter) planNTPSetup(conn ROSConn, in WizardNTPInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	rows, err := conn.Read("/system/ntp/client/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /system/ntp/client: %w", err)
	}
	var current map[string]string
	if len(rows) > 0 {
		current = rows[0]
	}
	if current["enabled"] == "yes" {
		return plan, nil // NTP already on; never overwrite an operator's own server choice (FR-56.2 posture)
	}
	if len(in.Servers) == 0 {
		return plan, nil
	}
	// Version-agnostic primary/secondary fields are accepted by both ROS6 and
	// ROS7's compatibility layer for /system/ntp/client; a router whose
	// version genuinely needs the ROS7 /system/ntp/client/servers list
	// instead reports that as a conflict-free no-op here since "enabled=yes"
	// is the only thing PlanWizard checks — no silent wrong-write, and the
	// gap is a documented follow-up, not a hidden guess.
	primary := in.Servers[0]
	secondary := ""
	if len(in.Servers) > 1 {
		secondary = in.Servers[1]
	}
	sentence := []string{"/system/ntp/client/set", "=enabled=yes", "=primary-ntp=" + primary}
	cmd := fmt.Sprintf("/system ntp client set enabled=yes primary-ntp=%s", primary)
	if secondary != "" {
		sentence = append(sentence, "=secondary-ntp="+secondary)
		cmd += " secondary-ntp=" + secondary
	}
	item := &PlanItem{Action: "set", Path: "/system/ntp/client", Command: cmd, CurrentState: "disabled", Sentence: sentence}
	resolveConflict(&plan, resolutions, item, nil)
	return plan, nil
}

// --- firewall wizard step (NAT + expired pool) -------------------------------

func (a mikrotikAdapter) planFirewallWizard(conn ROSConn, in WizardFirewallInput, resolutions map[string]string) (AutoSetupPlan, error) {
	var plan AutoSetupPlan

	natRows, err := conn.Read("/ip/firewall/nat/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ip/firewall/nat: %w", err)
	}
	masqueraded := map[string]bool{}
	for _, r := range natRows {
		if r["chain"] == "srcnat" && r["action"] == "masquerade" {
			masqueraded[r["out-interface"]] = true
		}
	}
	for _, iface := range in.NATOutInterfaces {
		iface = strings.TrimSpace(iface)
		if iface == "" || masqueraded[iface] {
			continue
		}
		sentence := []string{"/ip/firewall/nat/add", "=chain=srcnat", "=action=masquerade", "=out-interface=" + iface, "=comment=" + hikradComment}
		item := PlanItem{
			Action: "add", Path: "/ip/firewall/nat",
			Command:      fmt.Sprintf("/ip firewall nat add chain=srcnat action=masquerade out-interface=%s", iface),
			CurrentState: "not present", Sentence: sentence,
		}
		plan.Items = append(plan.Items, item)
	}

	if in.ExpiredPool != nil && in.ExpiredPool.PoolName != "" {
		poolPlan, err := a.PlanPool(conn, PoolPushInput{Name: in.ExpiredPool.PoolName, Ranges: in.ExpiredPool.Ranges})
		if err != nil {
			return plan, err
		}
		for _, item := range poolPlan.Items {
			resolveConflict(&plan, resolutions, &item, nil)
		}
		for _, c := range poolPlan.Conflicts {
			resolveConflict(&plan, resolutions, nil, &c)
		}
	}
	return plan, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
