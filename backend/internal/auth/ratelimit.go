package auth

// Login protection (FR-28.2): per-account and per-IP failure counting with
// progressive lockout. Counters live in Redis behind a tiny store interface so
// the policy is unit-testable without a broker (fakeStore in tests) and so a
// missing Redis degrades to "no limiting" rather than a panic.
//
// Anti-lockout design (task edge case): account and IP locks are separate and
// always time-limited (never permanent); an admin can clear an account lock
// immediately via unlockAccount. So an attacker cannot permanently lock out an
// admin — the lock self-expires and is admin-clearable.

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	accountFailLimit = 5
	accountLockTTL   = 15 * time.Minute
	ipFailLimit      = 20
	ipLockTTL        = 15 * time.Minute
	failWindow       = 15 * time.Minute
)

// store is the minimal Redis surface the limiter needs.
type store interface {
	incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	set(ctx context.Context, key string, ttl time.Duration) error
	ttl(ctx context.Context, key string) (time.Duration, error)
	del(ctx context.Context, keys ...string) error
}

type loginLimiter struct {
	s store
}

func newLoginLimiter(rdb *redis.Client) *loginLimiter {
	if rdb == nil {
		return &loginLimiter{s: nil} // disabled; allowed() short-circuits
	}
	return &loginLimiter{s: redisStore{rdb: rdb}}
}

func acctFailKey(u string) string { return "auth:fail:acct:" + u }
func acctLockKey(u string) string { return "auth:lock:acct:" + u }
func ipFailKey(ip string) string  { return "auth:fail:ip:" + ip }
func ipLockKey(ip string) string  { return "auth:lock:ip:" + ip }

// lockState reports whether login is currently blocked and, if so, for how
// long. A store error fails open (allowed) so a Redis hiccup never bars all
// logins.
func (l *loginLimiter) lockState(ctx context.Context, username, ip string) (locked bool, retryAfter time.Duration) {
	if l == nil || l.s == nil {
		return false, 0
	}
	for _, key := range []string{acctLockKey(username), ipLockKey(ip)} {
		if d, err := l.s.ttl(ctx, key); err == nil && d > 0 {
			if d > retryAfter {
				retryAfter = d
			}
			locked = true
		}
	}
	return locked, retryAfter
}

// recordFailure increments both counters and engages a lock when a threshold
// is crossed. Returns whether the account is now locked.
func (l *loginLimiter) recordFailure(ctx context.Context, username, ip string) bool {
	if l == nil || l.s == nil {
		return false
	}
	locked := false
	if n, err := l.s.incr(ctx, acctFailKey(username), failWindow); err == nil && n >= accountFailLimit {
		_ = l.s.set(ctx, acctLockKey(username), accountLockTTL)
		locked = true
	}
	if n, err := l.s.incr(ctx, ipFailKey(ip), failWindow); err == nil && n >= ipFailLimit {
		_ = l.s.set(ctx, ipLockKey(ip), ipLockTTL)
	}
	return locked
}

// reset clears an account's failure/lock state on a successful login (the IP
// counter is left to age out so a shared-IP attacker isn't reset by one
// legitimate login).
func (l *loginLimiter) reset(ctx context.Context, username string) {
	if l == nil || l.s == nil {
		return
	}
	_ = l.s.del(ctx, acctFailKey(username), acctLockKey(username))
}

// unlockAccount is the admin unlock (FR-28.2): clears an account's lock and
// failure count immediately.
func (l *loginLimiter) unlockAccount(ctx context.Context, username string) error {
	if l == nil || l.s == nil {
		return nil
	}
	return l.s.del(ctx, acctFailKey(username), acctLockKey(username))
}

// redisStore is the production store.
type redisStore struct{ rdb *redis.Client }

func (s redisStore) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = s.rdb.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

func (s redisStore) set(ctx context.Context, key string, ttl time.Duration) error {
	return s.rdb.Set(ctx, key, "1", ttl).Err()
}

func (s redisStore) ttl(ctx context.Context, key string) (time.Duration, error) {
	d, err := s.rdb.TTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// redis returns -2 (no key) / -1 (no expiry) as negative durations.
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

func (s redisStore) del(ctx context.Context, keys ...string) error {
	return s.rdb.Del(ctx, keys...).Err()
}
