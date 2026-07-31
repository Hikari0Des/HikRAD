package radius

// FR-108 (v3-5): push a HikRAD-OWNED IP pool onto a router's /ip pool, and
// adopt a router's existing pools as read-only references.
//
// The router-ownership sync principle (PRD Decision 48) is enforced here, at
// the one layer that reads the ownership column:
//   - push  refuses anything not management_mode='system' — HikRAD only ever
//           writes objects it owns.
//   - adopt only ever INSERTs 'router' rows and never writes to the router.
//
// Writes follow the same hash-gated preview/apply contract as FR-66/FR-67
// (Decision 17): plan is read-only, apply refuses unless the freshly recomputed
// hash still matches the previewed one, so router drift between the two aborts.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

// poolPushHash binds the preview to the NAS, the pool's identity/ranges, and
// the freshly-recomputed plan — drift in any of them invalidates it.
func poolPushHash(nasID string, p poolRow, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })

	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s", nasID, p.ID, p.Name, strings.Join(p.Ranges, ","))
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// loadPushablePool resolves the NAS + pool and enforces ownership. A
// router-adopted pool is never pushable: HikRAD does not own its ranges.
func (m *module) loadPushablePool(w http.ResponseWriter, r *http.Request) (nasRow, poolRow, bool) {
	ctx := r.Context()
	n, err := getNAS(ctx, m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return nasRow{}, poolRow{}, false
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return nasRow{}, poolRow{}, false
	}
	p, err := getPool(ctx, m.db, chi.URLParam(r, "poolId"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "pool not found")
		return nasRow{}, poolRow{}, false
	}
	if err != nil {
		m.internal(w, "get pool", err)
		return nasRow{}, poolRow{}, false
	}
	if m.refuseIfRouterManaged(w, p, "push") {
		return nasRow{}, poolRow{}, false
	}
	if len(p.Ranges) == 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "pool_no_ranges",
			"this pool has no ranges to push")
		return nasRow{}, poolRow{}, false
	}
	return n, p, true
}

// poolPushPlanHandler previews the /ip pool reconciliation. Read-only.
func (m *module) poolPushPlanHandler(w http.ResponseWriter, r *http.Request) {
	n, p, ok := m.loadPushablePool(w, r)
	if !ok {
		return
	}
	conn, err := m.connectNAS(r.Context(), n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	plan, err := vendor.For(n.Vendor).PlanPool(conn, vendor.PoolPushInput{Name: p.Name, Ranges: p.Ranges})
	if err != nil {
		m.internal(w, "plan pool push", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items:       nonNilItems(plan.Items),
		Conflicts:   nonNilConflicts(plan.Conflicts),
		PreviewHash: poolPushHash(n.ID, p, plan),
	})
}

type poolPushApplyRequest struct {
	PreviewHash string `json:"preview_hash" validate:"required"`
}

type poolPushApplyResponse struct {
	Results []vendor.ApplyResult `json:"results"`
	AllOK   bool                 `json:"all_ok"`
	// NoOp is true when the router already matched — nothing was written.
	NoOp bool `json:"no_op"`
}

// poolPushApplyHandler writes the plan, but only if the router still looks the
// way it did at preview time.
func (m *module) poolPushApplyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, p, ok := m.loadPushablePool(w, r)
	if !ok {
		return
	}
	var req poolPushApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	plan, err := vendor.For(n.Vendor).PlanPool(conn, vendor.PoolPushInput{Name: p.Name, Ranges: p.Ranges})
	if err != nil {
		m.internal(w, "plan pool push", err)
		return
	}
	if poolPushHash(n.ID, p, plan) != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's state (or the pool) has changed since this preview was generated; re-run plan and try again")
		return
	}
	if len(plan.Items) == 0 {
		_ = auth.Audit(ctx, "pools.push", "pool", p.ID, nil, map[string]any{
			"nas_id": n.ID, "pool": p.Name, "outcome": "no_op",
		})
		httpapi.JSON(w, http.StatusOK, poolPushApplyResponse{Results: []vendor.ApplyResult{}, AllOK: true, NoOp: true})
		return
	}

	results := vendor.For(n.Vendor).ApplyAutoSetup(conn, plan)
	allOK := true
	for _, res := range results {
		if !res.OK {
			allOK = false
			break
		}
	}
	_ = auth.Audit(ctx, "pools.push", "pool", p.ID, nil, map[string]any{
		"nas_id": n.ID, "pool": p.Name, "ranges": p.Ranges,
		"outcome": map[bool]string{true: "applied", false: "partial"}[allOK], "results": results,
	})
	httpapi.JSON(w, http.StatusOK, poolPushApplyResponse{Results: results, AllOK: allOK})
}

type poolAdoptRequest struct {
	Confirm bool `json:"confirm"`
}

type adoptedPoolView struct {
	Name         string `json:"name"`
	RouterRanges string `json:"router_ranges"`
	Adopted      bool   `json:"adopted"`
	// Skipped names a pool HikRAD already owns under the same name — adopting
	// it would silently convert an operator-owned pool into a read-only one.
	SkippedReason string `json:"skipped_reason,omitempty"`
}

// poolAdoptHandler imports the router's existing pools as READ-ONLY references.
// It never writes to the router, and never converts a HikRAD-owned ('system')
// pool into a router-managed one.
func (m *module) poolAdoptHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := getNAS(ctx, m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	var req poolAdoptRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !req.Confirm {
		httpapi.Error(w, http.StatusUnprocessableEntity, "confirm_required",
			"adopting imports the router's pools as read-only references; set confirm=true")
		return
	}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	adapter := vendor.For(n.Vendor)
	found, err := adapter.ReadPools(conn)
	if err != nil {
		m.internal(w, "read router pools", err)
		return
	}
	// Adopt only pools the router's PPPoE service actually references (FR-108):
	// pools here are PPPoE-only, so a pool no PPPoE profile points at (a DHCP or
	// hotspot pool) must not be swept in. Intersect the router's /ip pool list
	// with the pool names named by its PPP profiles.
	pppoePools, err := adapter.PPPoEPoolNames(conn)
	if err != nil {
		m.internal(w, "read pppoe pools", err)
		return
	}
	out := make([]adoptedPoolView, 0, len(found))
	for _, rp := range found {
		if !pppoePools[rp.Name] {
			out = append(out, adoptedPoolView{
				Name:          rp.Name,
				RouterRanges:  rp.Ranges,
				Adopted:       false,
				SkippedReason: "not used by any PPPoE server",
			})
			continue
		}
		row, adopted, err := adoptPool(ctx, m.db, rp.Name, rp.Ranges)
		if err != nil {
			m.internal(w, "adopt pool", err)
			return
		}
		v := adoptedPoolView{Name: rp.Name, RouterRanges: rp.Ranges, Adopted: adopted}
		if !adopted {
			v.SkippedReason = "a HikRAD-owned pool already uses this name"
		} else {
			v.RouterRanges = row.RouterRanges
		}
		out = append(out, v)
	}
	_ = auth.Audit(ctx, "pools.adopt", "nas", n.ID, nil, map[string]any{"pools": out})
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}
