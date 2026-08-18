package radius

// FR-148 hotspot login page management HTTP surface. Three endpoints, scoped
// to one hotspot service instance:
//
//	GET  /api/v1/nas/{id}/services/{serviceId}/hotspot-page       -> design + push/staleness state
//	PUT  /api/v1/nas/{id}/services/{serviceId}/hotspot-page       {mode, custom_html} -> save (no router write)
//	POST /api/v1/nas/{id}/services/{serviceId}/hotspot-page/push  -> render + push to router flash
//
// Save and push are deliberately separate actions (not one PUT-triggers-push
// flow): the operator may want to draft a design without immediately
// touching a live router, and re-push (identical design, no edit) is itself
// a meaningful action after a RouterOS upgrade wiped flash — that case has
// nothing new to "save".

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

type hotspotPageView struct {
	Mode             string     `json:"mode"`
	DesignVersion    int        `json:"design_version"`
	PushedVersion    *int       `json:"pushed_version"`
	PushedAt         *time.Time `json:"pushed_at"`
	PushedROSVersion string     `json:"pushed_ros_version,omitempty"`
	Stale            bool       `json:"stale"`
	StaleReason      string     `json:"stale_reason,omitempty"`
	CustomHTML       string     `json:"custom_html,omitempty"`
}

// currentROSVersionFor is the best-known current RouterOS version without a
// synchronous router call — the device worker's last sample (v4-6), falling
// back to the NAS record's own stored version. Both may be empty for a
// never-sampled NAS; the caller treats that as "cannot tell yet", not stale.
func (m *module) currentROSVersionFor(ctx context.Context, n nasRow) string {
	if latest, ok, err := latestOneDeviceMetric(ctx, m.db, n.ID); err == nil && ok && latest.ROSVersion != "" {
		return latest.ROSVersion
	}
	if n.ROSVersion != nil {
		return *n.ROSVersion
	}
	return ""
}

func (m *module) hotspotPageGetHandler(w http.ResponseWriter, r *http.Request) {
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
	if _, err := getService(ctx, m.db, nasID, serviceID); errors.Is(err, errServiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "service not found")
		return
	} else if err != nil {
		m.internal(w, "get service", err)
		return
	}
	row, err := getHotspotPage(ctx, m.db, serviceID)
	if err != nil {
		m.internal(w, "get hotspot page", err)
		return
	}
	stale, reason := row.hotspotPageStale(m.currentROSVersionFor(ctx, n))
	view := hotspotPageView{
		Mode: row.Mode, DesignVersion: row.DesignVersion, PushedVersion: row.PushedVersion,
		PushedAt: row.PushedAt, PushedROSVersion: row.PushedROSVersion,
		Stale: stale, StaleReason: reason, CustomHTML: row.CustomHTML,
	}
	httpapi.JSON(w, http.StatusOK, view)
}

type hotspotPageSaveRequest struct {
	Mode       string `json:"mode" validate:"required,oneof=router_default branded_template custom"`
	CustomHTML string `json:"custom_html"`
}

func (m *module) hotspotPageSaveHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nasID, serviceID := chi.URLParam(r, "id"), chi.URLParam(r, "serviceId")
	if _, err := getNAS(ctx, m.db, nasID); errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	} else if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	if _, err := getService(ctx, m.db, nasID, serviceID); errors.Is(err, errServiceNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "service not found")
		return
	} else if err != nil {
		m.internal(w, "get service", err)
		return
	}
	var req hotspotPageSaveRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if req.Mode == "custom" && strings.TrimSpace(req.CustomHTML) == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "html_required", "a custom design needs its HTML content")
		return
	}
	if err := saveHotspotPageDesign(ctx, m.db, serviceID, nasID, req.Mode, req.CustomHTML); err != nil {
		m.internal(w, "save hotspot page", err)
		return
	}
	_ = auth.Audit(ctx, "nas.hotspot_page_save", "nas", nasID, nil, map[string]any{"service_id": serviceID, "mode": req.Mode})
	row, err := getHotspotPage(ctx, m.db, serviceID)
	if err != nil {
		m.internal(w, "read back hotspot page", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, hotspotPageView{Mode: row.Mode, DesignVersion: row.DesignVersion, CustomHTML: row.CustomHTML})
}

// hotspotPagePushHandler implements POST .../hotspot-page/push (FR-148): the
// only endpoint here that touches the router. router_default is refused —
// there is nothing to push for "leave the router's own page alone".
func (m *module) hotspotPagePushHandler(w http.ResponseWriter, r *http.Request) {
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
	} else if err != nil {
		m.internal(w, "get service", err)
		return
	}
	if svc.Service != "hotspot" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "not_hotspot", "login pages only apply to hotspot service instances")
		return
	}
	row, err := getHotspotPage(ctx, m.db, serviceID)
	if err != nil {
		m.internal(w, "get hotspot page", err)
		return
	}
	if row.Mode == "router_default" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "nothing_to_push",
			"this instance is set to the router's own default page — choose a branded or custom design first")
		return
	}

	var html string
	if row.Mode == "custom" {
		html = row.CustomHTML
	} else {
		id := platform.LoadIdentity(ctx, m.settings) // FR-148: branding from the identity resolver, never a second read
		html = renderBrandedHotspotPage(id, n)
	}

	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	if err := vendor.For(n.Vendor).PushHotspotPage(conn, svc.ROSServerName, map[string]string{"login.html": html}); err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	rosVersion := m.currentROSVersionFor(ctx, n)
	if err := markHotspotPagePushed(ctx, m.db, serviceID, row.DesignVersion, rosVersion); err != nil {
		m.internal(w, "mark hotspot page pushed", err)
		return
	}
	_ = auth.Audit(ctx, "nas.hotspot_page_push", "nas", nasID, nil, map[string]any{
		"service_id": serviceID, "mode": row.Mode, "design_version": row.DesignVersion, "ros_version": rosVersion,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"status": "pushed", "design_version": row.DesignVersion, "ros_version": rosVersion})
}

// renderBrandedHotspotPage is HikRAD's built-in branded template (FR-148):
// username/password + voucher-code login, and a reserved (disabled) self-
// registration tab v4-11 fills in — never omitted, so the router-side markup
// v4-11 needs is already present rather than requiring every operator's
// pushed page to be re-pushed again when that phase ships.
func renderBrandedHotspotPage(id platform.Identity, n nasRow) string {
	name := id.Name
	if name == "" {
		name = n.Name
	}
	theme := "#2563eb"
	if id.ThemeColor != nil && *id.ThemeColor != "" {
		theme = *id.ThemeColor
	}
	logo := ""
	if id.LogoURL != nil && *id.LogoURL != "" {
		logo = fmt.Sprintf(`<img src="%s" alt="%s" class="logo">`, *id.LogoURL, name)
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>
  body{font-family:sans-serif;background:#f4f4f5;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
  .card{background:#fff;border-radius:12px;padding:32px;max-width:360px;width:100%%;box-shadow:0 4px 24px rgba(0,0,0,.08)}
  .logo{max-height:48px;margin-bottom:16px}
  h1{font-size:18px;margin:0 0 16px;color:#111}
  .tabs{display:flex;gap:8px;margin-bottom:16px}
  .tab{flex:1;text-align:center;padding:8px;border-radius:8px;background:#f4f4f5;cursor:pointer}
  .tab.active{background:%s;color:#fff}
  input{width:100%%;box-sizing:border-box;padding:10px;margin-bottom:10px;border:1px solid #ddd;border-radius:8px}
  button{width:100%%;padding:10px;border:0;border-radius:8px;background:%s;color:#fff;font-weight:600;cursor:pointer}
</style></head>
<body>
  <form class="card" name="login" action="$(link-login-only)" method="post">
    %s
    <h1>%s</h1>
    <div class="tabs">
      <div class="tab active">Username / Password</div>
      <div class="tab">Voucher</div>
      <!-- self-registration tab reserved for v4-11 (FR-169) -->
    </div>
    <input type="hidden" name="dst" value="$(link-orig)">
    <input type="text" name="username" placeholder="Username or voucher code" required>
    <input type="password" name="password" placeholder="Password (leave blank for voucher)">
    <button type="submit">Connect</button>
  </form>
</body></html>`, name, theme, theme, logo, name)
}
