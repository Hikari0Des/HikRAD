package radius

// Hotspot/PPPoE server management HTTP handlers (v2 phase 3, FR-67, contract
// C9). Four endpoints:
//
//	GET  /api/v1/nas/{id}/services/{serviceId}/router-config
//	POST /api/v1/nas/{id}/services/plan    {service_id?, ...} -> {items, conflicts, preview_hash}
//	POST /api/v1/nas/{id}/services/apply   {service_id?, preview_hash, ...} -> {results, all_ok, service}
//	POST /api/v1/nas/{id}/services/{serviceId}/adopt   {confirm:true}
//
// plan/apply share PlanService/ApplyService's hash-gated preview/apply
// pattern with the FR-56/FR-66 auto-setup pair (same tamper-safety
// reasoning); adopt writes nothing to the router at all — it only flips
// nas_services.management_mode, and only on an explicit confirm (FR-67.5).

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

// serviceRouterConfigHandler implements GET .../services/{serviceId}/router-config
// (FR-67.2/67.5). Works for BOTH management modes — a router-mode row is
// always inspectable, only writes are gated on mode. Reuses DiscoverServices
// (FR-62.6) rather than a new read path: it already returns exactly the
// router-truth fields (interface, pool, profile-adjacent name, enabled) this
// endpoint needs, filtered here to the one instance addressed.
func (m *module) serviceRouterConfigHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nasID, serviceID := chi.URLParam(r, "id"), chi.URLParam(r, "serviceId")
	n, err := getNAS(ctx, m.db, nasID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	svc, err := getService(ctx, m.db, nasID, serviceID)
	if errors.Is(err, errServiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "service not found")
		return
	}
	if err != nil {
		m.internal(w, "get service", err)
		return
	}

	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer conn.Close()

	found, err := vendor.For(n.Vendor).DiscoverServices(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"connected but could not read the router's services: "+err.Error())
		return
	}
	for _, f := range found {
		if f.Service == svc.Service && strings.EqualFold(f.ROSServerName, svc.ROSServerName) {
			httpapi.JSON(w, http.StatusOK, discoveredServiceView{
				Service: f.Service, ROSServerName: f.ROSServerName, Label: f.Label,
				InterfaceNote: f.Interface, RouterPoolName: f.PoolName, Enabled: !f.Disabled,
				MatchedServiceID: svc.ID,
			})
			return
		}
	}
	httpapi.Error(w, http.StatusNotFound, "not_found_on_router",
		"this service is not currently visible on the router (renamed or removed on the router side)")
}

// serviceProvisionRequest is FR-67.3/67.4's create-or-edit body (contract C9).
// ServiceID empty = create; non-empty = edit an existing SYSTEM-managed row.
type serviceProvisionRequest struct {
	ServiceID     string               `json:"service_id"`
	Service       string               `json:"service" validate:"required,oneof=pppoe hotspot"`
	Label         string               `json:"label"`
	Interface     string               `json:"interface"`
	ROSServerName string               `json:"ros_server_name" validate:"required"`
	IPPoolID      *string              `json:"ip_pool_id"`
	LocalAddress  string               `json:"local_address"`
	AddressRange  string               `json:"address_range"`
	Values        autoSetupValuesInput `json:"values"`
}

// buildServicePlan resolves credentials, connects, and computes the
// ServiceProvisionInput + AutoSetupPlan shared by plan/apply — the plan/apply
// pair for one server instance, mirroring buildAutoSetupPlan's role for the
// whole-NAS pair.
func (m *module) buildServicePlan(ctx context.Context, n nasRow, req serviceProvisionRequest) (vendor.ServiceProvisionInput, vendor.AutoSetupPlan, vendor.ROSConn, func(), error) {
	closeConn := func() {}
	if req.ServiceID != "" {
		svc, err := getService(ctx, m.db, n.ID, req.ServiceID)
		if err != nil {
			return vendor.ServiceProvisionInput{}, vendor.AutoSetupPlan{}, nil, closeConn, err
		}
		if svc.ManagementMode != "system" {
			return vendor.ServiceProvisionInput{}, vendor.AutoSetupPlan{}, nil, closeConn, errServiceNotAdopted
		}
	}

	poolName := ""
	if req.IPPoolID != nil && *req.IPPoolID != "" {
		if err := m.db.QueryRow(ctx, `SELECT name FROM ip_pools WHERE id = $1::uuid`, *req.IPPoolID).Scan(&poolName); err != nil {
			return vendor.ServiceProvisionInput{}, vendor.AutoSetupPlan{}, nil, closeConn, fmt.Errorf("radius: resolve ip pool: %w", err)
		}
	}

	baseValues, _, err := m.snippetInputFor(ctx, n, "")
	if err != nil {
		return vendor.ServiceProvisionInput{}, vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	baseValues = req.Values.apply(baseValues)

	in := vendor.ServiceProvisionInput{
		Kind: req.Service, ROSServerName: req.ROSServerName, Label: req.Label,
		Interface: req.Interface, LocalAddress: req.LocalAddress, AddressRange: req.AddressRange,
		PoolName: poolName, Values: baseValues, Editing: req.ServiceID != "",
	}

	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		return in, vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	closeConn = func() { _ = conn.Close() }

	plan, err := vendor.For(n.Vendor).PlanService(conn, in)
	if err != nil {
		return in, plan, conn, closeConn, fmt.Errorf("radius: plan service: %w", err)
	}
	return in, plan, conn, closeConn, nil
}

var errServiceNotAdopted = errors.New("radius: service is router-managed; adopt it before editing")

func (m *module) servicePlanHandler(w http.ResponseWriter, r *http.Request) {
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
	var req serviceProvisionRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}

	in, plan, _, closeConn, err := m.buildServicePlan(ctx, n, req)
	defer closeConn()
	if !m.writeServicePlanError(w, err) {
		return
	}

	hash := serviceHash(n.ID, req.ServiceID, in, plan)
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items: nonNilItems(plan.Items), Conflicts: nonNilConflicts(plan.Conflicts), PreviewHash: hash,
	})
}

type serviceApplyRequest struct {
	serviceProvisionRequest
	PreviewHash string `json:"preview_hash" validate:"required"`
}

type serviceApplyResponse struct {
	Results []vendor.ApplyResult `json:"results"`
	AllOK   bool                 `json:"all_ok"`
	Service serviceView          `json:"service"`
}

func (m *module) serviceApplyHandler(w http.ResponseWriter, r *http.Request) {
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
	if !rosMatrixValidated(strOrDeref(n.ROSVersion)) {
		httpapi.Error(w, http.StatusConflict, "ros_not_validated",
			"server provisioning is not yet validated for this NAS's RouterOS version")
		return
	}
	var req serviceApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}

	in, plan, conn, closeConn, err := m.buildServicePlan(ctx, n, req.serviceProvisionRequest)
	defer closeConn()
	if !m.writeServicePlanError(w, err) {
		return
	}

	hash := serviceHash(n.ID, req.ServiceID, in, plan)
	if hash != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's state (or the values submitted) has changed since this preview was generated; re-run plan and try again")
		return
	}
	if len(plan.Conflicts) > 0 {
		_ = auth.Audit(ctx, "nas.service_apply", "nas", n.ID, nil, map[string]any{
			"outcome": "aborted_conflict", "conflicts": plan.Conflicts,
		})
		httpapi.JSON(w, http.StatusConflict, map[string]any{
			"error":     map[string]any{"code": "conflict", "message": "server provisioning aborted: nothing was written"},
			"conflicts": plan.Conflicts,
		})
		return
	}

	results := vendor.For(n.Vendor).ApplyService(conn, plan)
	allOK := true
	for _, res := range results {
		if !res.OK {
			allOK = false
			break
		}
	}
	if !allOK {
		_ = auth.Audit(ctx, "nas.service_apply", "nas", n.ID, nil, map[string]any{"outcome": "partial", "results": results})
		httpapi.JSON(w, http.StatusOK, serviceApplyResponse{Results: results, AllOK: false})
		return
	}

	poolID := req.IPPoolID
	id, err := upsertSystemService(ctx, m.db, n.ID, req.ServiceID, systemServiceInput{
		Service: req.Service, Label: req.Label, InterfaceNote: req.Interface,
		IPPoolID: poolID, ROSServerName: req.ROSServerName,
	})
	if err != nil {
		m.internal(w, "persist service", err)
		return
	}
	m.afterNASChange(ctx)
	row, err := getService(ctx, m.db, n.ID, id)
	if err != nil {
		m.internal(w, "read back service", err)
		return
	}
	_ = auth.Audit(ctx, "nas.service_apply", "nas", n.ID, nil, map[string]any{
		"outcome": "applied", "results": results, "service_id": id, "editing": req.ServiceID != "",
	})
	httpapi.JSON(w, http.StatusOK, serviceApplyResponse{Results: results, AllOK: true, Service: row.view()})
}

func (m *module) writeServicePlanError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, errServiceNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "service not found")
	case errors.Is(err, errServiceNotAdopted):
		httpapi.Error(w, http.StatusConflict, "not_adopted", "this service is router-managed; adopt it before editing (POST .../services/{serviceId}/adopt)")
	default:
		m.autoSetupConnectError(w, err)
	}
	return false
}

// serviceHash mirrors planHash's tamper-safety reasoning (C6/C9): apply is
// tied to the exact ServiceProvisionInput fields AND the freshly-recomputed
// plan, so drift in either invalidates the hash.
func serviceHash(nasID, serviceID string, in vendor.ServiceProvisionInput, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })
	conflicts := append([]vendor.PlanConflict(nil), plan.Conflicts...)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path+conflicts[i].Existing < conflicts[j].Path+conflicts[j].Existing })

	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s:%s:%s:%s:%s:%s:%s:%v", nasID, serviceID, in.Kind, in.ROSServerName,
		in.Label, in.Interface, in.LocalAddress, in.AddressRange, in.PoolName, in.Editing)
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	for _, c := range conflicts {
		fmt.Fprintf(&b, "|conflict:%s:%s:%s", c.Path, c.Existing, c.Reason)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// adoptServiceHandler implements POST .../services/{serviceId}/adopt
// (FR-67.5). Writes NOTHING to the router — only nas_services.management_mode
// — and only on an explicit confirm, never as a side effect of viewing config.
func (m *module) adoptServiceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nasID, serviceID := chi.URLParam(r, "id"), chi.URLParam(r, "serviceId")
	if _, err := getNAS(ctx, m.db, nasID); errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	} else if err != nil {
		m.internal(w, "get nas", err)
		return
	}

	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !req.Confirm {
		httpapi.Error(w, http.StatusUnprocessableEntity, "confirm_required",
			"adopting a router-managed service requires an explicit confirm")
		return
	}

	svc, err := getService(ctx, m.db, nasID, serviceID)
	if errors.Is(err, errServiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "service not found")
		return
	}
	if err != nil {
		m.internal(w, "get service", err)
		return
	}
	if svc.ManagementMode == "system" {
		httpapi.Error(w, http.StatusConflict, "already_system", "this service is already system-managed")
		return
	}

	if err := setServiceManagementMode(ctx, m.db, nasID, serviceID, "system"); err != nil {
		m.internal(w, "adopt service", err)
		return
	}
	m.afterNASChange(ctx)
	row, err := getService(ctx, m.db, nasID, serviceID)
	if err != nil {
		m.internal(w, "read back service", err)
		return
	}
	_ = auth.Audit(ctx, "nas.service_adopt", "nas", nasID,
		map[string]any{"management_mode": "router"}, map[string]any{"management_mode": "system"})
	httpapi.JSON(w, http.StatusOK, row.view())
}

func strOrDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
