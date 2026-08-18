package vendor

// Router manager I (v4-6, FR-139-143): the read-only device inspection surface
// plus the two action tools (Reboot/Ping). Every read here is a print/query
// sentence — never add/set — read by the device worker only (radius/
// device_worker.go), never synchronously from an HTTP request handler.
//
// This deliberately reuses the SAME ROSConn/DialROS transport ReadConfig/
// PlanAutoSetup/DiscoverServices already use rather than a second RouterOS
// client, and follows ReadConfig's established shape: best-effort per
// section (a router with no /ip hotspot at all, say, degrades that section to
// empty rather than failing the whole read), because a partial answer that
// still shows most of the router is better than an all-or-nothing failure.

import (
	"fmt"
	"strconv"
	"strings"
)

// ReadDeviceMonitor implements Adapter.ReadDeviceMonitor for MikroTik.
func (mikrotikAdapter) ReadDeviceMonitor(conn ROSConn) (DeviceMonitorSample, error) {
	var s DeviceMonitorSample

	rows, err := conn.Read("/system/resource/print")
	if err != nil {
		return s, fmt.Errorf("mikrotik: read /system/resource: %w", err)
	}
	if len(rows) == 0 {
		return s, fmt.Errorf("mikrotik: /system/resource returned no row")
	}
	r := rows[0]
	s.Version = r["version"]
	s.BoardName = r["board-name"]
	s.CPUPercent = parsePercent(r["cpu-load"])
	s.CPUCores, _ = strconv.Atoi(r["cpu-count"])
	s.UptimeSecs = parseUptimeSecs(r["uptime"])
	if total, free := parseFloat(r["total-memory"]), parseFloat(r["free-memory"]); total > 0 {
		s.MemPercent = (total - free) / total * 100
	}

	if idRows, err := conn.Read("/system/identity/print"); err == nil && len(idRows) > 0 {
		s.Identity = idRows[0]["name"]
	}
	// CHR and other non-hardware builds have no /system/routerboard at all —
	// that degrades to an empty serial, not an error.
	if rbRows, err := conn.Read("/system/routerboard/print"); err == nil && len(rbRows) > 0 {
		s.SerialNumber = rbRows[0]["serial-number"]
	}

	// Active session count: PPP + Hotspot, whichever services exist on this
	// router (a hotspot-only or PPPoE-only box degrades the missing half to
	// zero rather than erroring the whole sample).
	if pppRows, err := conn.Read("/ppp/active/print"); err == nil {
		s.SessionCount += len(pppRows)
	}
	if hsRows, err := conn.Read("/ip/hotspot/active/print"); err == nil {
		s.SessionCount += len(hsRows)
	}

	return s, nil
}

// ReadInterfaces implements Adapter.ReadInterfaces for MikroTik.
func (mikrotikAdapter) ReadInterfaces(conn ROSConn) ([]InterfaceState, error) {
	rows, err := conn.Read("/interface/print", "=stats=")
	if err != nil {
		// Some ROS6 builds reject the =stats= flag on /interface/print; fall
		// back to the plain read so interfaces still show even without
		// byte counters (the worker's traffic diff simply stays at zero).
		rows, err = conn.Read("/interface/print")
		if err != nil {
			return nil, fmt.Errorf("mikrotik: read /interface: %w", err)
		}
	}
	out := make([]InterfaceState, 0, len(rows))
	for _, r := range rows {
		mtu, _ := strconv.Atoi(r["mtu"])
		out = append(out, InterfaceState{
			Name:     r["name"],
			Type:     r["type"],
			Running:  isTrue(r["running"]),
			Disabled: isTrue(r["disabled"]),
			MAC:      r["mac-address"],
			MTU:      mtu,
			Comment:  r["comment"],
			RxBytes:  parseInt64(r["rx-byte"]),
			TxBytes:  parseInt64(r["tx-byte"]),
		})
	}
	return out, nil
}

// ReadGeneral implements Adapter.ReadGeneral for MikroTik.
func (mikrotikAdapter) ReadGeneral(conn ROSConn) (GeneralInfo, error) {
	var g GeneralInfo

	if idRows, err := conn.Read("/system/identity/print"); err == nil && len(idRows) > 0 {
		g.Identity = idRows[0]["name"]
	}
	if clockRows, err := conn.Read("/system/clock/print"); err == nil && len(clockRows) > 0 {
		g.TimeZone = clockRows[0]["time-zone-name"]
		g.Time = clockRows[0]["time"]
		g.Date = clockRows[0]["date"]
	}
	if ntpRows, err := conn.Read("/system/ntp/client/print"); err == nil && len(ntpRows) > 0 {
		r := ntpRows[0]
		g.NTPEnabled = isTrue(r["enabled"])
		g.NTPMode = r["mode"]
		if servers := r["servers"]; servers != "" {
			g.NTPServers = splitCommaList(servers)
		} else if p := r["primary-ntp"]; p != "" && p != "0.0.0.0" {
			g.NTPServers = append(g.NTPServers, p)
			if s := r["secondary-ntp"]; s != "" && s != "0.0.0.0" {
				g.NTPServers = append(g.NTPServers, s)
			}
		}
	}
	return g, nil
}

// ReadBridges implements Adapter.ReadBridges for MikroTik.
func (mikrotikAdapter) ReadBridges(conn ROSConn) ([]BridgeInfo, error) {
	rows, err := conn.Read("/interface/bridge/print")
	if err != nil {
		// No /interface/bridge at all reads as "none configured", not an error.
		return nil, nil
	}
	byName := make(map[string]*BridgeInfo, len(rows))
	out := make([]BridgeInfo, 0, len(rows))
	for _, r := range rows {
		b := BridgeInfo{Name: r["name"], Comment: r["comment"]}
		out = append(out, b)
		byName[b.Name] = &out[len(out)-1]
	}
	if len(out) == 0 {
		return out, nil
	}
	portRows, err := conn.Read("/interface/bridge/port/print")
	if err != nil {
		return out, nil // ports are enrichment; a read failure still shows the bridges
	}
	for _, r := range portRows {
		b, ok := byName[r["bridge"]]
		if !ok {
			continue
		}
		pvid, _ := strconv.Atoi(r["pvid"])
		b.Ports = append(b.Ports, BridgePort{Interface: r["interface"], PVID: pvid})
	}
	return out, nil
}

// ReadVLANs implements Adapter.ReadVLANs for MikroTik.
func (mikrotikAdapter) ReadVLANs(conn ROSConn) ([]VLANInfo, error) {
	rows, err := conn.Read("/interface/vlan/print")
	if err != nil {
		return nil, nil
	}
	out := make([]VLANInfo, 0, len(rows))
	for _, r := range rows {
		vid, _ := strconv.Atoi(r["vlan-id"])
		out = append(out, VLANInfo{
			Name: r["name"], VLANID: vid, Interface: r["interface"], Comment: r["comment"],
		})
	}
	return out, nil
}

// ReadIPTab implements Adapter.ReadIPTab for MikroTik: static addresses, DHCP
// client, DHCP servers, and the live lease table (FR-143 — "worth building
// carefully: it is the operator's answer to which device is that").
func (mikrotikAdapter) ReadIPTab(conn ROSConn) (IPTabSnapshot, error) {
	var snap IPTabSnapshot

	if rows, err := conn.Read("/ip/address/print"); err == nil {
		for _, r := range rows {
			snap.Addresses = append(snap.Addresses, IPAddressInfo{
				Address: r["address"], Interface: r["interface"],
				Comment: r["comment"], Disabled: isTrue(r["disabled"]),
			})
		}
	}
	if rows, err := conn.Read("/ip/dhcp-client/print"); err == nil {
		for _, r := range rows {
			snap.DHCPClients = append(snap.DHCPClients, DHCPClientInfo{
				Interface: r["interface"], Status: r["status"],
				Address: r["address"], Gateway: r["gateway"],
			})
		}
	}
	if rows, err := conn.Read("/ip/dhcp-server/print"); err == nil {
		for _, r := range rows {
			snap.DHCPServers = append(snap.DHCPServers, DHCPServerInfo{
				Name: r["name"], Interface: r["interface"],
				PoolName: r["address-pool"], Disabled: isTrue(r["disabled"]),
			})
		}
	}
	if rows, err := conn.Read("/ip/dhcp-server/lease/print"); err == nil {
		for _, r := range rows {
			status := r["status"]
			if status == "" {
				status = "unknown"
			}
			snap.Leases = append(snap.Leases, DHCPLease{
				Address: r["address"], MAC: r["mac-address"], Hostname: r["host-name"],
				Server: r["server"], Status: status, ExpiresIn: r["expires-after"],
			})
		}
	}
	return snap, nil
}

// ReadDNS implements Adapter.ReadDNS for MikroTik.
func (mikrotikAdapter) ReadDNS(conn ROSConn) (DNSInfo, error) {
	var d DNSInfo
	if rows, err := conn.Read("/ip/dns/print"); err == nil && len(rows) > 0 {
		r := rows[0]
		d.Servers = splitCommaList(r["servers"])
		d.AllowRemoteRequests = isTrue(r["allow-remote-requests"])
		d.CacheSize, _ = strconv.Atoi(strings.TrimSuffix(r["cache-size"], "KiB"))
		d.CacheUsed, _ = strconv.Atoi(strings.TrimSuffix(r["cache-used"], "KiB"))
	}
	if rows, err := conn.Read("/ip/dns/static/print"); err == nil {
		for _, r := range rows {
			d.Static = append(d.Static, DNSStaticEntry{
				Name: r["name"], Address: r["address"], Comment: r["comment"],
			})
		}
	}
	return d, nil
}

// Reboot implements Adapter.Reboot for MikroTik. The single write this phase
// performs — every caller is double-confirmed, permission-gated and audited
// before reaching here (device_api.go). RouterOS's /system/reboot is a
// fire-and-forget command: the connection is expected to drop, so a transport
// error immediately after issuing it is treated as success, not failure.
func (mikrotikAdapter) Reboot(conn ROSConn) error {
	_, err := conn.Write("/system/reboot")
	if err != nil && !looksLikeConnectionDrop(err) {
		return fmt.Errorf("mikrotik: reboot: %w", err)
	}
	return nil
}

// Ping implements Adapter.Ping for MikroTik. /ping is a query command
// (structurally a read — it returns one reply sentence per echo, not a
// "!done" write ack), so it goes through conn.Read like every inspection call
// above; it is gated as an action only because of what it DOES on the wire.
func (mikrotikAdapter) Ping(conn ROSConn, target string, count int) (PingResult, error) {
	if count <= 0 {
		count = 4
	}
	if count > 10 {
		count = 10 // bounded — this is a diagnostic click, not a flood tool
	}
	rows, err := conn.Read("/ping", "=address="+target, "=count="+strconv.Itoa(count))
	if err != nil {
		return PingResult{}, fmt.Errorf("mikrotik: ping %s: %w", target, err)
	}
	var res PingResult
	var minRtt, maxRtt, sumRtt float64
	first := true
	for _, r := range rows {
		res.Sent++
		if r["status"] != "" {
			continue // timeout/unreachable row — no rtt to fold in
		}
		res.Received++
		rtt := parseRttMs(r["time"])
		if first || rtt < minRtt {
			minRtt = rtt
		}
		if first || rtt > maxRtt {
			maxRtt = rtt
		}
		sumRtt += rtt
		first = false
	}
	if res.Sent > 0 {
		res.PacketLossPercent = float64(res.Sent-res.Received) / float64(res.Sent) * 100
	}
	if res.Received > 0 {
		res.MinRttMs, res.MaxRttMs, res.AvgRttMs = minRtt, maxRtt, sumRtt/float64(res.Received)
	}
	return res, nil
}

// --- small parsing helpers --------------------------------------------------

func parsePercent(s string) float64 {
	return parseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"))
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// parseUptimeSecs turns RouterOS's "3w2d4h5m6s" uptime rendering into whole
// seconds. Best-effort like parseSecs in mikrotik_config.go — an unparseable
// value yields 0 rather than erroring; this is a display field.
func parseUptimeSecs(s string) int64 {
	var total int64
	var n int64
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			n = n*10 + int64(c-'0')
		case c == 'w':
			total += n * 7 * 24 * 3600
			n = 0
		case c == 'd':
			total += n * 24 * 3600
			n = 0
		case c == 'h':
			total += n * 3600
			n = 0
		case c == 'm':
			total += n * 60
			n = 0
		case c == 's':
			total += n
			n = 0
		default:
			n = 0
		}
	}
	return total
}

// parseRttMs turns RouterOS's ping "time" field ("1ms234us" or "500us" or
// "12ms") into milliseconds.
func parseRttMs(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "ms") {
		return parseFloat(strings.TrimSuffix(s, "ms"))
	}
	if strings.HasSuffix(s, "us") {
		return parseFloat(strings.TrimSuffix(s, "us")) / 1000
	}
	if strings.HasSuffix(s, "s") {
		return parseFloat(strings.TrimSuffix(s, "s")) * 1000
	}
	return parseFloat(s)
}

func splitCommaList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// looksLikeConnectionDrop reports whether err is the kind of transport error
// expected right after a device reboots itself mid-response.
func looksLikeConnectionDrop(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "closed") ||
		strings.Contains(msg, "reset") || strings.Contains(msg, "broken pipe")
}
