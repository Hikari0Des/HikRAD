package radius

// NAS auto-discovery (FR-56.1): passive MikroTik Neighbor Discovery (MNDP,
// UDP 5678) plus an operator-triggered IP-range scan that checks whether the
// RouterOS API port is open. STRICTLY READ-ONLY: MNDP is listen-only and the
// scan is a bare TCP reachability probe that opens and immediately closes the
// connection — it never logs into or sends anything to a router (the API
// auto-setup with preview/apply is Phase 4, sub-PRD 02 FR-56.2–56.4). Results
// pre-fill the FR-14 wizard, deduplicated against registered NAS.

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	mndpPort        = 5678
	rosAPIPort      = 8728
	discoverMaxCIDR = 1024 // refuse to scan a range larger than /22-ish
)

// discovered is one candidate router (C7-B response shape).
type discovered struct {
	IP                string `json:"ip"`
	Identity          string `json:"identity"`
	ROSVersion        string `json:"ros_version"`
	MAC               string `json:"mac"`
	AlreadyRegistered bool   `json:"already_registered"`
}

type discoverRequest struct {
	// MNDPWaitMs is how long to listen for MNDP announcements (default 3000,
	// capped at 10000).
	MNDPWaitMs int `json:"mndp_wait_ms"`
	// ScanCIDR, when set, is additionally TCP-probed on the RouterOS API port.
	ScanCIDR string `json:"scan_cidr"`
}

func (m *module) discoverHandler(w http.ResponseWriter, r *http.Request) {
	var req discoverRequest
	// Body is optional; ignore an empty body.
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &req) {
		return
	}
	wait := time.Duration(req.MNDPWaitMs) * time.Millisecond
	if wait <= 0 {
		wait = 3 * time.Second
	}
	if wait > 10*time.Second {
		wait = 10 * time.Second
	}

	ctx := r.Context()
	byIP := map[string]*discovered{}

	// Passive MNDP listen (best-effort; a bind failure just yields no neighbors).
	for _, n := range listenMNDP(ctx, wait, m.log) {
		d := &discovered{IP: n.IP, Identity: n.Identity, ROSVersion: n.Version, MAC: n.MAC}
		byIP[n.IP] = d
	}

	// Optional active range scan (port-open probe only).
	if req.ScanCIDR != "" {
		ips, err := scanRange(ctx, req.ScanCIDR)
		if err != nil {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(),
				httpapi.FieldError{Field: "scan_cidr", Message: err.Error()})
			return
		}
		for _, ip := range ips {
			if _, ok := byIP[ip]; !ok {
				byIP[ip] = &discovered{IP: ip}
			}
		}
	}

	// Dedup against the registry.
	registered, err := registeredIPs(ctx, m.db)
	if err != nil {
		m.internal(w, "list nas for discovery dedup", err)
		return
	}
	out := make([]discovered, 0, len(byIP))
	for ip, d := range byIP {
		d.AlreadyRegistered = registered[canonicalIP(ip)]
		out = append(out, *d)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// registeredIPs is the set of already-registered NAS IPs (canonicalized), for
// deduping discovery results.
func registeredIPs(ctx context.Context, db *pgxpool.Pool) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT host(ip) FROM nas`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		out[canonicalIP(ip)] = true
	}
	return out, rows.Err()
}

// mndpNeighbor is a parsed MNDP announcement.
type mndpNeighbor struct {
	IP       string
	Identity string
	Version  string
	Platform string
	MAC      string
}

// listenMNDP binds UDP :5678 and collects announcements for wait. Read-only.
func listenMNDP(ctx context.Context, wait time.Duration, log logger) []mndpNeighbor {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{Port: mndpPort})
	if err != nil {
		log.Warn("radius: MNDP listen failed (discovery continues without it)", "error", err)
		return nil
	}
	defer pc.Close()

	deadline := time.Now().Add(wait)
	_ = pc.SetReadDeadline(deadline)
	seen := map[string]mndpNeighbor{}
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			break
		}
		n, src, rerr := pc.ReadFromUDP(buf)
		if rerr != nil {
			break // deadline or socket closed
		}
		nb, ok := parseMNDP(buf[:n])
		if !ok {
			continue
		}
		if nb.IP == "" && src != nil {
			nb.IP = src.IP.String()
		}
		if nb.IP != "" {
			seen[nb.IP] = nb
		}
	}
	out := make([]mndpNeighbor, 0, len(seen))
	for _, nb := range seen {
		out = append(out, nb)
	}
	return out
}

// parseMNDP decodes the MikroTik Neighbor Discovery TLV payload. Layout: a
// 4-byte header followed by TLVs of {u16 type, u16 length, value}. We read the
// fields the wizard pre-fills; unknown TLVs are skipped.
func parseMNDP(b []byte) (mndpNeighbor, bool) {
	if len(b) < 4 {
		return mndpNeighbor{}, false
	}
	var nb mndpNeighbor
	p := 4 // skip header
	for p+4 <= len(b) {
		typ := binary.BigEndian.Uint16(b[p : p+2])
		length := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		p += 4
		if p+length > len(b) {
			break
		}
		val := b[p : p+length]
		p += length
		switch typ {
		case 1: // MAC address
			if length == 6 {
				nb.MAC = net.HardwareAddr(val).String()
			}
		case 5: // identity
			nb.Identity = string(val)
		case 7: // version
			nb.Version = string(val)
		case 8: // platform
			nb.Platform = string(val)
		}
	}
	if nb.Identity == "" && nb.MAC == "" {
		return mndpNeighbor{}, false
	}
	return nb, true
}

// scanRange TCP-probes the RouterOS API port across a CIDR, returning the
// hosts that accept a connection. It opens then immediately closes each socket
// — no RouterOS API traffic is ever sent (read-only discovery contract).
func scanRange(ctx context.Context, cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("scan_cidr %q is not a valid CIDR", cidr)
	}
	if !prefix.Addr().Is4() {
		return nil, fmt.Errorf("scan_cidr must be IPv4")
	}
	// Reject an oversized range by its prefix length BEFORE enumerating hosts,
	// so a /8 can never allocate millions of strings.
	if hostBits := 32 - prefix.Bits(); hostBits >= 11 { // > 2046 hosts
		return nil, fmt.Errorf("scan_cidr is too large; use a /%d or smaller", 32-10)
	}
	hosts := hostsInPrefix(prefix)
	if len(hosts) == 0 {
		return nil, fmt.Errorf("scan_cidr covers no scannable hosts")
	}
	if len(hosts) > discoverMaxCIDR {
		return nil, fmt.Errorf("scan_cidr is too large (%d hosts, max %d)", len(hosts), discoverMaxCIDR)
	}

	// Bounded concurrency so a /22 doesn't open thousands of sockets at once.
	const workers = 64
	jobs := make(chan string)
	results := make(chan string, len(hosts))
	var dialer net.Dialer
	worker := func() {
		for ip := range jobs {
			cctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			conn, derr := dialer.DialContext(cctx, "tcp", net.JoinHostPort(ip, itoa(rosAPIPort)))
			cancel()
			if derr == nil {
				_ = conn.Close()
				results <- ip
			}
		}
	}
	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() { worker(); done <- struct{}{} }()
	}
	go func() {
		for _, ip := range hosts {
			select {
			case jobs <- ip:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()
	for i := 0; i < workers; i++ {
		<-done
	}
	close(results)
	var open []string
	for ip := range results {
		open = append(open, ip)
	}
	return open, nil
}

// hostsInPrefix enumerates the usable host addresses of an IPv4 prefix
// (excluding network/broadcast for a prefix shorter than /31).
func hostsInPrefix(p netip.Prefix) []string {
	if !p.Addr().Is4() {
		return nil
	}
	p = p.Masked()
	var out []string
	addr := p.Addr()
	for p.Contains(addr) {
		out = append(out, addr.String())
		next := addr.Next()
		if !next.IsValid() {
			break
		}
		addr = next
	}
	// Trim network/broadcast for prefixes with host room.
	if p.Bits() <= 30 && len(out) >= 2 {
		out = out[1 : len(out)-1]
	}
	return out
}

// logger is the minimal slog surface listenMNDP needs, so tests can pass a
// discard logger without the real *slog.Logger plumbing.
type logger interface {
	Warn(msg string, args ...any)
}
