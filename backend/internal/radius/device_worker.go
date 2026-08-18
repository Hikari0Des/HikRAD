package radius

// Router manager I device worker (v4-6, FR-139-143). Runs for the process
// lifetime inside hikrad-api (same home as enforce.go's/tod.go's/
// limitsworker.go's sweeps — started from module.go's Register), on its own
// ticker, and is the ONLY thing in this phase that ever dials a router: every
// HTTP handler in device_api.go reads exclusively from the tables this file
// writes (device_metrics, device_interfaces, interface_traffic,
// device_config_cache), which is what makes "no synchronous RouterOS call in
// the HTTP request path" true by construction rather than by convention.
//
// deviceWorkerInterval deliberately reuses monitorsvc's existing SNMP probe
// cadence (60s) rather than introducing a new configurable interval — the
// phase brief is explicit that device-metrics sampling rides the existing
// worker cadence, not a new setting.
import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/radius/vendor"
)

const deviceWorkerInterval = 60 * time.Second

// startDeviceWorker drives the sampling loop until ctx is cancelled.
func (m *module) startDeviceWorker(ctx context.Context) {
	t := time.NewTicker(deviceWorkerInterval)
	defer t.Stop()
	m.sampleAllDevices(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.sampleAllDevices(ctx)
		}
	}
}

// sampleAllDevices samples every enabled NAS with saved RouterOS API
// credentials. A NAS without credentials is silently skipped — same
// "422 no_api_credentials" boundary every other router-facing feature in this
// codebase already enforces (FR-56/62.6/65/67); the device tabs surface that
// as an explicit state rather than the worker guessing at unauthenticated
// reachability.
func (m *module) sampleAllDevices(ctx context.Context) {
	list, err := listNAS(ctx, m.db)
	if err != nil {
		m.log.Warn("device worker: list nas failed", "error", err)
		return
	}
	for _, n := range list {
		if !n.Enabled || n.APIUser == "" || len(n.APIPasswordEnc) == 0 {
			continue
		}
		m.sampleOneDevice(ctx, n)
	}
}

// sampleOneDevice connects once and reads every FR-139-143 data slice off
// that single session, so a router pays for one login per tick rather than
// one per tab. Any failure (bad credentials, unreachable, decrypt error)
// marks the device unreachable in device_metrics and leaves every cached tab
// exactly as it was (gate item 2: a dead router renders cached state with a
// staleness marker, never a blank tab) — see writeDeviceCache's reachable=false
// branch.
func (m *module) sampleOneDevice(ctx context.Context, n nasRow) {
	sctx, cancel := context.WithTimeout(ctx, vendor.DialROSTimeout+5*time.Second)
	defer cancel()

	apiPassword, err := decryptToString(n.APIPasswordEnc)
	if err != nil {
		m.log.Warn("device worker: decrypt api password failed", "nas_id", n.ID, "error", err)
		m.markUnreachable(sctx, n.ID)
		return
	}
	conn, err := m.dialROS(sctx, n.IP, apiPortOrDefault(n.APIPort), n.APIUser, apiPassword)
	if err != nil {
		m.markUnreachable(sctx, n.ID)
		return
	}
	defer conn.Close()

	adapter := vendor.For(n.Vendor)

	sample, err := adapter.ReadDeviceMonitor(conn)
	if err != nil {
		m.markUnreachable(sctx, n.ID)
		return
	}
	if err := writeDeviceMetrics(sctx, m.db, n.ID, true, sample); err != nil {
		m.log.Warn("device worker: write metrics failed", "nas_id", n.ID, "error", err)
	}

	if ifaces, err := adapter.ReadInterfaces(conn); err == nil {
		if err := upsertDeviceInterfaces(sctx, m.db, n.ID, ifaces); err != nil {
			m.log.Warn("device worker: write interfaces failed", "nas_id", n.ID, "error", err)
		}
	} else {
		m.log.Warn("device worker: read interfaces failed", "nas_id", n.ID, "error", err)
	}

	m.refreshConfigCache(sctx, n.ID, adapter, conn)
}

// refreshConfigCache reads and caches every FR-140 config-only tab
// (General/Bridge/VLAN/IP/DNS). Each section is independent: one section's
// read error only skips that section's cache write (the section keeps
// showing its own last-known state), it never fails the whole tick.
func (m *module) refreshConfigCache(ctx context.Context, nasID string, adapter vendor.Adapter, conn vendor.ROSConn) {
	if g, err := adapter.ReadGeneral(conn); err == nil {
		_ = writeDeviceCache(ctx, m.db, nasID, classGeneral, true, g)
	} else {
		m.log.Warn("device worker: read general failed", "nas_id", nasID, "error", err)
	}
	if b, err := adapter.ReadBridges(conn); err == nil {
		_ = writeDeviceCache(ctx, m.db, nasID, classBridge, true, ownedBridges(b))
	} else {
		m.log.Warn("device worker: read bridges failed", "nas_id", nasID, "error", err)
	}
	if v, err := adapter.ReadVLANs(conn); err == nil {
		_ = writeDeviceCache(ctx, m.db, nasID, classVLAN, true, ownedVLANs(v))
	} else {
		m.log.Warn("device worker: read vlans failed", "nas_id", nasID, "error", err)
	}
	if ip, err := adapter.ReadIPTab(conn); err == nil {
		view, verr := m.ipTabView(ctx, ip)
		if verr != nil {
			m.log.Warn("device worker: ip tab ownership lookup failed", "nas_id", nasID, "error", verr)
			view = ipTabView{Snapshot: ip} // still cache, everything reads as router-owned
		}
		_ = writeDeviceCache(ctx, m.db, nasID, classIP, true, view)
	} else {
		m.log.Warn("device worker: read ip tab failed", "nas_id", nasID, "error", err)
	}
	if d, err := adapter.ReadDNS(conn); err == nil {
		_ = writeDeviceCache(ctx, m.db, nasID, classDNS, true, d)
	} else {
		m.log.Warn("device worker: read dns failed", "nas_id", nasID, "error", err)
	}
}

func (m *module) markUnreachable(ctx context.Context, nasID string) {
	_ = writeDeviceMetrics(ctx, m.db, nasID, false, vendor.DeviceMonitorSample{})
	for _, c := range []deviceObjectClass{classGeneral, classBridge, classVLAN, classIP, classDNS} {
		_ = writeDeviceCache(ctx, m.db, nasID, c, false, nil)
	}
}

// --- ownership badging (router-ownership principle, applied to v4-6's
// read-only tabs). Nothing in this phase writes bridges/VLANs/DNS to a
// router, so those object classes badge "router" (adopted reference)
// unconditionally and honestly — there is no HikRAD-created bridge or VLAN
// for "system" to mean anything yet (that is v4-7's write surface). The IP
// tab's DHCP servers are the one place a v4-6 read can legitimately badge
// "system": ip_pools already carries FR-108's management_mode, so a DHCP
// server whose address-pool name matches a HikRAD-owned pool is badged
// accordingly — computed server-side, never inferred by the panel.

type ownedItem[T any] struct {
	Item      T      `json:"item"`
	Ownership string `json:"ownership"`
}

func ownedBridges(bridges []vendor.BridgeInfo) []ownedItem[vendor.BridgeInfo] {
	out := make([]ownedItem[vendor.BridgeInfo], 0, len(bridges))
	for _, b := range bridges {
		out = append(out, ownedItem[vendor.BridgeInfo]{Item: b, Ownership: "router"})
	}
	return out
}

func ownedVLANs(vlans []vendor.VLANInfo) []ownedItem[vendor.VLANInfo] {
	out := make([]ownedItem[vendor.VLANInfo], 0, len(vlans))
	for _, v := range vlans {
		out = append(out, ownedItem[vendor.VLANInfo]{Item: v, Ownership: "router"})
	}
	return out
}

// ipTabView is the cached IP-tab payload: the raw snapshot plus a per-DHCP-
// server ownership badge computed once at cache-write time (never inferred
// client-side, per the frozen contract).
type ipTabView struct {
	Snapshot        vendor.IPTabSnapshot `json:"snapshot"`
	DHCPServerOwner map[string]string    `json:"dhcp_server_ownership"` // server name -> "system"|"router"
}

func (m *module) ipTabView(ctx context.Context, snap vendor.IPTabSnapshot) (ipTabView, error) {
	owned, err := systemOwnedPoolNames(ctx, m.db)
	if err != nil {
		return ipTabView{}, err
	}
	badges := make(map[string]string, len(snap.DHCPServers))
	for _, s := range snap.DHCPServers {
		if owned[s.PoolName] {
			badges[s.Name] = "system"
		} else {
			badges[s.Name] = "router"
		}
	}
	return ipTabView{Snapshot: snap, DHCPServerOwner: badges}, nil
}

func systemOwnedPoolNames(ctx context.Context, db pgxQuerier) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT name FROM ip_pools WHERE management_mode = 'system'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
