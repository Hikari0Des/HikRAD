package radius

// NAS config inspection (v2 phase 3, FR-65, contract C2):
//
//	GET /api/v1/nas/{id}/config -> read-only RADIUS-relevant router state
//
// Pure read: reuses the same saved API credentials and dial path as auto-setup
// (FR-56.2) and the FR-13.1 version probe. Never writes.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

type radiusEntryConfigView struct {
	Address       string `json:"address"`
	Service       string `json:"service"`
	Comment       string `json:"comment"`
	SrcAddress    string `json:"src_address"`
	SecretPresent bool   `json:"secret_present"`
}

type hotspotProfileConfigView struct {
	Name              string `json:"name"`
	UseRadius         bool   `json:"use_radius"`
	InterimUpdateSecs int    `json:"interim_update_secs"`
}

// nasConfigResponse is the C2 response shape.
type nasConfigResponse struct {
	NASID      string                  `json:"nas_id"`
	ROSVersion string                  `json:"ros_version"`
	BoardName  string                  `json:"board_name"`
	Identity   string                  `json:"identity"`
	Radius     []radiusEntryConfigView `json:"radius"`
	RadiusIncoming struct {
		Accept bool `json:"accept"`
		Port   int  `json:"port"`
	} `json:"radius_incoming"`
	PPPAAA struct {
		UseRadius         bool `json:"use_radius"`
		Accounting        bool `json:"accounting"`
		InterimUpdateSecs int  `json:"interim_update_secs"`
	} `json:"ppp_aaa"`
	HotspotProfiles []hotspotProfileConfigView `json:"hotspot_profiles"`
	WalledGarden    []string                   `json:"walled_garden"`
}

func nasConfigView(nasID string, info vendor.DeviceInfo, snap vendor.ConfigSnapshot) nasConfigResponse {
	resp := nasConfigResponse{
		NASID: nasID, ROSVersion: info.Version, BoardName: info.BoardName, Identity: info.Identity,
		Radius: make([]radiusEntryConfigView, 0, len(snap.RadiusEntries)),
		HotspotProfiles: make([]hotspotProfileConfigView, 0, len(snap.HotspotProfiles)),
		WalledGarden:    nonNilStrings(snap.WalledGarden),
	}
	for _, e := range snap.RadiusEntries {
		resp.Radius = append(resp.Radius, radiusEntryConfigView{
			Address: e.Address, Service: e.Service, Comment: e.Comment,
			SrcAddress: e.SrcAddress, SecretPresent: e.SecretPresent,
		})
	}
	resp.RadiusIncoming.Accept = snap.RadiusIncoming.Accept
	resp.RadiusIncoming.Port = snap.RadiusIncoming.Port
	resp.PPPAAA.UseRadius = snap.PPPAAA.UseRadius
	resp.PPPAAA.Accounting = snap.PPPAAA.Accounting
	resp.PPPAAA.InterimUpdateSecs = snap.PPPAAA.InterimUpdateSecs
	for _, p := range snap.HotspotProfiles {
		resp.HotspotProfiles = append(resp.HotspotProfiles, hotspotProfileConfigView{
			Name: p.Name, UseRadius: p.UseRadius, InterimUpdateSecs: p.InterimUpdateSecs,
		})
	}
	return resp
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nasConfigHandler implements GET /nas/{id}/config (FR-65.1). 422 without
// saved API credentials / 502 when the router doesn't answer — identical
// contract to probeNASHandler/discoverServicesHandler, which this deliberately
// mirrors (FR-65.1).
func (m *module) nasConfigHandler(w http.ResponseWriter, r *http.Request) {
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
	if n.APIUser == "" || len(n.APIPasswordEnc) == 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_api_credentials",
			"no RouterOS API credentials saved for this NAS")
		return
	}
	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		m.internal(w, "decrypt nas api password", err)
		return
	}
	conn, err := m.dialROS(ctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"could not connect to the router: "+err.Error())
		return
	}
	defer conn.Close()

	adapter := vendor.For(n.Vendor)
	snap, err := adapter.ReadConfig(conn)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable",
			"connected but could not read the router's config: "+err.Error())
		return
	}
	// Version/board/identity ride along (FR-65's "reuses the v1.1 probe");
	// best-effort like probeNASHandler's identity read — never fails the call.
	info, _ := vendor.ReadDeviceInfo(conn)

	_ = auth.Audit(ctx, "nas.config_inspect", "nas", n.ID, nil, map[string]any{
		"radius_entries": len(snap.RadiusEntries), "hotspot_profiles": len(snap.HotspotProfiles),
	})
	httpapi.JSON(w, http.StatusOK, nasConfigView(n.ID, info, snap))
}
