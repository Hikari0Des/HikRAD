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
	// names maps an instance id to the name an operator recognises it by — the
	// label they typed, else the router's own server name. Kept beside Services
	// rather than inside vendor.ServiceInstance because a display name is not
	// something an adapter resolves on; it exists so the live view can say WHICH
	// hotspot zone a session is on, not just "hotspot".
	names map[string]string
}

// serviceName is the display name for an instance id, "" when unknown.
func (n nasInfo) serviceName(id string) string { return n.names[id] }

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
			svcs, names, serr := r.servicesOf(ctx, id)
			if serr != nil {
				// Transient DB error: don't cache a NAS with no instances — that
				// would pin every session on it to the pppoe fallback for the
				// whole TTL.
				return info
			}
			info = nasInfo{ID: id, Vendor: vend, Services: svcs, names: names}
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

func (r *nasResolver) servicesOf(ctx context.Context, nasID string) ([]vendor.ServiceInstance, map[string]string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id::text, service, ros_server_name, label FROM nas_services
		  WHERE nas_id = $1::uuid AND enabled ORDER BY service DESC, label, id`, nasID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []vendor.ServiceInstance
	names := map[string]string{}
	for rows.Next() {
		var s vendor.ServiceInstance
		var label string
		if err := rows.Scan(&s.ID, &s.Service, &s.ROSServerName, &label); err != nil {
			return nil, nil, err
		}
		out = append(out, s)
		// The operator's label first; the router's own name is the fallback they
		// would still recognise.
		if label != "" {
			names[s.ID] = label
		} else {
			names[s.ID] = s.ROSServerName
		}
	}
	return out, names, rows.Err()
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
//     attributes say, so the session is that instance's.
//  3. A NAS whose enabled instances are ALL THE SAME KIND: we may not know
//     which one, but the kind is not in doubt, so record it unattributed rather
//     than fall through to a guess. This is what a hotspot-only NAS running
//     several zones looks like, and without it every one of its sessions was
//     labelled pppoe in the panel.
//  4. The coarse hint, unattributed — and pppoe only as the last resort, which
//     is what a pre-v2 record and a NAS with no instances imply.
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
	if kind, ok := n.soleKind(); ok {
		return kind, ""
	}
	if hint := rec.coarseService(); hint != "" {
		return hint, ""
	}
	return livestate.ServicePPPoE, ""
}

// soleKind reports the service kind when every enabled instance on the NAS is
// the same one — a hotspot-only or PPPoE-only router, whatever its zone count.
func (n nasInfo) soleKind() (string, bool) {
	if len(n.Services) == 0 {
		return "", false
	}
	kind := n.Services[0].Service
	for _, s := range n.Services[1:] {
		if s.Service != kind {
			return "", false
		}
	}
	return kind, true
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
