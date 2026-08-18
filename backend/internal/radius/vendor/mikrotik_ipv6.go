package vendor

// FR-149 IPv6 dual-stack for the MikroTik adapter: router-side pool
// construction and PPP-profile attachment for DHCPv6-PD. The per-subscriber
// reply attribute itself (IntentDelegatedPrefix) is mapped in mikrotik.go's
// Apply, not here — this file is the one-time router setup a service
// instance needs before any subscriber can receive a delegated prefix at
// all, planned through the same additive/update-or-conflict vocabulary as
// every other object in this package.

import "fmt"

// PlanIPv6 implements Adapter.PlanIPv6.
func (a mikrotikAdapter) PlanIPv6(conn ROSConn, in WizardIPv6Input) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	if in.PoolName == "" || in.Prefix == "" {
		return plan, nil
	}
	prefixLen := in.PrefixLen
	if prefixLen == 0 {
		prefixLen = 56
	}

	rows, err := conn.Read("/ipv6/pool/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ipv6/pool: %w", err)
	}
	var existing map[string]string
	for _, r := range rows {
		if r["name"] == in.PoolName {
			existing = r
			break
		}
	}
	plLen := fmt.Sprintf("%d", prefixLen)
	if existing == nil {
		sentence := []string{"/ipv6/pool/add", "=name=" + in.PoolName, "=prefix=" + in.Prefix, "=prefix-length=" + plLen}
		plan.Items = append(plan.Items, PlanItem{
			Action: "add", Path: "/ipv6/pool",
			Command:      fmt.Sprintf("/ipv6 pool add name=%s prefix=%s prefix-length=%s", in.PoolName, in.Prefix, plLen),
			CurrentState: "not present", Sentence: sentence,
		})
	} else if existing["prefix"] != in.Prefix || existing["prefix-length"] != plLen {
		plan.Conflicts = append(plan.Conflicts, PlanConflict{
			Path: "/ipv6/pool", Existing: fmt.Sprintf("prefix=%s prefix-length=%s", existing["prefix"], existing["prefix-length"]),
			Key:    "/ipv6/pool:" + in.PoolName,
			Reason: "an IPv6 pool with this name already exists with a different prefix",
		})
		return plan, nil
	}

	if in.ROSServerName == "" {
		return plan, nil
	}
	profRows, err := conn.Read("/ppp/profile/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ppp/profile: %w", err)
	}
	for _, r := range profRows {
		if r["name"] != in.ROSServerName {
			continue
		}
		if r["dhcpv6-pd-pool"] == in.PoolName {
			return plan, nil // already attached
		}
		if r["dhcpv6-pd-pool"] != "" {
			plan.Conflicts = append(plan.Conflicts, PlanConflict{
				Path: "/ppp/profile", Existing: "dhcpv6-pd-pool=" + r["dhcpv6-pd-pool"], Key: "/ppp/profile:" + in.ROSServerName,
				Reason:         "this PPP profile already delegates IPv6 from a different pool",
				Resolvable:     true,
				UpdateCommand:  fmt.Sprintf("/ppp profile set [find name=%s] dhcpv6-pd-pool=%s", in.ROSServerName, in.PoolName),
				updateSentence: []string{"/ppp/profile/set", "=.id=" + r[".id"], "=dhcpv6-pd-pool=" + in.PoolName},
			})
			return plan, nil
		}
		plan.Items = append(plan.Items, PlanItem{
			Action: "set", Path: "/ppp/profile",
			Command:      fmt.Sprintf("/ppp profile set [find name=%s] dhcpv6-pd-pool=%s", in.ROSServerName, in.PoolName),
			CurrentState: "dhcpv6-pd-pool=(unset)", Sentence: []string{"/ppp/profile/set", "=.id=" + r[".id"], "=dhcpv6-pd-pool=" + in.PoolName},
		})
		return plan, nil
	}
	return plan, nil
}
