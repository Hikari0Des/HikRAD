package profiles

// Profile persistence + the JSON read shape (C7-D / C1-D). Speeds are stored as
// abstract down/up kbps; vendor VSA rendering is [02]'s adapter job. Quotas and
// behaviors drive [02]'s auth-time policy through D's AuthView read-model.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Profile is the full read/write shape (C7-D). Nullable columns are pointers so
// "unset" (inherit / unlimited) is distinct from a zero value.
type Profile struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	PriceIQD            int64   `json:"price_iqd"`
	DurationDays        int     `json:"duration_days"`
	RateDownKbps        int     `json:"rate_down_kbps"`
	RateUpKbps          int     `json:"rate_up_kbps"`
	PoolID              *string `json:"pool_id"`
	SessionLimitDefault int     `json:"session_limit_default"`
	QuotaMode           string  `json:"quota_mode"`
	QuotaTotalBytes     *int64  `json:"quota_total_bytes"`
	QuotaDownBytes      *int64  `json:"quota_down_bytes"`
	QuotaUpBytes        *int64  `json:"quota_up_bytes"`
	ThrottleRate        *string `json:"throttle_rate"`
	ExpiryBehavior      string  `json:"expiry_behavior"`
	QuotaBehavior       string  `json:"quota_behavior"`
	HotspotRateDownKbps *int    `json:"hotspot_rate_down_kbps"`
	HotspotRateUpKbps   *int    `json:"hotspot_rate_up_kbps"`
	// Burst/priority segments (FR-11): abstract "rx/tx" intents surfaced to B via
	// the AuthView and rendered to VSAs only by B's vendor adapter (FR-17).
	BurstRate      *string `json:"burst_rate"`
	BurstThreshold *string `json:"burst_threshold"`
	BurstTime      *string `json:"burst_time"`
	RatePriority   *string `json:"rate_priority"`
	MinRate        *string `json:"min_rate"`
	// NASID/NASServiceID scope every subscriber on this profile to one NAS /
	// service instance (FR-64), unless the subscriber overrides it. Both nil =
	// any NAS (the v1 default).
	NASID        *string `json:"nas_id"`
	NASServiceID *string `json:"nas_service_id"`
	Archived     bool    `json:"archived"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

const profileColumns = `id::text, name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps,
	pool_id::text, session_limit_default, quota_mode, quota_total_bytes, quota_down_bytes,
	quota_up_bytes, throttle_rate, expiry_behavior, quota_behavior,
	hotspot_rate_down_kbps, hotspot_rate_up_kbps,
	burst_rate, burst_threshold, burst_time, rate_priority, min_rate,
	nas_id::text, nas_service_id::text, archived,
	to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`

func scanProfile(row pgx.Row) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Name, &p.PriceIQD, &p.DurationDays, &p.RateDownKbps, &p.RateUpKbps,
		&p.PoolID, &p.SessionLimitDefault, &p.QuotaMode, &p.QuotaTotalBytes, &p.QuotaDownBytes,
		&p.QuotaUpBytes, &p.ThrottleRate, &p.ExpiryBehavior, &p.QuotaBehavior,
		&p.HotspotRateDownKbps, &p.HotspotRateUpKbps,
		&p.BurstRate, &p.BurstThreshold, &p.BurstTime, &p.RatePriority, &p.MinRate,
		&p.NASID, &p.NASServiceID,
		&p.Archived, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// listProfiles returns profiles ordered by name; includeArchived=false hides
// archived plans from the operator's picker (they stay visible on subscribers
// that already reference them).
func listProfiles(ctx context.Context, db *pgxpool.Pool, includeArchived bool) ([]Profile, error) {
	q := `SELECT ` + profileColumns + ` FROM profiles`
	if !includeArchived {
		q += ` WHERE NOT archived`
	}
	q += ` ORDER BY name, id`
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Profile, 0)
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func getProfile(ctx context.Context, db *pgxpool.Pool, id string) (Profile, error) {
	return scanProfile(db.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = $1::uuid`, id))
}

func insertProfile(ctx context.Context, db *pgxpool.Pool, in profileInput) (Profile, error) {
	return scanProfile(db.QueryRow(ctx,
		`INSERT INTO profiles
		   (name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps, pool_id,
		    session_limit_default, quota_mode, quota_total_bytes, quota_down_bytes,
		    quota_up_bytes, throttle_rate, expiry_behavior, quota_behavior,
		    hotspot_rate_down_kbps, hotspot_rate_up_kbps,
		    burst_rate, burst_threshold, burst_time, rate_priority, min_rate,
		    nas_id, nas_service_id)
		 VALUES ($1,$2,$3,$4,$5,$6::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22::uuid,$23::uuid)
		 RETURNING `+profileColumns,
		in.Name, in.PriceIQD, in.DurationDays, in.RateDownKbps, in.RateUpKbps, in.PoolID,
		in.SessionLimitDefault, in.QuotaMode, in.QuotaTotalBytes, in.QuotaDownBytes,
		in.QuotaUpBytes, in.ThrottleRate, in.ExpiryBehavior, in.QuotaBehavior,
		in.HotspotRateDownKbps, in.HotspotRateUpKbps,
		in.BurstRate, in.BurstThreshold, in.BurstTime, in.RatePriority, in.MinRate,
		in.NASID, in.NASServiceID))
}

func updateProfile(ctx context.Context, db *pgxpool.Pool, id string, in profileInput) (Profile, error) {
	return scanProfile(db.QueryRow(ctx,
		`UPDATE profiles SET
		    name=$2, price_iqd=$3, duration_days=$4, rate_down_kbps=$5, rate_up_kbps=$6,
		    pool_id=$7::uuid, session_limit_default=$8, quota_mode=$9, quota_total_bytes=$10,
		    quota_down_bytes=$11, quota_up_bytes=$12, throttle_rate=$13, expiry_behavior=$14,
		    quota_behavior=$15, hotspot_rate_down_kbps=$16, hotspot_rate_up_kbps=$17,
		    burst_rate=$19, burst_threshold=$20, burst_time=$21, rate_priority=$22, min_rate=$23,
		    nas_id=$24::uuid, nas_service_id=$25::uuid,
		    archived=$18
		  WHERE id=$1::uuid
		 RETURNING `+profileColumns,
		id, in.Name, in.PriceIQD, in.DurationDays, in.RateDownKbps, in.RateUpKbps, in.PoolID,
		in.SessionLimitDefault, in.QuotaMode, in.QuotaTotalBytes, in.QuotaDownBytes,
		in.QuotaUpBytes, in.ThrottleRate, in.ExpiryBehavior, in.QuotaBehavior,
		in.HotspotRateDownKbps, in.HotspotRateUpKbps, in.Archived,
		in.BurstRate, in.BurstThreshold, in.BurstTime, in.RatePriority, in.MinRate,
		in.NASID, in.NASServiceID))
}

func archiveProfile(ctx context.Context, db *pgxpool.Pool, id string) (Profile, error) {
	return scanProfile(db.QueryRow(ctx,
		`UPDATE profiles SET archived = true WHERE id = $1::uuid RETURNING `+profileColumns, id))
}

// subscribersOnProfile returns the ids of every subscriber currently assigned to
// a profile — the set D invalidates B's policy cache for on an immediate edit.
func subscribersOnProfile(ctx context.Context, db *pgxpool.Pool, profileID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT id::text FROM subscribers WHERE profile_id = $1::uuid`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// isNotFound reports the profile row was absent.
func isNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
