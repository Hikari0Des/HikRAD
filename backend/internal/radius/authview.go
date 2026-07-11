package radius

// Auth read-model seam (contract C4). D owns the subscriber/profile data and
// exposes it to B's authorize path through PolicyProvider; C exposes live
// session counts through the LiveCounter seam. Both are *injected* rather than
// imported: D's subscribers package calls radius.InvalidatePolicy on every
// mutation, so radius importing subscribers would be an import cycle. The
// concrete providers are wired at boot (D/C module init) via SetPolicyProvider
// / SetLiveCounter; until then the engine degrades safely (unknown_user / zero
// live sessions) so this package builds and unit-tests standalone.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNoSubscriber is what a PolicyProvider returns from GetAuthView when no
// subscriber matches the username; the engine maps it to reason unknown_user.
var ErrNoSubscriber = errors.New("radius: no such subscriber")

// AuthView is the cached policy read-model for one subscriber (contract C4,
// amended 2026-07-09 for FR-58). PasswordEnc is the AES-GCM-sealed RADIUS
// password (platform/crypto envelope) — decrypted only in the authorize path
// (NFR-4.2). All rate strings are abstract "rx/tx" intents, not VSAs.
type AuthView struct {
	SubscriberID     string    `json:"subscriber_id"`
	PasswordEnc      []byte    `json:"password_enc"`
	Status           string    `json:"status"`          // active | disabled | expired
	ExpiresAt        time.Time `json:"expires_at"`      // zero = no expiry
	ExpiryBehavior   string    `json:"expiry_behavior"` // block | expired_pool
	QuotaBehavior    string    `json:"quota_behavior"`  // block | throttle | expired_pool
	QuotaExhausted   bool      `json:"quota_exhausted"`
	RateLimit        string    `json:"rate_limit"`
	PoolName         string    `json:"pool_name"`
	ExpiredPoolName  string    `json:"expired_pool_name"`
	SessionLimit     int       `json:"session_limit"`
	MacLockMode      string    `json:"mac_lock_mode"` // off | learn | fixed
	LearnedMac       string    `json:"learned_mac"`
	ThrottleRate     string    `json:"throttle_rate"`
	AllowHotspot     bool      `json:"allow_hotspot"`      // FR-58
	HotspotRateLimit string    `json:"hotspot_rate_limit"` // empty = fall back to RateLimit
	// StaticIP is the subscriber's fixed Framed-IP-Address (FR-16.2), empty
	// when the subscriber uses a pool. NOTE: this field is NOT in the frozen
	// C4 AuthView struct but the engine provably needs it — the FR-16.2 /
	// static-IP-precedence edge case requires emitting Framed-IP-Address, and
	// nothing else in AuthView carries the address. Flagged as a required C4
	// amendment for D to populate; defaults empty so it is backward-compatible.
	StaticIP string `json:"static_ip"`
	// Burst rate fields (FR-11): abstract "rx/tx" pairs D populates from the
	// profile's burst columns (0200s). They apply only to the normal/full-speed
	// reply; the vendor adapter renders them into the concrete rate string
	// (ComposeRate) so no burst syntax leaks into this package (FR-17). All
	// default empty → the base RateLimit is emitted unchanged (backward-compatible
	// C4 extension, mirrors StaticIP above).
	BurstRate      string `json:"burst_rate"`
	BurstThreshold string `json:"burst_threshold"`
	BurstTime      string `json:"burst_time"`
	RatePriority   string `json:"rate_priority"`
	MinRate        string `json:"min_rate"`
}

// PolicyProvider is D's C4 read-model, injected into the authorize path.
type PolicyProvider interface {
	// GetAuthView returns the policy view for username, or ErrNoSubscriber.
	GetAuthView(ctx context.Context, username string) (AuthView, error)
	// LearnMac records the first MAC seen for a learn-mode subscriber (FR-5).
	LearnMac(ctx context.Context, subscriberID, mac string) error
}

var (
	seamMu         sync.RWMutex
	provider       PolicyProvider
	liveCountFn    = func(subscriberID, service string) int { return 0 }
	nasLiveCountFn = func(nasID string) int { return 0 }
	poolUsageFn    = func(poolName string) int { return 0 }
)

// SetPolicyProvider installs D's read-model. Called once from the subscribers
// module at boot.
func SetPolicyProvider(p PolicyProvider) {
	seamMu.Lock()
	provider = p
	seamMu.Unlock()
}

// SetLiveCounter installs C's live-session counter (C6 live.Count). service ""
// counts all services; "pppoe"/"hotspot" count that service only (FR-58.2).
func SetLiveCounter(count func(subscriberID, service string) int) {
	seamMu.Lock()
	liveCountFn = count
	seamMu.Unlock()
}

// SetNASLiveCounter installs C's per-NAS live-session count (from live.List
// filtered by nas_id), used by the delete-with-live-sessions confirmation
// (FR-13.4). Unset → 0, so a delete is never blocked before C wires it.
func SetNASLiveCounter(count func(nasID string) int) {
	seamMu.Lock()
	nasLiveCountFn = count
	seamMu.Unlock()
}

func currentNASLiveCount() func(string) int {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return nasLiveCountFn
}

// SetPoolUsageCounter installs C's per-pool live-session count (from live.List
// bucketed by pool), used for the FR-16.3 utilization %. Unset → 0 (0% util).
func SetPoolUsageCounter(count func(poolName string) int) {
	seamMu.Lock()
	poolUsageFn = count
	seamMu.Unlock()
}

func currentPoolUsage() func(string) int {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return poolUsageFn
}

func currentProvider() PolicyProvider {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return provider
}

func currentLiveCount() func(string, string) int {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return liveCountFn
}

// --- Redis-cached read-model (contract C4) ---------------------------------
//
// Primary key auth:view:<username> holds the JSON AuthView; auth:view:sub:<id>
// is a reverse index so InvalidatePolicy(subscriberID) — the frozen B-exposed
// hook D calls on every mutation — can find and delete the username-keyed entry
// without B ever querying D's tables. The fast-changing quota-exhausted bit is
// overlaid live from C's frozen key (C8) on every hit so a cache TTL never
// serves a stale quota decision.
const (
	viewKeyPrefix  = "auth:view:"
	viewIdxPrefix  = "auth:view:sub:"
	quotaKeyPrefix = "quota:exhausted:" // C writes, D/B read (C8)
	viewCacheTTL   = 30 * time.Second
)

func viewKey(username string) string { return viewKeyPrefix + username }
func viewIdxKey(subID string) string { return viewIdxPrefix + subID }

// resolveView returns the AuthView for username, from cache when possible and
// otherwise from the provider (then cached). found is false when the provider
// reports ErrNoSubscriber. A nil rdb (unit tests, Redis outage) transparently
// bypasses the cache — the DB fallback path the NFR-1 budget is benchmarked
// against.
func (e *engine) resolveView(ctx context.Context, username string) (view AuthView, found bool, err error) {
	if e.rdb != nil {
		if raw, cerr := e.rdb.Get(ctx, viewKey(username)).Bytes(); cerr == nil {
			if json.Unmarshal(raw, &view) == nil {
				e.overlayQuota(ctx, &view)
				return view, true, nil
			}
		}
	}
	p := currentProvider()
	if p == nil {
		// D not wired yet: cannot authenticate anyone. Degrade to unknown_user.
		return AuthView{}, false, nil
	}
	view, err = p.GetAuthView(ctx, username)
	if errors.Is(err, ErrNoSubscriber) {
		return AuthView{}, false, nil
	}
	if err != nil {
		return AuthView{}, false, err
	}
	e.cacheView(ctx, username, view)
	e.overlayQuota(ctx, &view)
	return view, true, nil
}

func (e *engine) cacheView(ctx context.Context, username string, view AuthView) {
	if e.rdb == nil {
		return
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return
	}
	// Best-effort; a cache write failure just means the next authorize re-reads
	// the provider.
	pipe := e.rdb.Pipeline()
	pipe.Set(ctx, viewKey(username), raw, viewCacheTTL)
	if view.SubscriberID != "" {
		pipe.Set(ctx, viewIdxKey(view.SubscriberID), username, viewCacheTTL)
	}
	_, _ = pipe.Exec(ctx)
}

// overlayQuota refreshes the quota-exhausted bit from C's live key so a cached
// view never under-reports exhaustion (C8). Absent key / no Redis: leave the
// provider's value untouched.
func (e *engine) overlayQuota(ctx context.Context, view *AuthView) {
	if e.rdb == nil || view.SubscriberID == "" {
		return
	}
	v, err := e.rdb.Get(ctx, quotaKeyPrefix+view.SubscriberID).Bool()
	if err == nil && v {
		view.QuotaExhausted = true
	}
}

// InvalidatePolicy deletes the cached AuthView for a subscriber (contract C4,
// B-exposed). D calls it on every subscriber/profile mutation that could
// change an auth decision. Safe to call with a subscriber that was never
// cached. Uses the package default engine's Redis client, wired at boot.
func InvalidatePolicy(subscriberID string) error {
	e := defaultEngine()
	if e == nil || e.rdb == nil || subscriberID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	username, err := e.rdb.Get(ctx, viewIdxKey(subscriberID)).Result()
	if errors.Is(err, redis.Nil) {
		// Nothing cached for this subscriber; nothing to do.
		return nil
	}
	if err != nil {
		return err
	}
	return e.rdb.Del(ctx, viewKey(username), viewIdxKey(subscriberID)).Err()
}
