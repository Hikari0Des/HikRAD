package radius

// FR-145 Internet tab HTTP surface. Four endpoints:
//
//	GET  /api/v1/nas/{id}/wan              -> live router uplinks merged with HikRAD's management_mode
//	POST /api/v1/nas/{id}/wan/plan         {interface, mode, ...} -> {items, conflicts, preview_hash}
//	POST /api/v1/nas/{id}/wan/apply        {..., preview_hash} -> {results, all_ok}
//	POST /api/v1/nas/{id}/wan/adopt        {confirm:true} -> imports router-discovered uplinks read-only
//
// "Ours editable, pre-existing shown read-only" (the phase brief): the list
// endpoint is what makes that distinction visible — every uplink the router
// reports is annotated with HikRAD's own management_mode (defaulting to
// "router"/read-only for anything not created through plan/apply or
// explicitly adopted), never inferred from router state alone.

import (
	"context"
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

type wanUplinkView struct {
	Interface      string `json:"interface"`
	Mode           string `json:"mode"`
	Address        string `json:"address"`
	Gateway        string `json:"gateway"`
	Status         string `json:"status,omitempty"`
	Comment        string `json:"comment,omitempty"`
	ManagementMode string `json:"management_mode"`
}

func (m *module) wanListHandler(w http.ResponseWriter, r *http.Request) {
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
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	live, err := vendor.For(n.Vendor).ReadWANUplinks(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	known, err := listWANUplinks(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "list wan uplinks", err)
		return
	}
	modeByIface := map[string]string{}
	for _, u := range known {
		modeByIface[u.Interface] = u.ManagementMode
	}
	out := make([]wanUplinkView, 0, len(live))
	for _, u := range live {
		mode := modeByIface[u.Interface]
		if mode == "" {
			mode = "router" // never seen by HikRAD before: read-only until explicitly adopted (Decision 48)
		}
		out = append(out, wanUplinkView{
			Interface: u.Interface, Mode: u.Mode, Address: u.Address, Gateway: u.Gateway,
			Status: u.Status, Comment: u.Comment, ManagementMode: mode,
		})
	}
	_ = auth.Audit(ctx, "nas.wan_read", "nas", n.ID, nil, nil)
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(out, ""))
}

func (m *module) buildWANPlan(ctx context.Context, n nasRow, in vendor.WizardWANInput) (vendor.AutoSetupPlan, vendor.ROSConn, func(), error) {
	closeConn := func() {}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		return vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	closeConn = func() { _ = conn.Close() }
	plan, err := vendor.For(n.Vendor).PlanWANUplink(conn, in)
	if err != nil {
		return plan, conn, closeConn, fmt.Errorf("radius: plan wan uplink: %w", err)
	}
	return plan, conn, closeConn, nil
}

func (m *module) wanPlanHandler(w http.ResponseWriter, r *http.Request) {
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
	var req vendor.WizardWANInput
	if !httpapi.Bind(w, r, &req) {
		return
	}
	plan, _, closeConn, err := m.buildWANPlan(ctx, n, req)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	hash := wanPlanHash(n.ID, req, plan)
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items: nonNilItems(plan.Items), Conflicts: nonNilConflicts(plan.Conflicts), PreviewHash: hash,
	})
}

type wanApplyRequest struct {
	vendor.WizardWANInput
	PreviewHash string `json:"preview_hash" validate:"required"`
}

func (m *module) wanApplyHandler(w http.ResponseWriter, r *http.Request) {
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
	var req wanApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	plan, conn, closeConn, err := m.buildWANPlan(ctx, n, req.WizardWANInput)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	hash := wanPlanHash(n.ID, req.WizardWANInput, plan)
	if hash != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's state has changed since this preview was generated; re-run plan and try again")
		return
	}
	if len(plan.Conflicts) > 0 {
		httpapi.JSON(w, http.StatusConflict, map[string]any{
			"error":     map[string]any{"code": "conflict", "message": "wan uplink change aborted: nothing was written"},
			"conflicts": plan.Conflicts,
		})
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
	if allOK {
		if err := upsertSystemWANUplink(ctx, m.db, n.ID, req.Interface, req.Label, req.Mode, req.Address, req.Gateway); err != nil {
			m.log.Error("wan: persist uplink", "error", err, "nas_id", n.ID, "interface", req.Interface)
		}
	}
	_ = auth.Audit(ctx, "nas.wan_apply", "nas", n.ID, nil, map[string]any{
		"outcome": map[bool]string{true: "applied", false: "partial"}[allOK], "interface": req.Interface, "results": results,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"results": results, "all_ok": allOK})
}

type wanAdoptRequest struct {
	Confirm bool `json:"confirm"`
}

// wanAdoptHandler imports every router-reported uplink not already known to
// HikRAD as a READ-ONLY reference (mirrors poolAdoptHandler). Writes nothing
// to the router.
func (m *module) wanAdoptHandler(w http.ResponseWriter, r *http.Request) {
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
	var req wanAdoptRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !req.Confirm {
		httpapi.Error(w, http.StatusUnprocessableEntity, "confirm_required",
			"adopting imports the router's WAN uplinks as read-only references; set confirm=true")
		return
	}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	live, err := vendor.For(n.Vendor).ReadWANUplinks(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	known, err := listWANUplinks(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "list wan uplinks", err)
		return
	}
	system := map[string]bool{}
	for _, u := range known {
		if u.ManagementMode == "system" {
			system[u.Interface] = true
		}
	}
	adopted := 0
	for _, u := range live {
		if system[u.Interface] {
			continue // never convert an owned uplink into a read-only one
		}
		if err := adoptWANUplink(ctx, m.db, n.ID, u.Interface, u.Mode, u.Address, u.Gateway); err != nil {
			m.internal(w, "adopt wan uplink", err)
			return
		}
		adopted++
	}
	_ = auth.Audit(ctx, "nas.wan_adopt", "nas", n.ID, nil, map[string]any{"adopted": adopted, "total": len(live)})
	httpapi.JSON(w, http.StatusOK, map[string]any{"adopted": adopted, "total": len(live)})
}

func wanPlanHash(nasID string, in vendor.WizardWANInput, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })

	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s|%s", nasID, in.Interface, in.Mode, in.Address, in.Gateway)
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
