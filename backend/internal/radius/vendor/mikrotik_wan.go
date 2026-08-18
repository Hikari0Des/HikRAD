package vendor

// FR-145 Internet tab: WAN uplink read + plan for the MikroTik adapter.
// "Ours editable, pre-existing shown read-only" (the phase brief) is enforced
// by the CALLER's nas_wan_uplinks.management_mode column, not here — this
// file only reads the router's real interface config and plans additive/
// update writes exactly like every other object in this package.

import (
	"fmt"
)

// ReadWANUplinks implements Adapter.ReadWANUplinks.
func (a mikrotikAdapter) ReadWANUplinks(conn ROSConn) ([]WANUplinkInfo, error) {
	var out []WANUplinkInfo

	if rows, err := conn.Read("/ip/address/print"); err == nil {
		for _, r := range rows {
			if r["dynamic"] == "true" {
				continue // DHCP-assigned addresses are reported by the dhcp-client read below instead
			}
			out = append(out, WANUplinkInfo{
				Interface: r["interface"], Mode: "static", Address: r["address"], Comment: r["comment"],
			})
		}
	}
	if rows, err := conn.Read("/ip/dhcp-client/print"); err == nil {
		for _, r := range rows {
			out = append(out, WANUplinkInfo{
				Interface: r["interface"], Mode: "dhcp", Address: r["address"], Gateway: r["gateway"], Status: r["status"],
			})
		}
	}
	if rows, err := conn.Read("/interface/pppoe-client/print"); err == nil {
		for _, r := range rows {
			out = append(out, WANUplinkInfo{
				Interface: r["name"], Mode: "pppoe", Status: statusOf(r), Comment: r["comment"],
			})
		}
	}
	return out, nil
}

func statusOf(r map[string]string) string {
	if r["running"] == "true" {
		return "connected"
	}
	if r["disabled"] == "true" {
		return "disabled"
	}
	return "not connected"
}

// PlanWANUplink implements Adapter.PlanWANUplink.
func (a mikrotikAdapter) PlanWANUplink(conn ROSConn, in WizardWANInput) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	if in.Interface == "" || in.Mode == "" {
		return plan, nil
	}

	switch in.Mode {
	case "static":
		rows, err := conn.Read("/ip/address/print")
		if err != nil {
			return plan, fmt.Errorf("mikrotik: read /ip/address: %w", err)
		}
		for _, r := range rows {
			if r["interface"] == in.Interface {
				if r["address"] == in.Address {
					return plan, nil // already set
				}
				return plan, nil // a static address already exists on this interface with a different value —
				// intentionally NOT surfaced as a conflict here: unlike a singleton router-wide
				// object, a second /ip/address on the same interface is routine (secondary IPs),
				// so guessing "replace it" would be exactly the wrong-write FR-56.2 forbids.
				// The operator adds/edits addresses explicitly through the IP tab instead.
			}
		}
		sentence := []string{"/ip/address/add", "=address=" + in.Address, "=interface=" + in.Interface, "=comment=" + hikradComment}
		if in.Label != "" {
			sentence = append(sentence, "=comment="+hikradComment+": "+in.Label)
		}
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/ip/address",
			Command:      fmt.Sprintf("/ip address add address=%s interface=%s", in.Address, in.Interface),
			CurrentState: "not present", Sentence: sentence,
		})
	case "dhcp":
		rows, err := conn.Read("/ip/dhcp-client/print")
		if err != nil {
			return plan, fmt.Errorf("mikrotik: read /ip/dhcp-client: %w", err)
		}
		for _, r := range rows {
			if r["interface"] == in.Interface {
				return plan, nil // already a DHCP client on this interface
			}
		}
		sentence := []string{"/ip/dhcp-client/add", "=interface=" + in.Interface, "=disabled=no", "=comment=" + hikradComment}
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/ip/dhcp-client",
			Command:      fmt.Sprintf("/ip dhcp-client add interface=%s disabled=no", in.Interface),
			CurrentState: "not present", Sentence: sentence,
		})
	case "pppoe":
		rows, err := conn.Read("/interface/pppoe-client/print")
		if err != nil {
			return plan, fmt.Errorf("mikrotik: read /interface/pppoe-client: %w", err)
		}
		name := "pppoe-" + in.Interface
		for _, r := range rows {
			if r["interface"] == in.Interface {
				return plan, nil // already a PPPoE client on this interface
			}
		}
		sentence := []string{
			"/interface/pppoe-client/add", "=name=" + name, "=interface=" + in.Interface,
			"=user=" + in.PPPoEUser, "=password=" + in.PPPoEPassword, "=disabled=no", "=comment=" + hikradComment,
		}
		cmd := fmt.Sprintf("/interface pppoe-client add name=%s interface=%s user=%s password=**** disabled=no", name, in.Interface, in.PPPoEUser)
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/interface/pppoe-client", Command: cmd, CurrentState: "not present", Sentence: sentence,
		})
	default:
		return plan, fmt.Errorf("mikrotik: unknown WAN mode %q", in.Mode)
	}
	return plan, nil
}
