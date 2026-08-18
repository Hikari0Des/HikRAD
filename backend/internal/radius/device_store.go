package radius

// Router manager I data layer (v4-6, FR-139-143, migrations 0607-0609). The
// device worker (device_worker.go) is the sole writer of every table here;
// the HTTP handlers (device_api.go) only ever read — that split IS the
// "no synchronous router call in the request path" contract (gate items 2/3),
// not something enforced by a runtime check.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- device_metrics (FR-139 monitor grid, FR-141 Overview history) ---------

type deviceMetricsRow struct {
	Time         time.Time
	Reachable    bool
	ROSVersion   string
	BoardName    string
	Identity     string
	SerialNumber string
	CPUPercent   float64
	CPUCores     int
	MemPercent   float64
	SessionCount int
	UptimeSecs   int64
}

func writeDeviceMetrics(ctx context.Context, db *pgxpool.Pool, nasID string, reachable bool, s vendor.DeviceMonitorSample) error {
	_, err := db.Exec(ctx,
		`INSERT INTO device_metrics
		    (time, nas_id, reachable, ros_version, board_name, identity, serial_number,
		     cpu_percent, cpu_cores, mem_percent, session_count, uptime_secs)
		 VALUES (now(), $1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		nasID, reachable, s.Version, s.BoardName, s.Identity, s.SerialNumber,
		nullableF(reachable, s.CPUPercent), nullableI(reachable, s.CPUCores),
		nullableF(reachable, s.MemPercent), nullableI(reachable, s.SessionCount),
		nullableI64(reachable, s.UptimeSecs))
	return err
}

func nullableF(ok bool, v float64) any {
	if !ok {
		return nil
	}
	return v
}
func nullableI(ok bool, v int) any {
	if !ok {
		return nil
	}
	return v
}
func nullableI64(ok bool, v int64) any {
	if !ok {
		return nil
	}
	return v
}

// latestDeviceMetrics reads every enabled NAS's most recent sample in one
// query (FR-139 monitor grid). A NAS the worker has never sampled (freshly
// created, or no API credentials saved) is simply absent from the map — the
// caller renders it as "never sampled" rather than treating that as an error.
func latestDeviceMetrics(ctx context.Context, db *pgxpool.Pool) (map[string]deviceMetricsRow, error) {
	rows, err := db.Query(ctx,
		`SELECT DISTINCT ON (nas_id) nas_id::text, time, reachable, ros_version, board_name, identity,
		        serial_number, coalesce(cpu_percent,0), coalesce(cpu_cores,0), coalesce(mem_percent,0),
		        coalesce(session_count,0), coalesce(uptime_secs,0)
		   FROM device_metrics
		  ORDER BY nas_id, time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]deviceMetricsRow{}
	for rows.Next() {
		var id string
		var r deviceMetricsRow
		if err := rows.Scan(&id, &r.Time, &r.Reachable, &r.ROSVersion, &r.BoardName, &r.Identity,
			&r.SerialNumber, &r.CPUPercent, &r.CPUCores, &r.MemPercent, &r.SessionCount, &r.UptimeSecs); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}

// latestOneDeviceMetric reads one NAS's most recent sample (FR-141 Overview
// identity block). ok=false means the worker has never sampled this NAS.
func latestOneDeviceMetric(ctx context.Context, db *pgxpool.Pool, nasID string) (deviceMetricsRow, bool, error) {
	var r deviceMetricsRow
	err := db.QueryRow(ctx,
		`SELECT time, reachable, ros_version, board_name, identity, serial_number,
		        coalesce(cpu_percent,0), coalesce(cpu_cores,0), coalesce(mem_percent,0),
		        coalesce(session_count,0), coalesce(uptime_secs,0)
		   FROM device_metrics WHERE nas_id = $1::uuid ORDER BY time DESC LIMIT 1`, nasID).
		Scan(&r.Time, &r.Reachable, &r.ROSVersion, &r.BoardName, &r.Identity, &r.SerialNumber,
			&r.CPUPercent, &r.CPUCores, &r.MemPercent, &r.SessionCount, &r.UptimeSecs)
	if err == pgx.ErrNoRows {
		return deviceMetricsRow{}, false, nil
	}
	if err != nil {
		return deviceMetricsRow{}, false, err
	}
	return r, true, nil
}

// deviceMetricsHistory reads one NAS's samples since `since` (FR-141 Overview
// charts), oldest first.
func deviceMetricsHistory(ctx context.Context, db *pgxpool.Pool, nasID string, since time.Time) ([]deviceMetricsRow, error) {
	rows, err := db.Query(ctx,
		`SELECT time, reachable, coalesce(cpu_percent,0), coalesce(mem_percent,0), coalesce(session_count,0)
		   FROM device_metrics
		  WHERE nas_id = $1::uuid AND time >= $2
		  ORDER BY time ASC`, nasID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceMetricsRow
	for rows.Next() {
		var r deviceMetricsRow
		if err := rows.Scan(&r.Time, &r.Reachable, &r.CPUPercent, &r.MemPercent, &r.SessionCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- device_interfaces + interface_traffic (FR-141 Interfaces tab) --------

type deviceInterfaceRow struct {
	Name      string
	Type      string
	Running   bool
	Disabled  bool
	MAC       string
	MTU       int
	Comment   string
	RxBps     int64
	TxBps     int64
	UpdatedAt time.Time
}

// upsertDeviceInterfaces snapshots the router's current interface state and
// derives a bps rate for each interface from the PREVIOUS sample's byte
// counters (read back inside the same call), writing one interface_traffic
// row per interface per tick. Returns the merged view (state + this tick's
// rate) for the caller's own use if needed; the API layer re-reads from the
// DB rather than trusting this return value, so a partial failure here still
// leaves the DB as the single source of truth.
func upsertDeviceInterfaces(ctx context.Context, db *pgxpool.Pool, nasID string, ifaces []vendor.InterfaceState) error {
	prev, err := previousInterfaceCounters(ctx, db, nasID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, ifc := range ifaces {
		var rxBps, txBps int64
		if p, ok := prev[ifc.Name]; ok {
			elapsed := now.Sub(p.sampledAt).Seconds()
			if elapsed > 0 {
				if d := ifc.RxBytes - p.rxBytes; d >= 0 {
					rxBps = int64(float64(d) * 8 / elapsed)
				}
				if d := ifc.TxBytes - p.txBytes; d >= 0 {
					txBps = int64(float64(d) * 8 / elapsed)
				}
			}
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO device_interfaces
			    (nas_id, name, type, running, disabled, mac_address, mtu, comment, rx_bytes, tx_bytes, rx_bps, tx_bps, sampled_at, updated_at)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
			 ON CONFLICT (nas_id, name) DO UPDATE SET
			    type = EXCLUDED.type, running = EXCLUDED.running, disabled = EXCLUDED.disabled,
			    mac_address = EXCLUDED.mac_address, mtu = EXCLUDED.mtu, comment = EXCLUDED.comment,
			    rx_bytes = EXCLUDED.rx_bytes, tx_bytes = EXCLUDED.tx_bytes,
			    rx_bps = EXCLUDED.rx_bps, tx_bps = EXCLUDED.tx_bps,
			    sampled_at = EXCLUDED.sampled_at, updated_at = EXCLUDED.updated_at`,
			nasID, ifc.Name, ifc.Type, ifc.Running, ifc.Disabled, ifc.MAC, nullMTU(ifc.MTU), ifc.Comment,
			ifc.RxBytes, ifc.TxBytes, rxBps, txBps, now); err != nil {
			return err
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO interface_traffic (time, nas_id, interface, rx_bps, tx_bps) VALUES ($1, $2::uuid, $3, $4, $5)`,
			now, nasID, ifc.Name, rxBps, txBps); err != nil {
			return err
		}
	}
	return nil
}

func nullMTU(m int) any {
	if m == 0 {
		return nil
	}
	return m
}

type prevCounter struct {
	rxBytes, txBytes int64
	sampledAt        time.Time
}

func previousInterfaceCounters(ctx context.Context, db *pgxpool.Pool, nasID string) (map[string]prevCounter, error) {
	rows, err := db.Query(ctx,
		`SELECT name, rx_bytes, tx_bytes, sampled_at FROM device_interfaces WHERE nas_id = $1::uuid`, nasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]prevCounter{}
	for rows.Next() {
		var name string
		var c prevCounter
		if err := rows.Scan(&name, &c.rxBytes, &c.txBytes, &c.sampledAt); err != nil {
			return nil, err
		}
		out[name] = c
	}
	return out, rows.Err()
}

func listDeviceInterfaces(ctx context.Context, db *pgxpool.Pool, nasID string) ([]deviceInterfaceRow, error) {
	rows, err := db.Query(ctx,
		`SELECT name, type, running, disabled, mac_address, coalesce(mtu,0), comment, rx_bps, tx_bps, updated_at
		   FROM device_interfaces WHERE nas_id = $1::uuid ORDER BY name`, nasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceInterfaceRow
	for rows.Next() {
		var r deviceInterfaceRow
		if err := rows.Scan(&r.Name, &r.Type, &r.Running, &r.Disabled, &r.MAC, &r.MTU, &r.Comment,
			&r.RxBps, &r.TxBps, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// interfaceTrafficHistory reads one interface's rate history since `since`.
func interfaceTrafficHistory(ctx context.Context, db *pgxpool.Pool, nasID, iface string, since time.Time) ([]struct {
	Time         time.Time
	RxBps, TxBps int64
}, error) {
	rows, err := db.Query(ctx,
		`SELECT time, rx_bps, tx_bps FROM interface_traffic
		  WHERE nas_id = $1::uuid AND interface = $2 AND time >= $3
		  ORDER BY time ASC`, nasID, iface, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Time         time.Time
		RxBps, TxBps int64
	}
	for rows.Next() {
		var t time.Time
		var rx, tx int64
		if err := rows.Scan(&t, &rx, &tx); err != nil {
			return nil, err
		}
		out = append(out, struct {
			Time         time.Time
			RxBps, TxBps int64
		}{t, rx, tx})
	}
	return out, rows.Err()
}

// --- device_config_cache (FR-140, every read-only tab) ---------------------

// deviceObjectClass enumerates the FR-140 cache slots — one per read-only tab.
type deviceObjectClass string

const (
	classOverview   deviceObjectClass = "overview"
	classInterfaces deviceObjectClass = "interfaces"
	classGeneral    deviceObjectClass = "general"
	classBridge     deviceObjectClass = "bridge"
	classVLAN       deviceObjectClass = "vlan"
	classIP         deviceObjectClass = "ip"
	classDNS        deviceObjectClass = "dns"
)

// deviceCacheHash is the same deterministic-digest technique planHash already
// established for the preview/apply pipeline (autosetup_api.go) — sha256 hex
// of the canonical JSON payload — applied here to a read-model cache instead
// of a preview/apply round trip, so a tab can report "unchanged since last
// read" without a second hashing scheme.
func deviceCacheHash(payload any) string {
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writeDeviceCache upserts one object class's snapshot. reachable=false keeps
// the previous data (a dead router must render the last-known state with a
// staleness marker, never blank it — gate item 2), only refreshing read_at
// and the reachable flag so the caller can compute staleness.
func writeDeviceCache(ctx context.Context, db *pgxpool.Pool, nasID string, class deviceObjectClass, reachable bool, payload any) error {
	if !reachable {
		_, err := db.Exec(ctx,
			`UPDATE device_config_cache SET reachable = false, read_at = now()
			  WHERE nas_id = $1::uuid AND object_class = $2`, nasID, string(class))
		return err
	}
	hash := deviceCacheHash(payload)
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx,
		`INSERT INTO device_config_cache (nas_id, object_class, data, hash, reachable, read_at)
		 VALUES ($1::uuid, $2, $3, $4, true, now())
		 ON CONFLICT (nas_id, object_class) DO UPDATE SET
		    data = EXCLUDED.data, hash = EXCLUDED.hash, reachable = true, read_at = now()`,
		nasID, string(class), data, hash)
	return err
}

// deviceCacheEntry is one row read back for the FR-140 frozen response shape
// ({data, read_at, stale, ownership}).
type deviceCacheEntry struct {
	Data      json.RawMessage
	ReadAt    time.Time
	Reachable bool
	Found     bool
}

// staleAfter is how old a cached read may be before the panel marks it stale
// (distinct from Reachable=false, which means the LAST attempt failed; a tab
// can be stale — old — while the router is currently reachable, simply
// because the worker hasn't ticked since the last edit).
const staleAfter = 5 * time.Minute

func readDeviceCache(ctx context.Context, db *pgxpool.Pool, nasID string, class deviceObjectClass) (deviceCacheEntry, error) {
	var e deviceCacheEntry
	err := db.QueryRow(ctx,
		`SELECT data, read_at, reachable FROM device_config_cache WHERE nas_id = $1::uuid AND object_class = $2`,
		nasID, string(class)).Scan(&e.Data, &e.ReadAt, &e.Reachable)
	if err != nil {
		return deviceCacheEntry{}, err
	}
	e.Found = true
	return e, nil
}

func (e deviceCacheEntry) isStale() bool {
	if !e.Found || !e.Reachable {
		return true
	}
	return time.Since(e.ReadAt) > staleAfter
}
