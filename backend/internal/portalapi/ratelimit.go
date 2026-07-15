package portalapi

// Portal login rate limiting (NFR-4.6). internal/auth's limiter is a private
// implementation detail of that package (read-only ownership per the task
// brief — not a symbol this package may import), so this is a small,
// self-contained limiter using the identical policy/thresholds so both
// surfaces enforce the same NFR-4.6 posture independently.

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	acctFailLimit = 5
	acctLockTTL   = 15 * time.Minute
	ipFailLimit   = 20
	ipLockTTL     = 15 * time.Minute
	failWindow    = 15 * time.Minute
)

type loginLimiter struct{ rdb *redis.Client }

func newLoginLimiter(rdb *redis.Client) *loginLimiter { return &loginLimiter{rdb: rdb} }

func acctFailKey(u string) string { return "portal:auth:fail:acct:" + u }
func acctLockKey(u string) string { return "portal:auth:lock:acct:" + u }
func ipFailKey(ip string) string  { return "portal:auth:fail:ip:" + ip }
func ipLockKey(ip string) string  { return "portal:auth:lock:ip:" + ip }

func (l *loginLimiter) lockState(ctx context.Context, username, ip string) (locked bool, retryAfter time.Duration) {
	if l == nil || l.rdb == nil {
		return false, 0
	}
	for _, key := range []string{acctLockKey(username), ipLockKey(ip)} {
		d, err := l.rdb.TTL(ctx, key).Result()
		if err != nil || d < 0 {
			continue
		}
		locked = true
		if d > retryAfter {
			retryAfter = d
		}
	}
	return locked, retryAfter
}

func (l *loginLimiter) recordFailure(ctx context.Context, username, ip string) {
	if l == nil || l.rdb == nil {
		return
	}
	if n, err := l.incr(ctx, acctFailKey(username), failWindow); err == nil && n >= acctFailLimit {
		_ = l.rdb.Set(ctx, acctLockKey(username), "1", acctLockTTL).Err()
	}
	if n, err := l.incr(ctx, ipFailKey(ip), failWindow); err == nil && n >= ipFailLimit {
		_ = l.rdb.Set(ctx, ipLockKey(ip), "1", ipLockTTL).Err()
	}
}

func (l *loginLimiter) reset(ctx context.Context, username string) {
	if l == nil || l.rdb == nil {
		return
	}
	_ = l.rdb.Del(ctx, acctFailKey(username), acctLockKey(username)).Err()
}

func (l *loginLimiter) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = l.rdb.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}
