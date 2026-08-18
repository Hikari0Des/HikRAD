package radius

// FR-147 RouterOS Update tab HTTP surface. Three endpoints:
//
//	GET  /api/v1/nas/{id}/update            -> vendor.RouterUpdateInfo (read-only)
//	POST /api/v1/nas/{id}/update/check      -> triggers the router's own check, then re-reads
//	POST /api/v1/nas/{id}/update/apply      {confirm:true} -> installs the available update
//
// Read and report by default (the phase brief); apply is double-confirmed
// (both the panel's own two-step confirm AND this endpoint's confirm=true
// body field, same posture as Reboot/FR-142), permission-gated on nas.update
// (kept separate from nas.edit and nas.reboot — installing firmware is
// neither a config edit nor the reboot action itself, even though it also
// reboots the router), and audited with an explicit warning surfaced by the
// panel that the device will reboot and sessions will drop.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

func (m *module) updateInfoHandler(w http.ResponseWriter, r *http.Request) {
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

	info, err := vendor.For(n.Vendor).ReadUpdateInfo(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	httpapi.JSON(w, http.StatusOK, info)
}

func (m *module) updateCheckHandler(w http.ResponseWriter, r *http.Request) {
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

	info, err := vendor.For(n.Vendor).CheckForUpdates(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.update_check", "nas", n.ID, nil, map[string]any{
		"latest_version": info.LatestVersion, "update_available": info.UpdateAvailable,
	})
	httpapi.JSON(w, http.StatusOK, info)
}

type updateApplyRequest struct {
	Confirm bool   `json:"confirm"`
	Channel string `json:"channel"`
}

// updateApplyHandler implements POST .../update/apply (FR-147): the phase's
// other genuinely destructive write besides Reboot — same refusal posture as
// deviceRebootHandler (no_api_credentials, explicit confirm required), same
// audit shape, and the caller is expected to have already double-confirmed
// in the UI with the "the device will reboot and sessions will drop"
// warning visible.
func (m *module) updateApplyHandler(w http.ResponseWriter, r *http.Request) {
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
	var req updateApplyRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !req.Confirm {
		httpapi.Error(w, http.StatusUnprocessableEntity, "confirmation_required",
			"installing a RouterOS update requires explicit confirmation — the device will reboot and sessions will drop")
		return
	}
	conn, err := m.connectNAS(ctx, n)
	if err != nil {
		m.autoSetupConnectError(w, err)
		return
	}
	defer func() { _ = conn.Close() }()

	if err := vendor.For(n.Vendor).ApplyRouterUpdate(conn, req.Channel); err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.update_apply", "nas", n.ID, nil, map[string]any{"channel": req.Channel})
	httpapi.JSON(w, http.StatusOK, map[string]any{"status": "update_installing"})
}
