package accounting

// Read-only lookups the consumer needs from other agents' tables (contract:
// "Consumes C1-B nas table read-only; subscriber id by username"). NAS records
// change rarely, so they are cached briefly to keep the consumer off the DB on
// the hot path; subscriber ids are resolved per record (correctness over a
// micro-optimization — a renamed/rebound user must map immediately).

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nasInfo is the slice of B's nas row the pipeline needs, plus the NAS's
// enabled service instances (FR-62). Until v2 phase 1 a NAS had a single `type`
// column that WAS the session's service; now a NAS runs many instances, so the
// service is resolved per record from the request's own attributes (C7) exactly
// as the authorize path does — otherwise a hotspot session on a mixed NAS would
// be recorded as pppoe and counted against the wrong FR-58.2 allowance.
type nasInfo struct {
	ID       string // zeroUUID when the source IP is not a registered NAS
	Vendor   string
	Services []vendor.ServiceInstance
}

type nasResolver struct {
	db  *pgxpool.Pool
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]nasEntry
}

type nasEntry struct {
	info nasInfo
	at   time.Time
}

func newNASResolver(db *pgxpool.Pool) *nasResolver {
	return &nasResolver{db: db, ttl: 10 * time.Second, m: map[string]nasEntry{}}
}

// byIP resolves a source IP to its NAS id, vendor and enabled service
// instances. An unregistered IP maps to the zeroUUID sentinel with no
// instances, so the session is still recorded (orphan tolerance) and the
// dedup/upsert keys stay concrete.
func (r *nasResolver) byIP(ctx context.Context, ip string) nasInfo {
	now := time.Now()
	r.mu.Lock()
	if e, ok := r.m[ip]; ok && now.Sub(e.at) < r.ttl {
		r.mu.Unlock()
		return e.info
	}
	r.mu.Unlock()

	info := nasInfo{ID: zeroUUID}
	if r.db != nil && ip != "" {
		var id, vend string
		err := r.db.QueryRow(ctx, `SELECT id::text, vendor FROM nas WHERE ip = $1::inet`, ip).Scan(&id, &vend)
		switch {
		case err == nil:
			svcs, serr := r.servicesOf(ctx, id)
			if serr != nil {
				// Transient DB error: don't cache a NAS with no instances — that
				// would pin every session on it to the pppoe fallback for the
				// whole TTL.
				return info
			}
			info = nasInfo{ID: id, Vendor: vend, Services: svcs}
		case errors.Is(err, pgx.ErrNoRows):
			// Unregistered NAS: keep the sentinel and cache it.
		default:
			// Transient DB error: don't cache a wrong answer.
			return info
		}
	}
	r.mu.Lock()
	r.m[ip] = nasEntry{info: info, at: now}
	r.mu.Unlock()
	return info
}

func (r *nasResolver) servicesOf(ctx context.Context, nasID string) ([]vendor.ServiceInstance, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id::text, service, ros_server_name FROM nas_services
		  WHERE nas_id = $1::uuid AND enabled ORDER BY service DESC, label, id`, nasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []vendor.ServiceInstance
	for rows.Next() {
		var s vendor.ServiceInstance
		if err := rows.Scan(&s.ID, &s.Service, &s.ROSServerName); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// resolveService maps an accounting record to one of the NAS's service
// instances through the same vendor seam the authorize path uses (C7 / FR-17),
// so a session is filed under the instance that authorized it.
//
// Returns the coarse service and the instance id ("" when none resolves).
//
// Resolution NEVER drops a record: losing an Accounting-Request is the one
// thing this pipeline must not do (M2), so an unresolvable instance degrades to
// "recorded, unattributed", never to "lost". The fallbacks, in order:
//
//  1. The vendor adapter's answer, when it can identify the instance.
//  2. A NAS running exactly ONE enabled instance: unambiguous whatever the
//     attributes say, so the session is that instance's. This matters because
//     accounting packets carry weaker service hints than Access-Requests (some
//     NASes omit Service-Type on interims entirely) — trusting the coarse guess
//     over an unambiguous fact would file a hotspot-only NAS's sessions as
//     pppoe and break FR-58.2's per-service counting.
//  3. The coarse hint, unattributed.
func (n nasInfo) resolveService(rec Record) (service, serviceID string) {
	if len(n.Services) == 0 {
		return livestate.ServicePPPoE, ""
	}
	if inst, ok := vendor.For(n.Vendor).ResolveService(vendor.ServiceQuery{
		Service:         rec.coarseService(),
		CalledStationID: rec.CalledStationID,
		NASPortType:     rec.NASPortType,
		NASPortID:       rec.NASPortID,
	}, n.Services); ok {
		return inst.Service, inst.ID
	}
	if len(n.Services) == 1 {
		return n.Services[0].Service, n.Services[0].ID
	}
	return rec.coarseService(), ""
}

// subscriberByUsername maps a login to a subscriber id, or "" when none matches
// (voucher / unknown login — the session is still recorded).
func subscriberByUsername(ctx context.Context, db *pgxpool.Pool, username string) string {
	if db == nil || username == "" {
		return ""
	}
	var id string
	err := db.QueryRow(ctx, `SELECT id::text FROM subscribers WHERE username = $1`, username).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}
