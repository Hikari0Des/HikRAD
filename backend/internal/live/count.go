package live

// Package-level live-state access (contract C6). The Redis/DB handles are set
// once at module Register; the radius authorize path calls Count/NASCount
// through the seams it exposes (radius.SetLiveCounter etc.), and the SSE/List/
// history handlers use the same handles. A nil handle degrades to empty/zero so
// nothing panics before wiring.

import (
	"context"
	"sort"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	pkgRDB *redis.Client
	pkgDB  *pgxpool.Pool
)

func setHandles(rdb *redis.Client, db *pgxpool.Pool) {
	pkgRDB = rdb
	pkgDB = db
}

// Count returns the number of live sessions for a subscriber, optionally scoped
// to one service (C6, FR-58.2). service "" = pppoe + hotspot. This is the seam
// B's session-limit check consumes; it is O(1) (SCARD).
func Count(subscriberID, service string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return livestate.Count(ctx, pkgRDB, subscriberID, service)
}

// NASCount returns the number of live sessions on a NAS (delete-confirmation).
func NASCount(nasID string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return livestate.NASCount(ctx, pkgRDB, nasID)
}

// List returns the live sessions passing filter f and the caller's scope,
// sorted by start time (newest first). It resolves subscriber attributes for
// profile/manager filtering in one batched query.
func List(ctx context.Context, f Filter, scope *auth.ManagerScope) ([]livestate.State, error) {
	all, err := livestate.All(ctx, pkgRDB)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	seen := map[string]struct{}{}
	for _, s := range all {
		if s.SubscriberID == "" {
			continue
		}
		if _, ok := seen[s.SubscriberID]; ok {
			continue
		}
		seen[s.SubscriberID] = struct{}{}
		ids = append(ids, s.SubscriberID)
	}
	attrs := resolveSubjects(ctx, pkgDB, ids)

	out := make([]livestate.State, 0, len(all))
	for _, s := range all {
		if matchState(s, f, scope, attrs[s.SubscriberID]) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}
