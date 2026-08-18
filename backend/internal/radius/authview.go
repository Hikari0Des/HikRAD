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

// NASScope is one place an account may authenticate (FR-64 / C4). An empty
// ServiceID means the whole NAS — every service instance on it.
type NASScope struct {
	NASID     string `json:"nas_id"`
	ServiceID string `json:"nas_service_id"`
}

// scopeAllows reports whether an account carrying these scopes may authenticate
// on this NAS through this service instance.
//
// An EMPTY set means any NAS: that is v1's behaviour, the column default, and
// what a view built before scoping existed decodes to — so the empty case must
// allow, never deny. A non-empty set allows if ANY row matches.
func scopeAllows(scopes []NASScope, nasID, serviceID string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s.NASID != nasID {
			continue
		}
		if s.ServiceID == "" || s.ServiceID == serviceID {
			return true
		}
	}
	return false
}

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
	HotspotRateLimit string    `json:"hotspot_rate_limit"` // empty = fall back to RateLimit
	// ServiceType (FR-61, C5) is pppoe | hotspot | dual. It replaced v1's
	// AllowHotspot bool: 'dual' is exactly the old allow_hotspot=true (FR-58
	// semantics, unchanged), 'pppoe' the old false, and 'hotspot' is the new
	// case v1 could not express — a full subscriber with no PPPoE access, whose
	// quota and session limit DO apply (unlike dual's hotspot leg).
	ServiceType string `json:"service_type"`
	// Scopes is the FR-64 effective scope the loader resolves with
	// subscriber-over-profile precedence: every NAS/service the account may
	// authenticate on. EMPTY MEANS ANY NAS (v1's behaviour, and the default for
	// the overwhelming majority of accounts) — it is not "nowhere". The engine
	// rejects nas_not_allowed when a non-empty set matches nothing.
	Scopes []NASScope `json:"scopes"`
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
	// AddressList is the operator-named MikroTik address-list a NORMAL login's
	// IP is placed into (FR-109), resolved subscriber-override-else-profile.
	// Empty ⇒ no address_list intent is emitted. Backward-compatible C4
	// extension, same posture as StaticIP/BurstRate above.
	AddressList string `json:"address_list"`
	// DelegatedPrefix (v4-7, FR-149) is a STATIC per-subscriber IPv6 prefix
	// pin (business accounts), sourced from subscribers.ipv6_static_prefix
	// (migration 0611). Empty ⇒ no delegated_prefix intent is emitted and the
	// subscriber's PPP session gets a prefix dynamically from the resolved
	// service instance's DHCPv6-PD pool instead (a router-native allocation
	// that needs no RADIUS attribute at all — only the STATIC override case
	// requires one). Backward-compatible C4 extension, same posture as
	// StaticIP/AddressList above. NOTE: the loader-side population of this
	// field from subscribers.ipv6_static_prefix lives in the subscribers
	// package (sub-PRD 04) and is a v4-7 follow-up — this field and its
	// policy.go emission are the RADIUS-side half, wired and safe to merge
	// ahead of it (an always-empty field changes no existing reply).
	DelegatedPrefix string `json:"delegated_prefix"`

	// --- v4-4 Limits engine (FR-128–131) ---------------------------------
	//
	// Backward-compatible C4 extension, same posture as StaticIP/BurstRate:
	// every new field defaults to its zero value, which decodes safely from a
	// cached pre-v4-4 JSON blob and means "no limit configured" — so every
	// pre-existing AuthView (including every fixture in authorize_test.go
	// that never sets these) keeps deciding exactly as it did (gate item 12).
	//
	// ExpiryBehavior/QuotaBehavior widen additively (FR-130) to also accept
	// "throttle" (expiry only — quota already had it) and "switch_plan";
	// their existing target fields (ThrottleRate, ExpiredPoolName) keep their
	// exact pre-v4-4 meaning. The fields below are ONLY consulted for the
	// specific action they belong to, resolved once by the loader
	// (subscribers/authview.go) so the RADIUS engine never touches the DB.
	ExpiryThrottleRate string `json:"expiry_throttle_rate"` // ExpiryBehavior == "throttle"
	ExpiryPoolOverride string `json:"expiry_pool_override"` // ExpiryBehavior == "expired_pool"; "" = ExpiredPoolName
	ExpirySwitchRate   string `json:"expiry_switch_rate"`   // ExpiryBehavior == "switch_plan"
	ExpirySwitchPool   string `json:"expiry_switch_pool"`

	QuotaPoolOverride string `json:"quota_pool_override"` // QuotaBehavior == "expired_pool"; "" = ExpiredPoolName
	QuotaSwitchRate   string `json:"quota_switch_rate"`   // QuotaBehavior == "switch_plan"
	QuotaSwitchPool   string `json:"quota_switch_pool"`

	// UptimeLimitSeconds (FR-128): cumulative connected-time ceiling: zero =
	// unlimited (no check). UptimeUsedSeconds is computed live by the loader
	// from subscriber_counters.uptime_cycle_anchor — derived on every load,
	// never stored-and-edited, the same posture accounting/quota.go already
	// uses for quota.
	UptimeLimitSeconds  int64  `json:"uptime_limit_seconds"`
	UptimeUsedSeconds   int64  `json:"uptime_used_seconds"`
	UptimeExhaustAction string `json:"uptime_exhaust_action"` // block | throttle | expired_pool | switch_plan
	UptimeThrottleRate  string `json:"uptime_throttle_rate"`
	UptimePoolOverride  string `json:"uptime_pool_override"`
	UptimeSwitchRate    string `json:"uptime_switch_rate"`
	UptimeSwitchPool    string `json:"uptime_switch_pool"`

	// DailyLimitBytes (FR-129): per-day byte cap, resetting at DailyResetMin
	// (minutes since local midnight, server timezone Asia/Baghdad — matches
	// the FR-11 TOD sweep convention). Zero limit = unlimited. The default
	// exhaustion is THROTTLE, not block — the commercial point of the
	// feature — and it self-restores at the next reset with no operator
	// action (radius/limitsworker.go). DailyUsedBytes is likewise derived
	// live from subscriber_counters.daily_period_start.
	DailyLimitBytes    int64  `json:"daily_limit_bytes"`
	DailyUsedBytes     int64  `json:"daily_used_bytes"`
	DailyResetMin      int    `json:"daily_reset_min"`
	DailyExhaustAction string `json:"daily_exhaust_action"` // block | throttle | expired_pool | switch_plan
	DailyThrottleRate  string `json:"daily_throttle_rate"`
	DailyPoolOverride  string `json:"daily_pool_override"`
	DailySwitchRate    string `json:"daily_switch_rate"`
	DailySwitchPool    string `json:"daily_switch_pool"`

	// SpeedWindows (FR-131): this subscriber's profile's time-of-day rate
	// overrides. A separate mechanism from the older FR-11
	// profile_tod_windows (radius/tod.go), which this phase leaves untouched
	// — see radius/limits.go's package doc for why they don't merge.
	SpeedWindows []SpeedWindow `json:"speed_windows"`
}

// PolicyProvider is D's C4 read-model, injected into the authorize path.
type PolicyProvider interface {
	// GetAuthView returns the policy view for username, or ErrNoSubscriber.
	GetAuthView(ctx context.Context, username string) (AuthView, error)
	// LearnMac records the first MAC seen for a learn-mode subscriber (FR-5).
	LearnMac(ctx context.Context, subscriberID, mac string) error
}

var (
	seamMu              sync.RWMutex
	provider            PolicyProvider
	liveCountFn         = func(subscriberID, service string) int { return 0 }
	nasLiveCountFn      = func(nasID string) int { return 0 }
	serviceLiveCountsFn func() map[string]int
	poolUsageFn         = func(poolName string) int { return 0 }
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

// SetServiceLiveCounts installs C's per-service-instance live counts for the
// FR-63 services sub-list: one call returns every instance's count, keyed by
// nas_services.id. Unset → empty, so the panel renders before C wires it.
//
// It returns the whole map rather than taking a service id (its original shape)
// so the NAS list reads live state ONCE instead of once per service instance —
// the list renders every NAS with all of its services.
func SetServiceLiveCounts(counts func() map[string]int) {
	seamMu.Lock()
	serviceLiveCountsFn = counts
	seamMu.Unlock()
}

func currentServiceLiveCounts() map[string]int {
	seamMu.RLock()
	fn := serviceLiveCountsFn
	seamMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// ServiceLiveCountsWired reports whether the live module has installed the
// counts seam.
//
// It exists so the owning module can TEST that it wired it. An unwired counter
// seam is silent by construction — the unset case deliberately degrades to zero
// so the panel renders before C boots — which means "never wired" and "nobody
// online" are indistinguishable at runtime. That is exactly how this seam
// shipped dead in v2 phase 1.
func ServiceLiveCountsWired() bool {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return serviceLiveCountsFn != nil
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

// InvalidatePolicyByProfile drops the cached AuthView of every subscriber on a
// profile (contract C11 / FR-64.4). A profile carries policy its subscribers
// inherit — including, since v2 phase 1, the FR-64 NAS assignment — so changing
// it must invalidate every view derived from it. Without this a scope change
// would take up to the 30 s cache TTL to bite on some subscribers and not
// others, which looks exactly like a flaky router.
//
// subscriberIDs is supplied by the caller (D owns the subscribers table; this
// package must not query it — that would be the import cycle the seam exists to
// avoid). Errors on individual ids are collected, not fatal: a best-effort miss
// self-heals at the TTL, whereas aborting the loop would leave later ids stale
// indefinitely.
func InvalidatePolicyByProfile(subscriberIDs []string) error {
	var firstErr error
	for _, id := range subscriberIDs {
		if err := InvalidatePolicy(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
