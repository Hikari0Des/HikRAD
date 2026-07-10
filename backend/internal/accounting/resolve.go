package accounting

// Read-only lookups the consumer needs from other agents' tables (contract:
// "Consumes C1-B nas table read-only; subscriber id by username"). NAS records
// change rarely, so they are cached briefly to keep the consumer off the DB on
// the hot path; subscriber ids are resolved per record (correctness over a
// micro-optimization — a renamed/rebound user must map immediately).

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nasInfo is the slice of B's nas row the pipeline needs.
type nasInfo struct {
	ID   string // zeroUUID when the source IP is not a registered NAS
	Type string // 'pppoe' | 'hotspot' → sessions.service (FR-58)
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

// byIP resolves a source IP to its NAS id + service type. An unregistered IP
// maps to the zeroUUID sentinel with the default 'pppoe' service, so the session
// is still recorded (orphan tolerance) and the dedup/upsert keys stay concrete.
func (r *nasResolver) byIP(ctx context.Context, ip string) nasInfo {
	now := time.Now()
	r.mu.Lock()
	if e, ok := r.m[ip]; ok && now.Sub(e.at) < r.ttl {
		r.mu.Unlock()
		return e.info
	}
	r.mu.Unlock()

	info := nasInfo{ID: zeroUUID, Type: "pppoe"}
	if r.db != nil && ip != "" {
		var id, typ string
		err := r.db.QueryRow(ctx, `SELECT id::text, type FROM nas WHERE ip = $1::inet`, ip).Scan(&id, &typ)
		if err == nil {
			info = nasInfo{ID: id, Type: typ}
		} else if err != pgx.ErrNoRows {
			// Transient DB error: don't cache a wrong answer, just return the
			// sentinel for this record.
			return info
		}
	}
	r.mu.Lock()
	r.m[ip] = nasEntry{info: info, at: now}
	r.mu.Unlock()
	return info
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
