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
	"errors"
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
	)
	err := p.db.QueryRow(ctx,
		`SELECT s.id::text, s.password_enc, s.status, s.expires_at,
		        p.expiry_behavior, p.quota_behavior, p.rate_down_kbps, p.rate_up_kbps,
		        p.throttle_rate, p.session_limit_default,
		        p.hotspot_rate_down_kbps, p.hotspot_rate_up_kbps,
		        s.session_limit_override, s.rate_override, s.mac_lock_mode, s.learned_mac,
		        host(s.static_ip), s.allow_hotspot,
		        (SELECT name FROM ip_pools WHERE id = p.pool_id),
		        (SELECT name FROM ip_pools WHERE purpose = 'expired' ORDER BY name LIMIT 1)
		   FROM subscribers s
		   LEFT JOIN profiles p ON p.id = s.profile_id
		  WHERE s.username = $1`, username).Scan(
		&v.SubscriberID, &v.PasswordEnc, &v.Status, &expiresAt,
		&expiryBeh, &quotaBeh, &rateDown, &rateUp,
		&throttle, &sessDefault, &hsDown, &hsUp,
		&sessOverride, &rateOverride, &v.MacLockMode, &learnedMac,
		&staticIP, &v.AllowHotspot, &poolName, &expiredPool)
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
