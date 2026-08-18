package radius

// Router manager I HTTP surface (v4-6, FR-139-143). Every handler here reads
// exclusively from device_store.go's tables — the device worker
// (device_worker.go) is the only thing that ever dials a router on this
// phase's read paths. The two exceptions, reboot and ping (FR-142), are
// explicitly allowed to call the router synchronously (the phase brief's own
// exception) because they are user-initiated action clicks, not page loads.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
)

// --- FR-139: monitor grid ---------------------------------------------------

type deviceMonitorView struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"` // "nas" | "monitor" — the NAS-vs-monitor tag
	Name         string     `json:"name"`
	IP           string     `json:"ip"`
	Enabled      bool       `json:"enabled"`
	Sampled      bool       `json:"sampled"`
	HasAPICreds  bool       `json:"has_api_credentials"`
	Reachable    *bool      `json:"reachable,omitempty"`
	ROSVersion   string     `json:"ros_version,omitempty"`
	CPUPercent   *float64   `json:"cpu_percent,omitempty"`
	CPUCores     *int       `json:"cpu_cores,omitempty"`
	MemPercent   *float64   `json:"mem_percent,omitempty"`
	SessionCount *int       `json:"session_count,omitempty"`
	UptimeSecs   *int64     `json:"uptime_secs,omitempty"`
	LastSampleAt *time.Time `json:"last_sample_at,omitempty"`
}

// listDeviceMonitorHandler implements GET /nas/monitor (FR-139). Reachability/
// CPU/memory/session/uptime for NAS rows come only from device_metrics — the
// device worker's own samples (never a synchronous router call here, gate
// items 2/3). Monitored devices (FR-60, a separate probe-target list owned by
// monitorsvc) are merged in read-only, tagged "monitor", from the existing
// ICMP-derived health_probes table — the same direct cross-domain table read
// monitorsvc's own target.go already does against `nas` in the other
// direction, not a new coupling pattern.
func (m *module) listDeviceMonitorHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := listNAS(ctx, m.db)
	if err != nil {
		m.internal(w, "list nas", err)
		return
	}
	latest, err := latestDeviceMetrics(ctx, m.db)
	if err != nil {
		m.internal(w, "latest device metrics", err)
		return
	}
	items := make([]deviceMonitorView, 0, len(list)+8)
	for _, n := range list {
		v := deviceMonitorView{
			ID: n.ID, Kind: "nas", Name: n.Name, IP: n.IP, Enabled: n.Enabled,
			HasAPICreds: n.APIUser != "" && len(n.APIPasswordEnc) > 0,
		}
		if row, ok := latest[n.ID]; ok {
			v.Sampled = true
			reachable := row.Reachable
			v.Reachable = &reachable
			v.ROSVersion = row.ROSVersion
			t := row.Time
			v.LastSampleAt = &t
			if row.Reachable {
				cpu, cores, mem, sess, up := row.CPUPercent, row.CPUCores, row.MemPercent, row.SessionCount, row.UptimeSecs
				v.CPUPercent, v.CPUCores, v.MemPercent, v.SessionCount, v.UptimeSecs = &cpu, &cores, &mem, &sess, &up
			}
		}
		items = append(items, v)
	}

	monRows, err := m.db.Query(ctx, `SELECT id::text, name, host(ip), enabled FROM monitored_devices ORDER BY name`)
	if err != nil {
		m.internal(w, "list monitored devices", err)
		return
	}
	type monRow struct {
		id, name, ip string
		enabled      bool
	}
	var mons []monRow
	for monRows.Next() {
		var mr monRow
		if err := monRows.Scan(&mr.id, &mr.name, &mr.ip, &mr.enabled); err != nil {
			monRows.Close()
			m.internal(w, "scan monitored device", err)
			return
		}
		mons = append(mons, mr)
	}
	monRows.Close()
	if err := monRows.Err(); err != nil {
		m.internal(w, "rows monitored devices", err)
		return
	}
	icmpOK := map[string]bool{}
	if len(mons) > 0 {
		rows, err := m.db.Query(ctx,
			`SELECT DISTINCT ON (device_id) device_id::text, ok FROM health_probes
			  WHERE device_id IS NOT NULL AND kind = 'icmp' ORDER BY device_id, at DESC`)
		if err == nil {
			for rows.Next() {
				var id string
				var ok bool
				if rows.Scan(&id, &ok) == nil {
					icmpOK[id] = ok
				}
			}
			rows.Close()
		}
	}
	for _, mr := range mons {
		ok, sampled := icmpOK[mr.id]
		v := deviceMonitorView{ID: mr.id, Kind: "monitor", Name: mr.name, IP: mr.ip, Enabled: mr.enabled, Sampled: sampled}
		if sampled {
			v.Reachable = &ok
		}
		items = append(items, v)
	}

	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, ""))
}

// --- FR-140: per-tab read endpoint -----------------------------------------

// deviceTabEnvelope is the frozen C-shape every /nas/{id}/device/{tab} read
// returns: {data, read_at, stale, ownership}. NoAPICredentials is additive
// (not part of the frozen shape) — it's what lets the panel show "add API
// credentials" instead of a spinner that never resolves for a NAS the worker
// has never been able to sample.
type deviceTabEnvelope struct {
	Data             any        `json:"data"`
	ReadAt           *time.Time `json:"read_at"`
	Stale            bool       `json:"stale"`
	Ownership        string     `json:"ownership"`
	NoAPICredentials bool       `json:"no_api_credentials"`
}

var deviceTabs = map[string]bool{
	"overview": true, "interfaces": true, "general": true,
	"bridge": true, "vlan": true, "ip": true, "dns": true,
}

func (m *module) deviceTabHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	tab := chi.URLParam(r, "tab")
	if !deviceTabs[tab] {
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown device tab")
		return
	}
	n, err := getNAS(ctx, m.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nas not found")
		return
	}
	if err != nil {
		m.internal(w, "get nas", err)
		return
	}
	noCreds := n.APIUser == "" || len(n.APIPasswordEnc) == 0

	switch tab {
	case "overview":
		m.overviewTab(w, r, n, noCreds)
	case "interfaces":
		m.interfacesTab(w, r, n, noCreds)
	default:
		m.cachedTab(w, r, n, deviceObjectClass(tab), noCreds)
	}
}

func overviewHistoryWindow(r *http.Request) time.Duration {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours > 24*90 {
		hours = 24 * 90
	}
	return time.Duration(hours) * time.Hour
}

type overviewHistoryPoint struct {
	Time         time.Time `json:"time"`
	Reachable    bool      `json:"reachable"`
	CPUPercent   float64   `json:"cpu_percent"`
	MemPercent   float64   `json:"mem_percent"`
	SessionCount int       `json:"session_count"`
}

type overviewData struct {
	Identity     string                 `json:"identity"`
	BoardName    string                 `json:"board_name"`
	ROSVersion   string                 `json:"ros_version"`
	SerialNumber string                 `json:"serial_number"`
	Reachable    bool                   `json:"reachable"`
	CPUPercent   float64                `json:"cpu_percent"`
	CPUCores     int                    `json:"cpu_cores"`
	MemPercent   float64                `json:"mem_percent"`
	SessionCount int                    `json:"session_count"`
	UptimeSecs   int64                  `json:"uptime_secs"`
	History      []overviewHistoryPoint `json:"history"`
}

// overviewTab implements FR-141's Overview tab. Ownership is always "router"
// (identity is never something HikRAD writes) — the badge is included purely
// for envelope-shape consistency across every tab.
func (m *module) overviewTab(w http.ResponseWriter, r *http.Request, n nasRow, noCreds bool) {
	ctx := r.Context()
	latest, ok, err := latestOneDeviceMetric(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "latest device metric", err)
		return
	}
	env := deviceTabEnvelope{Ownership: "router", NoAPICredentials: noCreds, Stale: true}
	if !ok {
		httpapi.JSON(w, http.StatusOK, env)
		return
	}
	since := time.Now().Add(-overviewHistoryWindow(r))
	hist, err := deviceMetricsHistory(ctx, m.db, n.ID, since)
	if err != nil {
		m.internal(w, "device metrics history", err)
		return
	}
	points := make([]overviewHistoryPoint, 0, len(hist))
	for _, h := range hist {
		points = append(points, overviewHistoryPoint{
			Time: h.Time, Reachable: h.Reachable, CPUPercent: h.CPUPercent,
			MemPercent: h.MemPercent, SessionCount: h.SessionCount,
		})
	}
	env.Data = overviewData{
		Identity: latest.Identity, BoardName: latest.BoardName, ROSVersion: latest.ROSVersion,
		SerialNumber: latest.SerialNumber, Reachable: latest.Reachable,
		CPUPercent: latest.CPUPercent, CPUCores: latest.CPUCores, MemPercent: latest.MemPercent,
		SessionCount: latest.SessionCount, UptimeSecs: latest.UptimeSecs, History: points,
	}
	t := latest.Time
	env.ReadAt = &t
	env.Stale = !latest.Reachable || time.Since(latest.Time) > staleAfter
	httpapi.JSON(w, http.StatusOK, env)
}

type interfaceView struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Running   bool   `json:"running"`
	Disabled  bool   `json:"disabled"`
	MAC       string `json:"mac_address"`
	MTU       int    `json:"mtu"`
	Comment   string `json:"comment"`
	RxBps     int64  `json:"rx_bps"`
	TxBps     int64  `json:"tx_bps"`
	Ownership string `json:"ownership"`
}

type trafficPoint struct {
	Time  time.Time `json:"time"`
	RxBps int64     `json:"rx_bps"`
	TxBps int64     `json:"tx_bps"`
}

type interfacesData struct {
	Interfaces     []interfaceView `json:"interfaces"`
	SelectedIface  string          `json:"selected_iface,omitempty"`
	TrafficHistory []trafficPoint  `json:"traffic_history,omitempty"`
}

// interfacesTab implements FR-141's Interfaces tab. Every interface badges
// "router": v4-6 never creates an interface, so nothing here is ever
// system-managed. ?iface= optionally requests that one interface's traffic
// history (FR-141's per-interface chart) instead of shipping every
// interface's full history on every load.
func (m *module) interfacesTab(w http.ResponseWriter, r *http.Request, n nasRow, noCreds bool) {
	ctx := r.Context()
	rows, err := listDeviceInterfaces(ctx, m.db, n.ID)
	if err != nil {
		m.internal(w, "list device interfaces", err)
		return
	}
	env := deviceTabEnvelope{Ownership: "router", NoAPICredentials: noCreds, Stale: true}
	if len(rows) == 0 {
		httpapi.JSON(w, http.StatusOK, env)
		return
	}
	views := make([]interfaceView, 0, len(rows))
	var newest time.Time
	for _, ifc := range rows {
		views = append(views, interfaceView{
			Name: ifc.Name, Type: ifc.Type, Running: ifc.Running, Disabled: ifc.Disabled,
			MAC: ifc.MAC, MTU: ifc.MTU, Comment: ifc.Comment, RxBps: ifc.RxBps, TxBps: ifc.TxBps,
			Ownership: "router",
		})
		if ifc.UpdatedAt.After(newest) {
			newest = ifc.UpdatedAt
		}
	}
	data := interfacesData{Interfaces: views}
	if iface := r.URL.Query().Get("iface"); iface != "" {
		since := time.Now().Add(-overviewHistoryWindow(r))
		hist, err := interfaceTrafficHistory(ctx, m.db, n.ID, iface, since)
		if err != nil {
			m.internal(w, "interface traffic history", err)
			return
		}
		data.SelectedIface = iface
		data.TrafficHistory = make([]trafficPoint, 0, len(hist))
		for _, h := range hist {
			data.TrafficHistory = append(data.TrafficHistory, trafficPoint{Time: h.Time, RxBps: h.RxBps, TxBps: h.TxBps})
		}
	}
	env.Data = data
	env.ReadAt = &newest
	env.Stale = time.Since(newest) > staleAfter
	httpapi.JSON(w, http.StatusOK, env)
}

// cachedTab implements the FR-143 Network tabs (Bridge/VLAN/IP/DNS) and the
// FR-142 General tab identity/time/NTP block, all served from
// device_config_cache — never a synchronous router call.
func (m *module) cachedTab(w http.ResponseWriter, r *http.Request, n nasRow, class deviceObjectClass, noCreds bool) {
	ctx := r.Context()
	entry, err := readDeviceCache(ctx, m.db, n.ID, class)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		m.internal(w, "read device cache", err)
		return
	}
	env := deviceTabEnvelope{Ownership: tabDefaultOwnership(class), NoAPICredentials: noCreds, Stale: true}
	if !entry.Found {
		httpapi.JSON(w, http.StatusOK, env)
		return
	}
	env.Data = entry.Data
	t := entry.ReadAt
	env.ReadAt = &t
	env.Stale = entry.isStale()
	httpapi.JSON(w, http.StatusOK, env)
}

// tabDefaultOwnership is the envelope-level ownership hint (per-item badges,
// where they exist, live inside `data` — see ipTabView). Every v4-6 tab
// except IP is uniformly "router" because nothing in this phase ever creates
// a bridge, VLAN, DNS entry or interface; IP is "mixed" because its DHCP
// servers can badge "system" (FR-108 pool ownership).
func tabDefaultOwnership(class deviceObjectClass) string {
	if class == classIP {
		return "mixed"
	}
	return "router"
}

// --- FR-140 explicit refresh -------------------------------------------------

// deviceRefreshHandler implements POST /nas/{id}/device/refresh: an explicit
// "refresh now" (FR-139/FR-140) that never blocks the request on the router.
// It kicks one off-cycle worker tick in the background and returns
// immediately; the panel polls the tab endpoints afterward and sees read_at
// advance once the tick completes — this is what keeps the frozen "no
// synchronous RouterOS call in the HTTP request path" contract true even for
// an explicit refresh action.
func (m *module) deviceRefreshHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	n, err := getNAS(ctx, m.db, id)
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
	go m.sampleOneDevice(context.Background(), n)
	httpapi.JSON(w, http.StatusAccepted, map[string]any{"status": "refreshing"})
}

// --- FR-142: reboot + ping ---------------------------------------------------

type deviceRebootRequest struct {
	Confirm bool `json:"confirm"`
}

// deviceRebootHandler implements POST /nas/{id}/device/reboot (FR-142): "the
// first genuinely destructive router action in the product." Double-confirm
// is a panel-level UX requirement (two separate confirmation steps before
// this call is ever made); the server's own contract is refusing outright
// without an explicit confirm=true, permission-gating on nas.reboot (kept
// separate from nas.edit — a reboot is not a config edit), auditing with the
// operator's identity (auth.Audit reads the actor from ctx), and refusing
// where "ownership" — HikRAD having been handed control of this router at
// all, the only consent gesture the schema has — is not established: no
// saved API credentials.
func (m *module) deviceRebootHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	n, err := getNAS(ctx, m.db, id)
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
			"HikRAD has no saved API credentials for this router — reboot refused")
		return
	}
	var req deviceRebootRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !req.Confirm {
		httpapi.Error(w, http.StatusUnprocessableEntity, "confirmation_required",
			"reboot requires explicit confirmation")
		return
	}
	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		m.internal(w, "decrypt nas api password", err)
		return
	}
	conn, err := m.dialROS(ctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", "could not connect to the router: "+err.Error())
		return
	}
	defer conn.Close()
	if err := vendor.For(n.Vendor).Reboot(conn); err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.reboot", "nas", n.ID, nil, map[string]any{"name": n.Name, "ip": n.IP})
	httpapi.JSON(w, http.StatusOK, map[string]any{"status": "reboot_sent"})
}

type devicePingRequest struct {
	Target string `json:"target" validate:"required"`
	Count  int    `json:"count"`
}

// devicePingHandler implements POST /nas/{id}/device/ping (FR-142's
// diagnostic tool). Read-only on the router (RouterOS's /ping is a query
// command) but still requires saved API credentials and is audited like any
// other router-facing action; no confirmation needed since it changes
// nothing.
func (m *module) devicePingHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	n, err := getNAS(ctx, m.db, id)
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
	var req devicePingRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		m.internal(w, "decrypt nas api password", err)
		return
	}
	conn, err := m.dialROS(ctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", "could not connect to the router: "+err.Error())
		return
	}
	defer conn.Close()
	res, err := vendor.For(n.Vendor).Ping(conn, req.Target, req.Count)
	if err != nil {
		httpapi.Error(w, http.StatusBadGateway, "router_unreachable", err.Error())
		return
	}
	_ = auth.Audit(ctx, "nas.ping", "nas", n.ID, nil, map[string]any{"target": req.Target, "received": res.Received, "sent": res.Sent})
	httpapi.JSON(w, http.StatusOK, res)
}
