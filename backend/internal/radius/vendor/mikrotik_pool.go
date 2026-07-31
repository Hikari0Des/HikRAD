package vendor

// RouterOS /ip pool read + push planning (FR-108, v3-5). This is the vendor
// boundary for pool synchronization: the radius core asks for a plan in
// abstract terms (a name and a set of ranges) and never names a RouterOS path
// or property itself (FR-17).
//
// Ownership rule (Decision 48): only a pool HikRAD OWNS is ever pushed. This
// file provides the mechanism; the refusal for router-adopted pools lives in
// the API layer, which is where the ownership column is read.

import (
	"fmt"
	"strings"
)

// PoolPushInput is a HikRAD-owned pool to reconcile onto a router.
type PoolPushInput struct {
	Name string
	// Ranges are HikRAD's stored CIDRs/IPs. RouterOS accepts a comma-separated
	// list of CIDRs or start-end ranges in its `ranges` property.
	Ranges []string
}

// RouterPool is a pool as it exists on the router — a read-only observation.
// Ranges is the router's raw string (often "a.b.c.d-e.f.g.h"), kept verbatim
// rather than parsed: HikRAD does not model the ranges of a pool it doesn't own.
type RouterPool struct {
	Name   string `json:"name"`
	Ranges string `json:"ranges"`
}

// poolRangesString renders HikRAD's stored ranges into RouterOS's `ranges`
// property form.
func poolRangesString(ranges []string) string {
	return strings.Join(ranges, ",")
}

// ReadPools lists the router's IP pools (read-only, FR-65 posture). Used by the
// adopt flow and to surface "named in HikRAD but missing on the router"
// mismatches — the exact condition behind the "no address from ip pool" login
// failure documented in docs/ops/known-issues.md.
func (mikrotikAdapter) ReadPools(conn ROSConn) ([]RouterPool, error) {
	rows, err := conn.Read("/ip/pool/print")
	if err != nil {
		return nil, fmt.Errorf("mikrotik: read /ip/pool: %w", err)
	}
	out := make([]RouterPool, 0, len(rows))
	for _, row := range rows {
		name := row["name"]
		if name == "" {
			continue
		}
		out = append(out, RouterPool{Name: name, Ranges: row["ranges"]})
	}
	return out, nil
}

// PPPoEPoolNames reads the router's PPP profiles and returns the set of pool
// names they reference (remote-address / local-address). RouterOS hands a PPPoE
// client its address out of the pool named on its profile, so this is exactly
// the set of pools that belong to PPPoE service. remote-address may instead be
// a bare IP; that is harmless here because the adopt flow intersects this set
// with the actual /ip pool names, and an IP matches no pool. Read-only.
func (mikrotikAdapter) PPPoEPoolNames(conn ROSConn) (map[string]bool, error) {
	rows, err := conn.Read("/ppp/profile/print")
	if err != nil {
		return nil, fmt.Errorf("mikrotik: read /ppp/profile: %w", err)
	}
	names := make(map[string]bool)
	for _, row := range rows {
		for _, k := range []string{"remote-address", "local-address"} {
			if v := strings.TrimSpace(row[k]); v != "" {
				names[v] = true
			}
		}
	}
	return names, nil
}

// PlanPool produces the additive plan that makes the router's pool match a
// HikRAD-owned pool. Same read-modify-write shape as planHotspotServer:
//   - absent on the router      → one /ip/pool/add
//   - present with other ranges → one /ip/pool/set (by .id)
//   - present and already equal → no items at all (a clean no-op plan)
//
// Nothing here writes: the caller applies the plan through ApplyAutoSetup only
// after the operator confirms and the plan hash still matches (Decision 17).
func (a mikrotikAdapter) PlanPool(conn ROSConn, in PoolPushInput) (AutoSetupPlan, error) {
	var plan AutoSetupPlan
	if in.Name == "" {
		return plan, fmt.Errorf("mikrotik: pool name is required")
	}
	if len(in.Ranges) == 0 {
		return plan, fmt.Errorf("mikrotik: pool %q has no ranges to push", in.Name)
	}
	want := poolRangesString(in.Ranges)

	rows, err := conn.Read("/ip/pool/print")
	if err != nil {
		return plan, fmt.Errorf("mikrotik: read /ip/pool: %w", err)
	}
	var existing map[string]string
	for _, row := range rows {
		if row["name"] == in.Name {
			existing = row
			break
		}
	}

	if existing == nil {
		plan.Items = append(plan.Items, PlanItem{
			Action:   "add",
			Path:     "/ip/pool",
			Command:  fmt.Sprintf("/ip pool add name=%s ranges=%s", in.Name, want),
			Sentence: []string{"/ip/pool/add", "=name=" + in.Name, "=ranges=" + want},
		})
		return plan, nil
	}
	if existing["ranges"] == want {
		return plan, nil // already in sync — nothing to do
	}
	plan.Items = append(plan.Items, PlanItem{
		Action:       "set",
		Path:         "/ip/pool",
		Command:      fmt.Sprintf("/ip pool set [find name=%s] ranges=%s", in.Name, want),
		CurrentState: fmt.Sprintf("ranges=%s", existing["ranges"]),
		Sentence:     []string{"/ip/pool/set", "=.id=" + existing[".id"], "=ranges=" + want},
	})
	return plan, nil
}
