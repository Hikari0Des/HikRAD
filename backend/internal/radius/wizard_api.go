package radius

// FR-144 setup wizard HTTP surface. Five endpoints:
//
//	GET  /api/v1/nas/{id}/wizard/state    -> {step, form_data, completed}
//	PUT  /api/v1/nas/{id}/wizard/state    {step, form_data} -> saved state
//	POST /api/v1/nas/{id}/wizard/detect   -> vendor.WizardDetection (read-only)
//	POST /api/v1/nas/{id}/wizard/plan     {...} -> {items, conflicts, preview_hash}
//	POST /api/v1/nas/{id}/wizard/apply    {..., preview_hash} -> {results, all_ok}
//
// plan/apply share the exact hash-gated preview/apply contract as every other
// write in this codebase (Decision 17): plan only ever issues read (print)
// RouterOS sentences via vendor.PlanWizard, which internally calls
// PlanAutoSetup/PlanService for the RADIUS/services steps rather than
// reimplementing them — one desired state, described once. apply refuses
// unless the plan recomputed server-side (never trusting anything the client
// echoes back) hashes to the value the caller supplies, and refuses outright
// on any unresolved conflict, so the review step's literal command list
// (gate item 5) is provably what apply actually sends.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// --- state persistence (gate items 3/4) -------------------------------------

type wizardStateResponse struct {
	Step      int             `json:"step"`
	FormData  json.RawMessage `json:"form_data"`
	Completed bool            `json:"completed"`
}

func (m *module) wizardStateGetHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := getNAS(ctx, m.db, id); errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	} else if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	s, err := getWizardState(ctx, m.db, id)
	if err != nil {
		m.internal(w, "get wizard state", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, wizardStateResponse{Step: s.Step, FormData: s.FormData, Completed: s.Completed})
}

type wizardStatePutRequest struct {
	Step     int             `json:"step" validate:"required,min=1,max=10"`
	FormData json.RawMessage `json:"form_data"`
}

func (m *module) wizardStatePutHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := getNAS(ctx, m.db, id); errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	} else if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	var req wizardStatePutRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if len(req.FormData) == 0 {
		req.FormData = json.RawMessage(`{}`)
	}
	if err := saveWizardState(ctx, m.db, id, req.Step, req.FormData, false); err != nil {
		m.internal(w, "save wizard state", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, wizardStateResponse{Step: req.Step, FormData: req.FormData})
}

// --- detect (gate item 2) ----------------------------------------------------

func (m *module) wizardDetectHandler(w http.ResponseWriter, r *http.Request) {
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

	detection, err := vendor.For(n.Vendor).DetectWizard(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.wizard_detect", "nas", n.ID, nil, nil)
	httpapi.JSON(w, http.StatusOK, detection)
}

// --- plan / apply (the central contract) -------------------------------------

type wizardServiceInput struct {
	ServiceID     string  `json:"service_id"`
	Service       string  `json:"service" validate:"omitempty,oneof=pppoe hotspot"`
	Label         string  `json:"label"`
	Interface     string  `json:"interface"`
	ROSServerName string  `json:"ros_server_name"`
	IPPoolID      *string `json:"ip_pool_id"`
	LocalAddress  string  `json:"local_address"`
	AddressRange  string  `json:"address_range"`
}

type wizardPlanRequest struct {
	Bridge      *vendor.WizardBridgeInput   `json:"bridge"`
	DNS         *vendor.WizardDNSInput      `json:"dns"`
	NTP         *vendor.WizardNTPInput      `json:"ntp"`
	WAN         []vendor.WizardWANInput     `json:"wan"`
	Values      autoSetupValuesInput        `json:"values"`
	Services    []wizardServiceInput        `json:"services"`
	Firewall    *vendor.WizardFirewallInput `json:"firewall"`
	IPv6        []vendor.WizardIPv6Input    `json:"ipv6"`
	Resolutions map[string]string           `json:"resolutions"`
}

// buildWizardPlan resolves credentials, connects, and computes the combined
// WizardInput + AutoSetupPlan shared by plan/apply.
func (m *module) buildWizardPlan(ctx context.Context, n nasRow, req wizardPlanRequest) (vendor.WizardInput, vendor.AutoSetupPlan, vendor.ROSConn, func(), error) {
	closeConn := func() {}
	base, _, err := m.snippetInputFor(ctx, n, "")
	if err != nil {
		return vendor.WizardInput{}, vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	base = req.Values.apply(base)
	// The expired-pool step's redirect host joins the shared walled-garden
	// list (the existing FR-9/expiry-behavior reply mechanism already
	// handles the actual redirect at login time via the redirect_expired
	// intent; the wizard's only job here is making sure the redirect page's
	// own host is reachable pre-login, same as any other walled-garden host).
	if req.Firewall != nil && req.Firewall.ExpiredPool != nil && req.Firewall.ExpiredPool.RedirectURL != "" {
		base.WalledGarden = append(base.WalledGarden, req.Firewall.ExpiredPool.RedirectURL)
	}

	services := make([]vendor.ServiceProvisionInput, 0, len(req.Services))
	for _, s := range req.Services {
		poolName := ""
		if s.IPPoolID != nil && *s.IPPoolID != "" {
			if err := m.db.QueryRow(ctx, `SELECT name FROM ip_pools WHERE id = $1::uuid`, *s.IPPoolID).Scan(&poolName); err != nil {
				return vendor.WizardInput{}, vendor.AutoSetupPlan{}, nil, closeConn, fmt.Errorf("radius: resolve ip pool: %w", err)
			}
		}
		services = append(services, vendor.ServiceProvisionInput{
			Kind: s.Service, ROSServerName: s.ROSServerName, Label: s.Label, Interface: s.Interface,
			LocalAddress: s.LocalAddress, AddressRange: s.AddressRange, PoolName: poolName,
			Values: base, Editing: s.ServiceID != "",
		})
	}

	in := vendor.WizardInput{
		Bridge: req.Bridge, DNS: req.DNS, NTP: req.NTP, WAN: req.WAN,
		RadiusValues: base, Services: services, Firewall: req.Firewall, IPv6: req.IPv6,
	}

	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		return in, vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	closeConn = func() { _ = conn.Close() }

	plan, err := vendor.For(n.Vendor).PlanWizard(conn, in, req.Resolutions)
	if err != nil {
		return in, plan, conn, closeConn, fmt.Errorf("radius: plan wizard: %w", err)
	}
	return in, plan, conn, closeConn, nil
}

func (m *module) wizardPlanHandler(w http.ResponseWriter, r *http.Request) {
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
	var req wizardPlanRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &req) {
		return
	}

	_, plan, _, closeConn, err := m.buildWizardPlan(ctx, n, req)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}

	hash := wizardPlanHash(n.ID, req.Resolutions, plan)
	_ = auth.Audit(ctx, "nas.wizard_preview", "nas", n.ID, nil, map[string]any{
		"items": len(plan.Items), "conflicts": len(plan.Conflicts),
	})
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items: nonNilItems(plan.Items), Conflicts: nonNilConflicts(plan.Conflicts), PreviewHash: hash,
	})
}

type wizardApplyRequest struct {
	wizardPlanRequest
	PreviewHash string `json:"preview_hash" validate:"required"`
}

type wizardApplyResponse struct {
	Results []vendor.ApplyResult `json:"results"`
	AllOK   bool                 `json:"all_ok"`
}

func (m *module) wizardApplyHandler(w http.ResponseWriter, r *http.Request) {
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
			"the setup wizard is not yet validated for this NAS's RouterOS version")
		return
	}
	var req wizardApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}

	in, plan, conn, closeConn, err := m.buildWizardPlan(ctx, n, req.wizardPlanRequest)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}

	hash := wizardPlanHash(n.ID, req.Resolutions, plan)
	if hash != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's state (or the wizard's values) has changed since this preview was generated; re-run the review step")
		return
	}
	if len(plan.Conflicts) > 0 {
		_ = auth.Audit(ctx, "nas.wizard_apply", "nas", n.ID, nil, map[string]any{
			"outcome": "aborted_conflict", "conflicts": plan.Conflicts,
		})
		httpapi.JSON(w, http.StatusConflict, map[string]any{
			"error":     map[string]any{"code": "conflict", "message": "wizard apply aborted: nothing was written"},
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
	_ = auth.Audit(ctx, "nas.wizard_apply", "nas", n.ID, nil, map[string]any{
		"outcome": map[bool]string{true: "applied", false: "partial"}[allOK], "results": results,
	})

	if allOK {
		m.persistWizardApply(ctx, n, req.wizardPlanRequest, in)
		if err := saveWizardState(ctx, m.db, n.ID, 10, json.RawMessage(`{}`), true); err != nil {
			m.log.Error("wizard: mark completed", "error", err, "nas_id", n.ID)
		}
		m.afterNASChange(ctx)
	}
	httpapi.JSON(w, http.StatusOK, wizardApplyResponse{Results: results, AllOK: allOK})
}

// persistWizardApply writes HikRAD-side rows (nas_services, ip_pools,
// per-service IPv6 config) for everything a fully-successful wizard apply
// just pushed to the router — mirroring serviceApplyHandler's
// upsertSystemService post-processing, extended to the wizard's other object
// types. Best-effort: the router write already succeeded (that's the
// authoritative outcome); a persistence failure here is logged, not fatal to
// the response, exactly like afterNASChange's cache-invalidation calls
// elsewhere in this package.
func (m *module) persistWizardApply(ctx context.Context, n nasRow, req wizardPlanRequest, in vendor.WizardInput) {
	for _, s := range req.Services {
		if _, err := upsertSystemService(ctx, m.db, n.ID, s.ServiceID, systemServiceInput{
			Service: s.Service, Label: s.Label, InterfaceNote: s.Interface, IPPoolID: s.IPPoolID, ROSServerName: s.ROSServerName,
		}); err != nil {
			m.log.Error("wizard: persist service", "error", err, "nas_id", n.ID, "ros_server_name", s.ROSServerName)
		}
	}
	if req.Firewall != nil && req.Firewall.ExpiredPool != nil && req.Firewall.ExpiredPool.PoolName != "" {
		ep := req.Firewall.ExpiredPool
		if _, err := m.db.Exec(ctx,
			`INSERT INTO ip_pools (name, ranges, purpose, management_mode)
			 VALUES ($1, $2::inet[], 'expired', 'system')
			 ON CONFLICT (name) DO UPDATE SET ranges = EXCLUDED.ranges, updated_at = now()
			  WHERE ip_pools.management_mode = 'system'`,
			ep.PoolName, ep.Ranges); err != nil {
			m.log.Error("wizard: persist expired pool", "error", err, "nas_id", n.ID, "pool", ep.PoolName)
		}
	}
	for _, ipv6 := range req.IPv6 {
		if !ipv6.Enabled || ipv6.ROSServerName == "" {
			continue
		}
		svc, err := findServiceByROSName(ctx, m.db, n.ID, ipv6.ROSServerName)
		if err != nil {
			m.log.Error("wizard: find service for ipv6", "error", err, "nas_id", n.ID, "ros_server_name", ipv6.ROSServerName)
			continue
		}
		if err := setServiceIPv6(ctx, m.db, n.ID, svc.ID, true, ipv6.PrefixLen, ipv6.PoolName); err != nil {
			m.log.Error("wizard: persist ipv6", "error", err, "nas_id", n.ID, "service_id", svc.ID)
		}
	}
}

// wizardPlanHash mirrors planHash/serviceHash/poolPushHash's tamper-safety
// reasoning. Every WizardInput field is deterministically reflected into
// plan.Items'/Conflicts' Command/CurrentState strings by the planXxx
// functions that produced them, so hashing Items/Conflicts (plus resolutions
// and the NAS id, exactly like the other hashers) is sufficient — there is
// no wizard-specific field that could change without changing the plan.
func wizardPlanHash(nasID string, resolutions map[string]string, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })
	conflicts := append([]vendor.PlanConflict(nil), plan.Conflicts...)
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].Path+conflicts[i].Existing < conflicts[j].Path+conflicts[j].Existing
	})

	resKeys := make([]string, 0, len(resolutions))
	for k := range resolutions {
		resKeys = append(resKeys, k)
	}
	sort.Strings(resKeys)

	var b strings.Builder
	b.WriteString(nasID)
	for _, k := range resKeys {
		fmt.Fprintf(&b, "|res:%s:%s", k, resolutions[k])
	}
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	for _, c := range conflicts {
		fmt.Fprintf(&b, "|conflict:%s:%s:%s:%v", c.Path, c.Existing, c.Reason, c.Resolvable)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
