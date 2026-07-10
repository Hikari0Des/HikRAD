package auth

import (
	"context"
	"testing"
	"time"
)

// fakeStore is an in-memory store for deterministic lockout-policy tests.
type fakeStore struct {
	counts map[string]int64
	locks  map[string]time.Duration
}

func newFakeStore() *fakeStore {
	return &fakeStore{counts: map[string]int64{}, locks: map[string]time.Duration{}}
}

func (f *fakeStore) incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	f.counts[key]++
	return f.counts[key], nil
}
func (f *fakeStore) set(_ context.Context, key string, ttl time.Duration) error {
	f.locks[key] = ttl
	return nil
}
func (f *fakeStore) ttl(_ context.Context, key string) (time.Duration, error) {
	return f.locks[key], nil
}
func (f *fakeStore) del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(f.counts, k)
		delete(f.locks, k)
	}
	return nil
}

func TestAccountLockoutAfterThreshold(t *testing.T) {
	ctx := context.Background()
	l := &loginLimiter{s: newFakeStore()}

	for i := 0; i < accountFailLimit-1; i++ {
		if locked := l.recordFailure(ctx, "sara", "1.1.1.1"); locked {
			t.Fatalf("locked too early at attempt %d", i+1)
		}
	}
	if locked, _ := l.lockState(ctx, "sara", "1.1.1.1"); locked {
		t.Fatal("should not be locked before threshold")
	}
	// Threshold-crossing failure locks.
	if locked := l.recordFailure(ctx, "sara", "1.1.1.1"); !locked {
		t.Fatal("expected account lock at threshold")
	}
	locked, retry := l.lockState(ctx, "sara", "1.1.1.1")
	if !locked || retry != accountLockTTL {
		t.Fatalf("lockState = %v %v", locked, retry)
	}

	// A successful login resets the account.
	l.reset(ctx, "sara")
	if locked, _ := l.lockState(ctx, "sara", "1.1.1.1"); locked {
		t.Fatal("reset should clear the account lock")
	}
}

func TestAdminUnlockClearsAccount(t *testing.T) {
	ctx := context.Background()
	l := &loginLimiter{s: newFakeStore()}
	for i := 0; i < accountFailLimit; i++ {
		l.recordFailure(ctx, "sara", "1.1.1.1")
	}
	if locked, _ := l.lockState(ctx, "sara", "1.1.1.1"); !locked {
		t.Fatal("expected locked")
	}
	if err := l.unlockAccount(ctx, "sara"); err != nil {
		t.Fatal(err)
	}
	if locked, _ := l.lockState(ctx, "sara", "1.1.1.1"); locked {
		t.Fatal("admin unlock should clear the lock")
	}
}

// Per-IP lock is independent of the per-account lock (anti-lockout separation).
func TestIPLockIndependentOfAccount(t *testing.T) {
	ctx := context.Background()
	l := &loginLimiter{s: newFakeStore()}
	// Distinct usernames so no single account hits its threshold, but the
	// shared IP accrues failures.
	for i := 0; i < ipFailLimit; i++ {
		l.recordFailure(ctx, "user"+string(rune('a'+i)), "9.9.9.9")
	}
	// A brand-new account from the locked IP is still blocked.
	if locked, _ := l.lockState(ctx, "fresh", "9.9.9.9"); !locked {
		t.Fatal("expected IP lock to block a fresh account from that IP")
	}
	// A different IP is unaffected.
	if locked, _ := l.lockState(ctx, "fresh", "10.10.10.10"); locked {
		t.Fatal("other IPs must not be locked")
	}
}

func TestNilLimiterIsNoop(t *testing.T) {
	ctx := context.Background()
	l := newLoginLimiter(nil) // no redis
	if locked, _ := l.lockState(ctx, "x", "y"); locked {
		t.Fatal("nil-backed limiter must never report locked")
	}
	if l.recordFailure(ctx, "x", "y") {
		t.Fatal("nil-backed limiter must never lock")
	}
	l.reset(ctx, "x")
	if err := l.unlockAccount(ctx, "x"); err != nil {
		t.Fatal(err)
	}
}
