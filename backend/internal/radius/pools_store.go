package radius

// IP pool storage and utilization math (FR-16). Pools return Framed-Pool at
// auth; utilization % is live sessions in the pool (C6, injected) over the
// pool's total address count, with an exhaustion flag at 90% (FR-16.3; the
// alert wiring is Phase 3).

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const poolExhaustionThreshold = 0.90

type poolRow struct {
	ID        string
	Name      string
	Ranges    []string
	Purpose   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// poolView is the JSON read shape (C7-B) including utilization.
type poolView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Ranges      []string  `json:"ranges"`
	Purpose     string    `json:"purpose"`
	Size        int64     `json:"size"`
	Used        int       `json:"used"`
	UtilPercent float64   `json:"util_percent"`
	Exhausted   bool      `json:"exhausted"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (p poolRow) view(used int) poolView {
	size := poolSize(p.Ranges)
	util := 0.0
	if size > 0 {
		util = math.Round(float64(used)/float64(size)*10000) / 100 // 2 dp
	}
	return poolView{
		ID: p.ID, Name: p.Name, Ranges: p.Ranges, Purpose: p.Purpose,
		Size: size, Used: used, UtilPercent: util,
		Exhausted: size > 0 && float64(used)/float64(size) >= poolExhaustionThreshold,
		CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}

const poolColumns = `id::text, name, ranges::text[], purpose, created_at, updated_at`

func scanPool(row pgx.Row) (poolRow, error) {
	var p poolRow
	err := row.Scan(&p.ID, &p.Name, &p.Ranges, &p.Purpose, &p.CreatedAt, &p.UpdatedAt)
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

func insertPool(ctx context.Context, db *pgxpool.Pool, name string, ranges []string, purpose string) (poolRow, error) {
	return scanPool(db.QueryRow(ctx,
		`INSERT INTO ip_pools (name, ranges, purpose) VALUES ($1, $2::inet[], $3)
		 RETURNING `+poolColumns,
		name, ranges, purpose))
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
