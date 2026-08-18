package subscribers

// AuthView read-model (contract C4). This is the single-query policy loader B's
// authorize engine consumes on every Access-Request cache miss. The caching
// layer + `auth:view:<username>` key + InvalidatePolicy all live in B's engine
// (internal/radius/authview.go); D's job is the fast cold loader plus LearnMac.
// So the ~1 ms "hot path" is B's cache hit (this is not called); the cold path
// is this one join. LearnMac is write-through by invalidation: it persists the
// MAC and drops B's cached view so the next auth re-reads it.
//
// Pool-name resolution reads B's ip_pools table directly in SQL (there is no
// exported name-by-id helper); that is read-only and necessary to populate
// PoolName / ExpiredPoolName, which nothing else in the view carries.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// quotaKeyPrefix mirrors radius/authview.go and accounting/quota.go (contract C8).
const quotaKeyPrefix = "quota:exhausted:"

// policyProvider implements radius.PolicyProvider.
type policyProvider struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

// GetAuthView loads the policy view for username, or radius.ErrNoSubscriber.
// The query joins subscriber → profile and resolves the active + expired pool
// names; overrides win over profile defaults (FR-7).
func (p *policyProvider) GetAuthView(ctx context.Context, username string) (radius.AuthView, error) {
	var (
		v            radius.AuthView
		expiresAt    *time.Time
		expiryBeh    *string
		quotaBeh     *string
		rateDown     *int
		rateUp       *int
		throttle     *string
		sessDefault  *int
		hsDown       *int
		hsUp         *int
		sessOverride *int
		rateOverride *string
		learnedMac   *string
		staticIP     *string
		poolName     *string
		expiredPool  *string
		// Burst/priority segments (FR-11): abstract rx/tx intents D populates from
		// the profile's 0203 burst columns; B's vendor adapter renders the VSAs.
		burstRate    *string
		burstThresh  *string
		burstTime    *string
		ratePriority *string
		minRate      *string
		// FR-109: named address-list, subscriber override else profile.
		addrListOverride *string
		addrListProfile  *string
		// FR-64 effective NAS scope, as the jsonb array the query below builds
		// (empty array = any NAS).
		scopesJSON []byte
		// v4-4 Limits engine (FR-128–131): the four exhaust_action/target
		// pairs (expiry/quota reuse their existing behavior column as the
		// action — see migration 0597), the two new limit ceilings, and the
		// speed-window set, all raw off profiles/profile_speed_windows.
		expiryExhaustTarget *string
		quotaExhaustTarget  *string
		uptimeLimitSeconds  *int64
		uptimeExhaustAction *string
		uptimeExhaustTarget *string
		dailyLimitBytes     *int64
		dailyResetMin       *int
		dailyExhaustAction  *string
		dailyExhaustTarget  *string
		speedWindowsJSON    []byte
	)
	err := p.db.QueryRow(ctx,
		`SELECT s.id::text, s.password_enc, s.status, s.expires_at,
		        p.expiry_behavior, p.quota_behavior, p.rate_down_kbps, p.rate_up_kbps,
		        p.throttle_rate, p.session_limit_default,
		        p.hotspot_rate_down_kbps, p.hotspot_rate_up_kbps,
		        s.session_limit_override, s.rate_override, s.mac_lock_mode, s.learned_mac,
		        host(s.static_ip), s.service_type,
		        (SELECT name FROM ip_pools WHERE id = p.pool_id),
		        (SELECT name FROM ip_pools WHERE purpose = 'expired' ORDER BY name LIMIT 1),
		        p.burst_rate, p.burst_threshold, p.burst_time, p.rate_priority, p.min_rate,
		        s.address_list_override, p.address_list,
		        -- v4-4 Limits engine (FR-128-131): expiry/quota reuse their
		        -- existing behavior column as the action (0597); the target
		        -- columns are new for all four limits.
		        p.expiry_exhaust_target, p.quota_exhaust_target,
		        p.uptime_limit_seconds, p.uptime_exhaust_action, p.uptime_exhaust_target,
		        p.daily_limit_bytes, p.daily_reset_min, p.daily_exhaust_action, p.daily_exhaust_target,
		        COALESCE(
		          (SELECT jsonb_agg(jsonb_build_object(
		                    'id', w.id::text, 'days_mask', w.days_mask,
		                    'start_min', w.start_min, 'end_min', w.end_min,
		                    'rate_down_kbps', w.rate_down_kbps, 'rate_up_kbps', w.rate_up_kbps))
		             FROM profile_speed_windows w WHERE w.profile_id = p.id AND w.enabled),
		          '[]'::jsonb),
		        -- FR-64 effective scope, subscriber-over-profile taken WHOLE: a
		        -- subscriber with ANY scope row of their own uses only their own
		        -- set, never a union with the profile's. jsonb_agg is NULL (not an
		        -- empty array) when a side has no rows, which is exactly what makes
		        -- this COALESCE chain express "the subscriber's set if they have
		        -- one, else the profile's, else any NAS".
		        COALESCE(
		          (SELECT jsonb_agg(jsonb_build_object(
		                    'nas_id', sc.nas_id::text,
		                    'nas_service_id', COALESCE(sc.nas_service_id::text, '')))
		             FROM subscriber_nas_scopes sc WHERE sc.subscriber_id = s.id),
		          (SELECT jsonb_agg(jsonb_build_object(
		                    'nas_id', pc.nas_id::text,
		                    'nas_service_id', COALESCE(pc.nas_service_id::text, '')))
		             FROM profile_nas_scopes pc WHERE pc.profile_id = p.id),
		          '[]'::jsonb)
		   FROM subscribers s
		   LEFT JOIN profiles p ON p.id = s.profile_id
		  WHERE s.username = $1`, username).Scan(
		&v.SubscriberID, &v.PasswordEnc, &v.Status, &expiresAt,
		&expiryBeh, &quotaBeh, &rateDown, &rateUp,
		&throttle, &sessDefault, &hsDown, &hsUp,
		&sessOverride, &rateOverride, &v.MacLockMode, &learnedMac,
		&staticIP, &v.ServiceType, &poolName, &expiredPool,
		&burstRate, &burstThresh, &burstTime, &ratePriority, &minRate,
		&addrListOverride, &addrListProfile,
		&expiryExhaustTarget, &quotaExhaustTarget,
		&uptimeLimitSeconds, &uptimeExhaustAction, &uptimeExhaustTarget,
		&dailyLimitBytes, &dailyResetMin, &dailyExhaustAction, &dailyExhaustTarget,
		&speedWindowsJSON,
		&scopesJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return radius.AuthView{}, radius.ErrNoSubscriber
	}
	if err != nil {
		return radius.AuthView{}, err
	}

	if expiresAt != nil {
		v.ExpiresAt = expiresAt.UTC()
	}
	v.ExpiryBehavior = strOr(expiryBeh, "block")
	v.QuotaBehavior = strOr(quotaBeh, "block")
	v.ThrottleRate = strOr(throttle, "")
	v.PoolName = strOr(poolName, "")
	v.ExpiredPoolName = strOr(expiredPool, "")
	v.LearnedMac = strOr(learnedMac, "")
	v.StaticIP = strOr(staticIP, "")
	v.BurstRate = strOr(burstRate, "")
	v.BurstThreshold = strOr(burstThresh, "")
	v.BurstTime = strOr(burstTime, "")
	v.RatePriority = strOr(ratePriority, "")
	v.MinRate = strOr(minRate, "")
	// FR-109: subscriber override wins, else the profile's address-list, else empty.
	if addrListOverride != nil && *addrListOverride != "" {
		v.AddressList = *addrListOverride
	} else {
		v.AddressList = strOr(addrListProfile, "")
	}

	// v4-4 Limits engine (FR-128–131). Interpret each limit's exhaust_target
	// by its own action (limits.go's resolveExhaustTarget); switch_plan is
	// the only branch that touches the DB again, and only when configured.
	v.UptimeLimitSeconds = int64Or(uptimeLimitSeconds, 0)
	v.UptimeExhaustAction = strOr(uptimeExhaustAction, "block")
	v.DailyLimitBytes = int64Or(dailyLimitBytes, 0)
	v.DailyResetMin = intOr(dailyResetMin, 0)
	v.DailyExhaustAction = strOr(dailyExhaustAction, "throttle")

	if v.ExpiryThrottleRate, v.ExpiryPoolOverride, v.ExpirySwitchRate, v.ExpirySwitchPool, err =
		resolveExhaustTarget(ctx, p.db, v.ExpiryBehavior, strOr(expiryExhaustTarget, "")); err != nil {
		return radius.AuthView{}, err
	}
	// Quota's throttle_rate keeps its pre-v4-4 meaning (p.throttle_rate,
	// already scanned into v.ThrottleRate above); exhaust_target is only
	// consulted for the two actions that are new to quota's semantics here.
	var quotaThrottleUnused string
	if quotaThrottleUnused, v.QuotaPoolOverride, v.QuotaSwitchRate, v.QuotaSwitchPool, err =
		resolveExhaustTarget(ctx, p.db, v.QuotaBehavior, strOr(quotaExhaustTarget, "")); err != nil {
		return radius.AuthView{}, err
	}
	_ = quotaThrottleUnused
	if v.UptimeThrottleRate, v.UptimePoolOverride, v.UptimeSwitchRate, v.UptimeSwitchPool, err =
		resolveExhaustTarget(ctx, p.db, v.UptimeExhaustAction, strOr(uptimeExhaustTarget, "")); err != nil {
		return radius.AuthView{}, err
	}
	if v.DailyThrottleRate, v.DailyPoolOverride, v.DailySwitchRate, v.DailySwitchPool, err =
		resolveExhaustTarget(ctx, p.db, v.DailyExhaustAction, strOr(dailyExhaustTarget, "")); err != nil {
		return radius.AuthView{}, err
	}

	// FR-128/FR-129: derive live usage only when a limit is actually
	// configured — the overwhelming majority of profiles set neither, and
	// skipping both the counters upsert and the sum queries keeps them on
	// the pre-v4-4 query cost.
	if v.UptimeLimitSeconds > 0 || v.DailyLimitBytes > 0 {
		uptimeAnchor, dailyStart, cerr := ensureCounters(ctx, p.db, v.SubscriberID)
		if cerr != nil {
			return radius.AuthView{}, cerr
		}
		if v.UptimeLimitSeconds > 0 {
			if v.UptimeUsedSeconds, err = sumUptimeSeconds(ctx, p.db, v.SubscriberID, uptimeAnchor); err != nil {
				return radius.AuthView{}, err
			}
		}
		if v.DailyLimitBytes > 0 {
			if v.DailyUsedBytes, err = sumDailyBytes(ctx, p.db, v.SubscriberID, dailyStart); err != nil {
				return radius.AuthView{}, err
			}
		}
	}

	// FR-131: speed windows, rendered through the same rateString() the base
	// rate uses (one rendering path).
	if len(speedWindowsJSON) > 0 {
		var rows []speedWindowRow
		if err := json.Unmarshal(speedWindowsJSON, &rows); err != nil {
			return radius.AuthView{}, fmt.Errorf("subscribers: decode speed windows for %q: %w", username, err)
		}
		v.SpeedWindows = make([]radius.SpeedWindow, 0, len(rows))
		for _, r := range rows {
			v.SpeedWindows = append(v.SpeedWindows, radius.SpeedWindow{
				ID:       r.ID,
				DaysMask: r.DaysMask,
				StartMin: r.StartMin,
				EndMin:   r.EndMin,
				Rate:     rateString(r.RateUpKbps, r.RateDownKbps),
			})
		}
	}
	// A scope set that fails to decode must not silently become "any NAS" — that
	// would widen an operator's deliberate restriction on a corrupt read. Fail
	// the load instead; the engine turns the error into a 500 and FreeRADIUS
	// rejects, which is the safe direction for a scoped account.
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &v.Scopes); err != nil {
			return radius.AuthView{}, fmt.Errorf("subscribers: decode nas scopes for %q: %w", username, err)
		}
	}

	// Session limit: per-user override else profile default else 1.
	switch {
	case sessOverride != nil:
		v.SessionLimit = *sessOverride
	case sessDefault != nil:
		v.SessionLimit = *sessDefault
	default:
		v.SessionLimit = 1
	}

	// Rate: per-user override string wins; otherwise render the profile kbps.
	if rateOverride != nil && *rateOverride != "" {
		v.RateLimit = *rateOverride
	} else {
		v.RateLimit = rateString(intOr(rateUp, 0), intOr(rateDown, 0))
	}
	// Hotspot rate: profile's hotspot kbps if set, else empty (B falls back to
	// RateLimit) — FR-58.1.
	if hsDown != nil || hsUp != nil {
		v.HotspotRateLimit = rateString(intOr(hsUp, 0), intOr(hsDown, 0))
	}

	// Quota-exhausted flag (C8). B also overlays this on every hit; reading it
	// here keeps the cold path correct too. Best-effort.
	if p.rdb != nil && v.SubscriberID != "" {
		if b, err := p.rdb.Get(ctx, quotaKeyPrefix+v.SubscriberID).Bool(); err == nil {
			v.QuotaExhausted = b
		}
	}
	return v, nil
}

// LearnMac records the first MAC seen for a learn-mode subscriber (FR-5.2) and
// invalidates B's cached view so the just-learned MAC takes effect immediately.
func (p *policyProvider) LearnMac(ctx context.Context, subscriberID, mac string) error {
	ct, err := p.db.Exec(ctx,
		`UPDATE subscribers SET learned_mac = $2
		  WHERE id = $1::uuid AND mac_lock_mode = 'learn' AND learned_mac IS NULL`,
		subscriberID, mac)
	if err != nil {
		return err
	}
	if ct.RowsAffected() > 0 {
		_ = radius.InvalidatePolicy(subscriberID)
	}
	return nil
}

// rateString renders a MikroTik-Rate-Limit "rx/tx" pair from up/down kbps.
// MikroTik's rx-rate limits the client's upload and tx-rate its download, so the
// abstract intent value is upload-first, download-second (the vendor adapter
// passes it through unchanged). Returns "" when both are zero.
func rateString(upKbps, downKbps int) string {
	if upKbps <= 0 && downKbps <= 0 {
		return ""
	}
	return rateToken(upKbps) + "/" + rateToken(downKbps)
}

// rateToken renders one side. Profiles store speeds as multiples of 1024 kbps
// ("10M" == 10240 kbps in the seed), so a clean multiple renders as "<n>M";
// anything else falls back to "<n>k". A non-positive value renders "0".
func rateToken(kbps int) string {
	if kbps <= 0 {
		return "0"
	}
	if kbps%1024 == 0 {
		return strconv.Itoa(kbps/1024) + "M"
	}
	return strconv.Itoa(kbps) + "k"
}

func strOr(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

func intOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

func int64Or(p *int64, d int64) int64 {
	if p == nil {
		return d
	}
	return *p
}
