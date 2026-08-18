package radius

// FR-146 Firewall tab HTTP surface. Five endpoints:
//
//	GET  /api/v1/nas/{id}/firewall/rules            -> []vendor.FirewallRule (live read)
//	POST /api/v1/nas/{id}/firewall/plan             {rule_id?, table, fields} -> {items, conflicts, preview_hash}
//	POST /api/v1/nas/{id}/firewall/apply            {..., preview_hash} -> {results, all_ok, revert:{snapshot_id, deadline}}
//	POST /api/v1/nas/{id}/firewall/{snapshotId}/confirm  -> cancels the pending auto-revert
//	GET  /api/v1/nas/{id}/firewall/pending          -> pending (unconfirmed) auto-revert snapshots
//
// The auto-revert safety net (frozen contract, phase brief) is NOT optional
// per-change — every apply snapshots the rules it is about to touch BEFORE
// writing anything, then schedules a revert deadline. The panel is expected
// to show the countdown and call confirm; if it never does, the worker in
// module.go (startFirewallRevertWorker) restores the pre-change values on
// its own. This is why the firewall tab's write path exists in this one
// file rather than reusing autosetup_api.go's generic plan/apply directly —
// the snapshot step must run between plan and write, which no other object
// type in this phase needs.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

// firewallRevertWindow is how long an operator has to confirm a firewall
// change before it is automatically reverted (gate item 11). Fixed rather
// than configurable in this phase — a short, predictable window is the
// point: long enough to notice a lockout (reconnect and click confirm),
// short enough that "I'll deal with it later" cannot leave a bad rule live
// indefinitely.
const firewallRevertWindow = 5 * time.Minute

func (m *module) firewallRulesHandler(w http.ResponseWriter, r *http.Request) {
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

	rules, err := vendor.For(n.Vendor).ReadFirewallRules(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.firewall_read", "nas", n.ID, nil, nil)
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(nonNilRules(rules), ""))
}

func nonNilRules(rules []vendor.FirewallRule) []vendor.FirewallRule {
	if rules == nil {
		return []vendor.FirewallRule{}
	}
	return rules
}

type firewallChangeRequest struct {
	vendor.FirewallChangeInput
	Description string `json:"description"`
}

func (m *module) buildFirewallPlan(ctx context.Context, n nasRow, req vendor.FirewallChangeInput) (vendor.AutoSetupPlan, vendor.ROSConn, func(), error) {
	closeConn := func() {}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		return vendor.AutoSetupPlan{}, nil, closeConn, err
	}
	closeConn = func() { _ = conn.Close() }
	plan, err := vendor.For(n.Vendor).PlanFirewallChange(conn, req)
	if err != nil {
		return plan, conn, closeConn, fmt.Errorf("radius: plan firewall change: %w", err)
	}
	return plan, conn, closeConn, nil
}

func (m *module) firewallPlanHandler(w http.ResponseWriter, r *http.Request) {
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
	var req firewallChangeRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	plan, _, closeConn, err := m.buildFirewallPlan(ctx, n, req.FirewallChangeInput)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	hash := firewallPlanHash(n.ID, req.FirewallChangeInput, plan)
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items: nonNilItems(plan.Items), Conflicts: nonNilConflicts(plan.Conflicts), PreviewHash: hash,
	})
}

type firewallApplyRequest struct {
	firewallChangeRequest
	PreviewHash string `json:"preview_hash" validate:"required"`
}

type firewallRevertInfo struct {
	SnapshotID string    `json:"snapshot_id"`
	Deadline   time.Time `json:"deadline"`
}

type firewallApplyResponse struct {
	Results []vendor.ApplyResult `json:"results"`
	AllOK   bool                 `json:"all_ok"`
	Revert  *firewallRevertInfo  `json:"revert,omitempty"`
}

// firewallApplyHandler is the phase's central safety mechanism in practice:
// snapshot BEFORE write, write, then return a revert handle the operator
// must confirm — never the other way around (a snapshot taken after the
// write would capture the NEW state, defeating the whole point).
func (m *module) firewallApplyHandler(w http.ResponseWriter, r *http.Request) {
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
	var req firewallApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	plan, conn, closeConn, err := m.buildFirewallPlan(ctx, n, req.FirewallChangeInput)
	defer closeConn()
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	hash := firewallPlanHash(n.ID, req.FirewallChangeInput, plan)
	if hash != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's firewall state has changed since this preview was generated; re-run plan and try again")
		return
	}
	if len(plan.Conflicts) > 0 {
		httpapi.JSON(w, http.StatusConflict, map[string]any{
			"error":     map[string]any{"code": "conflict", "message": "firewall change aborted: nothing was written"},
			"conflicts": plan.Conflicts,
		})
		return
	}

	// Snapshot BEFORE writing — the whole point of the ordering.
	snap, err := vendor.For(n.Vendor).SnapshotFirewallRules(conn, plan)
	if err != nil {
		m.internal(w, "snapshot firewall rules", err)
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

	resp := firewallApplyResponse{Results: results, AllOK: allOK}
	// Only an edit to an EXISTING rule (an "update"/set item) has a snapshot
	// worth reverting — a brand-new "add" item has no prior state, and
	// SnapshotFirewallRules already reflects that (see its doc: add items
	// contribute no revert sentence). A change with nothing to revert skips
	// the auto-revert machinery entirely rather than creating an empty,
	// meaningless pending row.
	if allOK && len(snap.RevertSentences) > 0 {
		deadline := time.Now().Add(firewallRevertWindow)
		actor := ""
		if mgr, ok := auth.ManagerFrom(ctx); ok {
			actor = mgr.ID
		}
		id, err := insertFirewallSnapshot(ctx, m.db, n.ID, req.Description, snap, deadline, actor)
		if err != nil {
			m.log.Error("firewall: insert snapshot", "error", err, "nas_id", n.ID)
		} else {
			resp.Revert = &firewallRevertInfo{SnapshotID: id, Deadline: deadline}
		}
	}
	_ = auth.Audit(ctx, "nas.firewall_apply", "nas", n.ID, nil, map[string]any{
		"outcome": map[bool]string{true: "applied", false: "partial"}[allOK],
		"results": results, "revert": resp.Revert,
	})
	httpapi.JSON(w, http.StatusOK, resp)
}

// firewallConfirmHandler implements POST .../firewall/{snapshotId}/confirm —
// the operator explicitly accepting a change within the window, cancelling
// the pending auto-revert (gate item 11's "confirm" branch).
func (m *module) firewallConfirmHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nasID, snapID := chi.URLParam(r, "id"), chi.URLParam(r, "snapshotId")
	if err := confirmFirewallSnapshot(ctx, m.db, nasID, snapID); errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no pending firewall change with that id")
		return
	} else if err != nil {
		m.internal(w, "confirm firewall snapshot", err)
		return
	}
	_ = auth.Audit(ctx, "nas.firewall_confirm", "nas", nasID, nil, map[string]any{"snapshot_id": snapID})
	httpapi.JSON(w, http.StatusOK, map[string]any{"status": "confirmed"})
}

type pendingSnapshotView struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Deadline    time.Time `json:"deadline"`
	CreatedAt   time.Time `json:"created_at"`
}

func (m *module) firewallPendingHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nasID := chi.URLParam(r, "id")
	rows, err := listPendingSnapshotsForNAS(ctx, m.db, nasID)
	if err != nil {
		m.internal(w, "list pending firewall snapshots", err)
		return
	}
	out := make([]pendingSnapshotView, 0, len(rows))
	for _, row := range rows {
		out = append(out, pendingSnapshotView{ID: row.ID, Description: row.ChangeDescription, Deadline: row.RevertDeadline, CreatedAt: row.CreatedAt})
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(out, ""))
}

func firewallPlanHash(nasID string, in vendor.FirewallChangeInput, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })
	conflicts := append([]vendor.PlanConflict(nil), plan.Conflicts...)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path+conflicts[i].Existing < conflicts[j].Path+conflicts[j].Existing })

	fieldKeys := make([]string, 0, len(in.Fields))
	for k := range in.Fields {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)

	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s", nasID, in.RuleID, in.Table)
	for _, k := range fieldKeys {
		fmt.Fprintf(&b, "|f:%s:%s", k, in.Fields[k])
	}
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	for _, c := range conflicts {
		fmt.Fprintf(&b, "|conflict:%s:%s:%s", c.Path, c.Existing, c.Reason)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// --- auto-revert worker ------------------------------------------------------

// startFirewallRevertWorker polls for firewall changes whose confirm window
// expired unconfirmed and restores their pre-change state (gate item 11).
// Ticks frequently relative to the revert window so a lockout is undone
// promptly rather than merely "eventually".
func (m *module) startFirewallRevertWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepDueFirewallReverts(ctx)
		}
	}
}

func (m *module) sweepDueFirewallReverts(ctx context.Context) {
	due, err := duePendingSnapshots(ctx, m.db)
	if err != nil {
		m.log.Error("firewall revert: list due snapshots", "error", err)
		return
	}
	for _, snap := range due {
		m.revertOneFirewallSnapshot(ctx, snap)
	}
}

func (m *module) revertOneFirewallSnapshot(ctx context.Context, snap firewallSnapshotRow) {
	n, err := getNAS(ctx, m.db, snap.NASID)
	if err != nil {
		_ = markSnapshotReverted(ctx, m.db, snap.ID, "nas lookup failed: "+err.Error())
		return
	}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		// The router being unreachable is exactly the scenario auto-revert
		// exists for — a lockout can make the router momentarily unreachable
		// over the API too. Leave the row pending so the next tick retries
		// rather than giving up after one failed attempt.
		m.log.Warn("firewall revert: connect failed, will retry", "nas_id", snap.NASID, "snapshot_id", snap.ID, "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	var failed string
	for _, sentence := range snap.Snapshot.RevertSentences {
		if _, err := conn.Write(sentence...); err != nil {
			failed = err.Error()
			break
		}
	}
	if err := markSnapshotReverted(ctx, m.db, snap.ID, failed); err != nil {
		m.log.Error("firewall revert: mark reverted", "error", err, "snapshot_id", snap.ID)
	}
	_ = auth.Audit(ctx, "nas.firewall_auto_revert", "nas", n.ID, nil, map[string]any{
		"snapshot_id": snap.ID, "ok": failed == "", "error": failed,
	})
}
