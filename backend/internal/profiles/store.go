package profiles

// Profile persistence + the JSON read shape (C7-D / C1-D). Speeds are stored as
// abstract down/up kbps; vendor VSA rendering is [02]'s adapter job. Quotas and
// behaviors drive [02]'s auth-time policy through D's AuthView read-model.

import (
	"context"
	"errors"

	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Profile is the full read/write shape (C7-D). Nullable columns are pointers so
// "unset" (inherit / unlimited) is distinct from a zero value.
type Profile struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Price               int64   `json:"price"`
	Currency            string  `json:"currency"`
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
	// NASScopes lists every NAS / service instance a subscriber on this profile
	// may authenticate on (FR-64), unless the subscriber carries a scope set of
	// their own. EMPTY = any NAS (the v1 default).
	NASScopes []radius.NASScope `json:"nas_scopes"`
	Archived  bool              `json:"archived"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

const profileColumns = `id::text, name, price, currency, duration_days, rate_down_kbps, rate_up_kbps,
	pool_id::text, session_limit_default, quota_mode, quota_total_bytes, quota_down_bytes,
	quota_up_bytes, throttle_rate, expiry_behavior, quota_behavior,
	hotspot_rate_down_kbps, hotspot_rate_up_kbps,
	burst_rate, burst_threshold, burst_time, rate_priority, min_rate,
	archived,
	to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`

func scanProfile(row pgx.Row) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Name, &p.Price, &p.Currency, &p.DurationDays, &p.RateDownKbps, &p.RateUpKbps,
		&p.PoolID, &p.SessionLimitDefault, &p.QuotaMode, &p.QuotaTotalBytes, &p.QuotaDownBytes,
		&p.QuotaUpBytes, &p.ThrottleRate, &p.ExpiryBehavior, &p.QuotaBehavior,
		&p.HotspotRateDownKbps, &p.HotspotRateUpKbps,
		&p.BurstRate, &p.BurstThreshold, &p.BurstTime, &p.RatePriority, &p.MinRate,
		&p.Archived, &p.CreatedAt, &p.UpdatedAt)
	// Scopes live in their own table; the caller attaches them. Default to empty
	// (= any NAS) so an un-enriched row never serializes as null.
	p.NASScopes = []radius.NASScope{}
	return p, err
}

// orEmptyScopes normalizes nil to an empty set — "no scopes" means any NAS
// (FR-64's default), which null would leave to a client's guess.
func orEmptyScopes(in []radius.NASScope) []radius.NASScope {
	if in == nil {
		return []radius.NASScope{}
	}
	return in
}

// attachScopes fills a whole page's NASScopes in one query, not one per row.
func attachScopes(ctx context.Context, db *pgxpool.Pool, items []Profile) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, p := range items {
		ids = append(ids, p.ID)
	}
	byOwner, err := radius.LoadScopesFor(ctx, db, radius.ProfileScopes, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].NASScopes = orEmptyScopes(byOwner[items[i].ID])
	}
	return nil
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
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	rows.Close()
	if err := attachScopes(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

func getProfile(ctx context.Context, db *pgxpool.Pool, id string) (Profile, error) {
	p, err := scanProfile(db.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = $1::uuid`, id))
	if err != nil {
		return Profile{}, err
	}
	scopes, err := radius.LoadScopes(ctx, db, radius.ProfileScopes, p.ID)
	if err != nil {
		return Profile{}, err
	}
	p.NASScopes = orEmptyScopes(scopes)
	return p, nil
}

// insertProfile and updateProfile take a transaction, not the pool: the profile
// row and its FR-64 scope set must land together, or a profile could exist
// briefly with no scopes and authorize its subscribers on every NAS.
func insertProfile(ctx context.Context, tx pgx.Tx, in profileInput) (Profile, error) {
	return scanProfile(tx.QueryRow(ctx,
		`INSERT INTO profiles
		   (name, price, currency, duration_days, rate_down_kbps, rate_up_kbps, pool_id,
		    session_limit_default, quota_mode, quota_total_bytes, quota_down_bytes,
		    quota_up_bytes, throttle_rate, expiry_behavior, quota_behavior,
		    hotspot_rate_down_kbps, hotspot_rate_up_kbps,
		    burst_rate, burst_threshold, burst_time, rate_priority, min_rate)
		 VALUES ($1,$2,$3,$4,$5,$6,$7::uuid,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		 RETURNING `+profileColumns,
		in.Name, in.Price, in.Currency, in.DurationDays, in.RateDownKbps, in.RateUpKbps, in.PoolID,
		in.SessionLimitDefault, in.QuotaMode, in.QuotaTotalBytes, in.QuotaDownBytes,
		in.QuotaUpBytes, in.ThrottleRate, in.ExpiryBehavior, in.QuotaBehavior,
		in.HotspotRateDownKbps, in.HotspotRateUpKbps,
		in.BurstRate, in.BurstThreshold, in.BurstTime, in.RatePriority, in.MinRate))
}

func updateProfile(ctx context.Context, tx pgx.Tx, id string, in profileInput) (Profile, error) {
	return scanProfile(tx.QueryRow(ctx,
		`UPDATE profiles SET
		    name=$2, price=$3, currency=$4, duration_days=$5, rate_down_kbps=$6, rate_up_kbps=$7,
		    pool_id=$8::uuid, session_limit_default=$9, quota_mode=$10, quota_total_bytes=$11,
		    quota_down_bytes=$12, quota_up_bytes=$13, throttle_rate=$14, expiry_behavior=$15,
		    quota_behavior=$16, hotspot_rate_down_kbps=$17, hotspot_rate_up_kbps=$18,
		    burst_rate=$20, burst_threshold=$21, burst_time=$22, rate_priority=$23, min_rate=$24,
		    archived=$19
		  WHERE id=$1::uuid
		 RETURNING `+profileColumns,
		id, in.Name, in.Price, in.Currency, in.DurationDays, in.RateDownKbps, in.RateUpKbps, in.PoolID,
		in.SessionLimitDefault, in.QuotaMode, in.QuotaTotalBytes, in.QuotaDownBytes,
		in.QuotaUpBytes, in.ThrottleRate, in.ExpiryBehavior, in.QuotaBehavior,
		in.HotspotRateDownKbps, in.HotspotRateUpKbps, in.Archived,
		in.BurstRate, in.BurstThreshold, in.BurstTime, in.RatePriority, in.MinRate))
}

// profileInUse reports whether anything references this plan. Checked before a
// delete so the refusal names a reason instead of surfacing an FK violation —
// the DB's ON DELETE RESTRICT is the backstop, not the UX.
//
// pending_profile_id is included: a scheduled plan change at next renewal is a
// live reference even though nobody is on the plan yet.
//
// The voucher reference is on voucher_batches, NOT vouchers: a code inherits its
// plan from its batch (0202). Querying vouchers.profile_id raised 42703 on every
// call, so this guard returned an error rather than an answer and no profile
// could be deleted at all — see docs/ops/known-issues.md.
func profileInUse(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	var inUse bool
	// v2-2: payment_intents/card_payments are RETIRED (Decision 37) — replaced
	// by payment_tickets, which generalizes card_payments's own profile_id
	// reference exactly (see docs/ops/known-issues.md for why this guard's
	// table list must stay in lockstep with schema changes, on pain of
	// repeating the exact 42703 bug this function's own history already hit).
	err := db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM subscribers WHERE profile_id = $1::uuid OR pending_profile_id = $1::uuid)
		     OR EXISTS (SELECT 1 FROM voucher_batches WHERE profile_id = $1::uuid)
		     OR EXISTS (SELECT 1 FROM payment_tickets WHERE profile_id = $1::uuid)`,
		id).Scan(&inUse)
	return inUse, err
}

func deleteProfile(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `DELETE FROM profiles WHERE id = $1::uuid`, id)
	return err
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
