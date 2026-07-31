package radius

// IP pool storage and utilization math (FR-16). Pools return Framed-Pool at
// auth; utilization % is live sessions in the pool (C6, injected) over the
// pool's total address count, with an exhaustion flag at 90% (FR-16.3; the
// alert wiring is Phase 3).

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const poolExhaustionThreshold = 0.90

// Pool ownership modes (FR-108, Decision 48). Mirrors nas_services'
// management_mode vocabulary so the two read identically.
const (
	// poolModeSystem: HikRAD owns the pool — editable, deletable, pushable to a
	// router. The column default, so every pre-v3-5 pool keeps today's behavior.
	poolModeSystem = "system"
	// poolModeRouter: adopted from a router — a read-only reference HikRAD
	// never writes back.
	poolModeRouter = "router"
)

type poolRow struct {
	ID   string
	Name string
	// ManagementMode (FR-108) is the router-ownership sync principle applied to
	// pools (Decision 48), mirroring nas_services.management_mode:
	//   "system" — HikRAD owns this pool: editable, deletable, pushable to a
	//              router's /ip pool. The default, so every pre-v3-5 pool keeps
	//              behaving exactly as it did.
	//   "router" — adopted from a router: a READ-ONLY reference. HikRAD never
	//              writes its ranges back, and edit/delete/push are refused.
	ManagementMode string
	Ranges         []string
	// RouterRanges is the router's raw ranges string for an ADOPTED pool
	// (e.g. "10.0.0.2-10.0.0.254"), recorded verbatim because RouterOS range
	// syntax is not valid inet. Empty for HikRAD-owned pools.
	RouterRanges string
	Purpose      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// poolView is the JSON read shape (C7-B) including utilization.
type poolView struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Ranges         []string  `json:"ranges"`
	Purpose        string    `json:"purpose"`
	ManagementMode string    `json:"management_mode"`
	RouterRanges   string    `json:"router_ranges,omitempty"`
	Size           int64     `json:"size"`
	Used           int       `json:"used"`
	UtilPercent    float64   `json:"util_percent"`
	Exhausted      bool      `json:"exhausted"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (p poolRow) view(used int) poolView {
	size := poolSize(p.Ranges)
	util := 0.0
	if size > 0 {
		util = math.Round(float64(used)/float64(size)*10000) / 100 // 2 dp
	}
	return poolView{
		ID: p.ID, Name: p.Name, Ranges: p.Ranges, Purpose: p.Purpose,
		ManagementMode: p.ManagementMode, RouterRanges: p.RouterRanges,
		Size: size, Used: used, UtilPercent: util,
		Exhausted: size > 0 && float64(used)/float64(size) >= poolExhaustionThreshold,
		CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}

const poolColumns = `id::text, name, ranges::text[], purpose, management_mode, COALESCE(router_ranges, ''), created_at, updated_at`

func scanPool(row pgx.Row) (poolRow, error) {
	var p poolRow
	err := row.Scan(&p.ID, &p.Name, &p.Ranges, &p.Purpose, &p.ManagementMode, &p.RouterRanges, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func listPools(ctx context.Context, db *pgxpool.Pool) ([]poolRow, error) {
	rows, err := db.Query(ctx, `SELECT `+poolColumns+` FROM ip_pools ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []poolRow
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func getPool(ctx context.Context, db *pgxpool.Pool, id string) (poolRow, error) {
	return scanPool(db.QueryRow(ctx, `SELECT `+poolColumns+` FROM ip_pools WHERE id = $1`, id))
}

// insertPool creates a HikRAD-owned pool. Anything created through the normal
// CRUD surface is 'system' by definition — only adoptPool inserts a read-only
// 'router' row (FR-108).
func insertPool(ctx context.Context, db *pgxpool.Pool, name string, ranges []string, purpose string) (poolRow, error) {
	return scanPool(db.QueryRow(ctx,
		`INSERT INTO ip_pools (name, ranges, purpose, management_mode)
		 VALUES ($1, $2::inet[], $3, 'system')
		 RETURNING `+poolColumns,
		name, ranges, purpose))
}

// adoptPool records a pool that already exists on a router as a READ-ONLY
// reference (FR-108 / Decision 48): HikRAD stores the name+ranges it observed
// so operators can see and select it, but never writes them back. Adopting is
// idempotent by name — re-running discovery refreshes an adopted pool's
// observed ranges but never touches a 'system' pool the operator owns.
func adoptPool(ctx context.Context, db *pgxpool.Pool, name, routerRanges string) (poolRow, bool, error) {
	row, err := scanPool(db.QueryRow(ctx,
		`INSERT INTO ip_pools (name, ranges, purpose, management_mode, router_ranges)
		 VALUES ($1, '{}'::inet[], 'active', 'router', $2)
		 ON CONFLICT (name) DO UPDATE
		    SET router_ranges = EXCLUDED.router_ranges, updated_at = now()
		  WHERE ip_pools.management_mode = 'router'
		 RETURNING `+poolColumns,
		name, routerRanges))
	if errors.Is(err, pgx.ErrNoRows) {
		// The name exists but is HikRAD-owned ('system'): the ON CONFLICT WHERE
		// clause deliberately skipped it. Never silently convert an owned pool
		// into a router-managed one.
		return poolRow{}, false, nil
	}
	if err != nil {
		return poolRow{}, false, err
	}
	return row, true, nil
}

func updatePool(ctx context.Context, db *pgxpool.Pool, id, name string, ranges []string, purpose string) (poolRow, error) {
	return scanPool(db.QueryRow(ctx,
		`UPDATE ip_pools SET name = $2, ranges = $3::inet[], purpose = $4 WHERE id = $1
		 RETURNING `+poolColumns,
		id, name, ranges, purpose))
}

func deletePool(ctx context.Context, db *pgxpool.Pool, id string) error {
	ct, err := db.Exec(ctx, `DELETE FROM ip_pools WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// poolSize is the total number of addresses across a pool's ranges. A bare IP
// counts as one; a CIDR counts its whole block. IPv6 blocks are capped so a
// /64 doesn't overflow — utilization on IPv6 pools is effectively always ~0%,
// which is acceptable for v1 (MikroTik pools are IPv4 in practice).
func poolSize(ranges []string) int64 {
	var total int64
	for _, r := range ranges {
		total += rangeSize(r)
		if total < 0 { // overflow guard
			return math.MaxInt64
		}
	}
	return total
}

func rangeSize(r string) int64 {
	if pfx, err := netip.ParsePrefix(r); err == nil {
		bits := pfx.Addr().BitLen()
		hostBits := bits - pfx.Bits()
		if hostBits <= 0 {
			return 1
		}
		if hostBits >= 62 { // avoid overflow; treat as effectively unbounded
			return math.MaxInt64
		}
		return int64(1) << uint(hostBits)
	}
	if _, err := netip.ParseAddr(r); err == nil {
		return 1
	}
	return 0
}

// validateRanges parses each range as an IP or CIDR, returning a normalized
// list suitable for an inet[] insert, or an error naming the bad value.
func validateRanges(ranges []string) ([]string, error) {
	if len(ranges) == 0 {
		return nil, fmt.Errorf("at least one range is required")
	}
	out := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if _, _, err := net.ParseCIDR(r); err == nil {
			out = append(out, r)
			continue
		}
		if ip := net.ParseIP(r); ip != nil {
			out = append(out, r)
			continue
		}
		return nil, fmt.Errorf("%q is not a valid IP or CIDR", r)
	}
	return out, nil
}

// staticIPInPool reports whether ip falls within any of the pool's ranges —
// the membership half of the static-IP uniqueness validation D calls (FR-16.2).
func staticIPInPool(ip string, ranges []string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, r := range ranges {
		if pfx, err := netip.ParsePrefix(r); err == nil {
			if pfx.Contains(addr) {
				return true
			}
			continue
		}
		if a, err := netip.ParseAddr(r); err == nil && a == addr {
			return true
		}
	}
	return false
}
