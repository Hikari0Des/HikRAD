package radius

// RouterOS API auto-setup HTTP handlers (FR-56.2-56.4, contract C6). Two
// endpoints:
//
//	POST /api/v1/nas/{id}/auto-setup/preview  -> {items, conflicts, preview_hash}
//	POST /api/v1/nas/{id}/auto-setup/apply    {preview_hash} -> {results, seen}
//
// Safety contract (frozen by Decision 17, restated because this is the
// scariest write path in the product): preview only ever issues read (print)
// RouterOS API sentences. apply refuses unless the plan it recomputes
// server-side — never trusting anything the client echoes back — hashes to
// exactly the value the caller supplies, which is how a router that changed
// state between the two calls is caught (a planted conflicting /radius entry,
// or literally anything else) without a stored preview-session table. A
// non-empty Conflicts list aborts the whole apply before a single write
// sentence is sent, so the router is provably untouched.

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

// autoSetupPreviewResponse is the C6 preview shape.
type autoSetupPreviewResponse struct {
	Items       []vendor.PlanItem     `json:"items"`
	Conflicts   []vendor.PlanConflict `json:"conflicts"`
	PreviewHash string                `json:"preview_hash"`
	ROSVersion  string                `json:"ros_version"`
}

func (m *module) autoSetupPreviewHandler(w http.ResponseWriter, r *http.Request) {
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

	plan, ros, _, closeConn, err := m.buildAutoSetupPlan(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer closeConn()

	hash := planHash(n.ID, plan)
	_ = auth.Audit(ctx, "nas.autosetup_preview", "nas", n.ID, nil, map[string]any{
		"items": len(plan.Items), "conflicts": len(plan.Conflicts), "ros_version": ros,
	})
	httpapi.JSON(w, http.StatusOK, autoSetupPreviewResponse{
		Items: nonNilItems(plan.Items), Conflicts: nonNilConflicts(plan.Conflicts),
		PreviewHash: hash, ROSVersion: ros,
	})
}

type autoSetupApplyRequest struct {
	PreviewHash string `json:"preview_hash" validate:"required"`
}

type autoSetupApplyResponse struct {
	Results []vendor.ApplyResult `json:"results"`
	AllOK   bool                 `json:"all_ok"`
	Seen    map[string]any       `json:"seen"`
}

func (m *module) autoSetupApplyHandler(w http.ResponseWriter, r *http.Request) {
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

	var req autoSetupApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}

	ros := ""
	if n.ROSVersion != nil {
		ros = *n.ROSVersion
	}
	if !rosMatrixValidated(ros) {
		httpapi.Error(w, http.StatusConflict, "ros_not_validated",
			"auto-setup apply is not yet validated for this NAS's RouterOS version; use GET /api/v1/nas/{id}/config-snippet instead")
		return
	}

	plan, gotROS, conn, closeConn, err := m.buildAutoSetupPlan(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer closeConn()

	hash := planHash(n.ID, plan)
	if hash != req.PreviewHash {
		httpapi.Error(w, http.StatusConflict, "preview_stale",
			"the router's state has changed since this preview was generated; re-run preview and try again")
		return
	}
	if len(plan.Conflicts) > 0 {
		_ = auth.Audit(ctx, "nas.autosetup_apply", "nas", n.ID, nil, map[string]any{
			"outcome": "aborted_conflict", "conflicts": plan.Conflicts, "ros_version": gotROS,
		})
		httpapi.JSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{"code": "conflict", "message": "auto-setup aborted: the router has existing configuration that would need to be changed; nothing was written"},
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
	_ = auth.Audit(ctx, "nas.autosetup_apply", "nas", n.ID, nil, map[string]any{
		"outcome": map[bool]string{true: "applied", false: "partial"}[allOK],
		"results": results, "ros_version": gotROS,
	})

	// FR-56.3: a successful apply auto-runs the FR-14.4 "seen since created"
	// test and reports the result — same Redis markers nasStatusHandler reads.
	// A brand-new /radius entry only gets exercised by the NAS's own next
	// auth attempt, so this is a status snapshot, not a synthetic probe.
	seen := map[string]any{"last_auth_at": nil, "last_acct_at": nil}
	if m.rdb != nil {
		if v, e := m.rdb.Get(ctx, nasSeenAuthPrefix+canonicalIP(n.IP)).Result(); e == nil {
			seen["last_auth_at"] = v
		}
		if v, e := m.rdb.Get(ctx, "nas:seen:acct:"+canonicalIP(n.IP)).Result(); e == nil {
			seen["last_acct_at"] = v
		}
	}
	seen["seen"] = seen["last_auth_at"] != nil || seen["last_acct_at"] != nil

	httpapi.JSON(w, http.StatusOK, autoSetupApplyResponse{Results: results, AllOK: allOK, Seen: seen})
}

// buildAutoSetupPlan resolves credentials, connects read-only, and computes
// the plan both preview and apply share. The returned conn is left open (so
// apply can reuse it for writes without a second connect+login round trip)
// and closeConn must always be deferred by the caller, even on error paths
// where conn itself is nil (closeConn is a no-op then).
func (m *module) buildAutoSetupPlan(ctx context.Context, n nasRow) (plan vendor.AutoSetupPlan, rosVersion string, conn vendor.ROSConn, closeConn func(), err error) {
	closeConn = func() {}
	if n.APIUser == "" || len(n.APIPasswordEnc) == 0 {
		return plan, "", nil, closeConn, fmt.Errorf("%w: no RouterOS API credentials saved for this NAS", errAutoSetupNoCreds)
	}
	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		return plan, "", nil, closeConn, fmt.Errorf("radius: decrypt nas api password: %w", err)
	}

	in, ros, err := m.snippetInputFor(ctx, n, "")
	if err != nil {
		return plan, ros, nil, closeConn, err
	}

	c, err := m.dialROS(ctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		return plan, ros, nil, closeConn, fmt.Errorf("%w: %s", errAutoSetupConnect, err.Error())
	}
	closeConn = func() { _ = c.Close() }

	plan, err = vendor.For(n.Vendor).PlanAutoSetup(c, in)
	if err != nil {
		return plan, ros, c, closeConn, fmt.Errorf("radius: plan auto-setup: %w", err)
	}
	return plan, ros, c, closeConn, nil
}

var (
	errAutoSetupNoCreds = errors.New("auto-setup: credentials not configured")
	errAutoSetupConnect = errors.New("auto-setup: could not connect to router")
)

func (m *module) autoSetupConnectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAutoSetupNoCreds):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_api_credentials", err.Error())
	case errors.Is(err, errAutoSetupConnect):
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
	default:
		m.internal(w, "auto-setup", err)
	}
}

// planHash is the C6 preview_hash: a deterministic digest of exactly what the
// plan would do (and why not, for conflicts) plus the NAS id, so a stale or
// cross-NAS hash can never be replayed. Recomputed identically at apply time
// over freshly-read router state — any drift changes the hash.
func planHash(nasID string, plan vendor.AutoSetupPlan) string {
	items := append([]vendor.PlanItem(nil), plan.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path+items[i].Command < items[j].Path+items[j].Command })
	conflicts := append([]vendor.PlanConflict(nil), plan.Conflicts...)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path+conflicts[i].Existing < conflicts[j].Path+conflicts[j].Existing })

	var b strings.Builder
	b.WriteString(nasID)
	for _, it := range items {
		fmt.Fprintf(&b, "|item:%s:%s:%s:%s", it.Action, it.Path, it.Command, it.CurrentState)
	}
	for _, c := range conflicts {
		fmt.Fprintf(&b, "|conflict:%s:%s:%s", c.Path, c.Existing, c.Reason)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func nonNilItems(items []vendor.PlanItem) []vendor.PlanItem {
	if items == nil {
		return []vendor.PlanItem{}
	}
	return items
}

func nonNilConflicts(conflicts []vendor.PlanConflict) []vendor.PlanConflict {
	if conflicts == nil {
		return []vendor.PlanConflict{}
	}
	return conflicts
}
