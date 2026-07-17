package billing

// v2 phase 9 (FR-71/FR-74/FR-76, contracts C1/C4/C6): plan cost and reseller
// wholesale-price resolution. Both follow the same "query the latest
// effective_from <= at" pattern v2-4's currency_rates already established —
// neither is ever mirrored onto another table, so there is nothing to keep
// in sync and nothing that can drift.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// resolveCost returns the plan cost in force at `at` (a renewal's own
// timestamp, so re-querying a past renewal's margin later is always
// reproducible). ok=false means the profile has no cost recorded as of `at`
// — the caller must treat this as UNKNOWN, never as zero (FR-71.1).
func resolveCost(ctx context.Context, tx pgx.Tx, profileID string, at time.Time) (cost int64, currency string, ok bool, err error) {
	err = tx.QueryRow(ctx,
		`SELECT cost, currency FROM profile_cost_history
		  WHERE profile_id = $1::uuid AND effective_from <= $2
		  ORDER BY effective_from DESC LIMIT 1`, profileID, at).
		Scan(&cost, &currency)
	if err == pgx.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return cost, currency, true, nil
}

// resolveWholesale returns the wholesale price a reseller (resellerManagerID)
// pays for subscriberID's renewal on profileID, most-specific-first
// (FR-76.2): a per-subscriber override, else the reseller's plan-wide price,
// else ok=false — meaning "use retail" (FR-74.1's fallback, byte-identical
// to pre-v2-9 behavior when no reseller_prices row exists at all).
func resolveWholesale(ctx context.Context, tx pgx.Tx, resellerManagerID, profileID, subscriberID string, at time.Time) (price int64, currency string, ok bool, err error) {
	if subscriberID != "" {
		err = tx.QueryRow(ctx,
			`SELECT price, currency FROM reseller_prices
			  WHERE manager_id = $1::uuid AND profile_id = $2::uuid AND subscriber_id = $3::uuid
			    AND effective_from <= $4
			  ORDER BY effective_from DESC LIMIT 1`,
			resellerManagerID, profileID, subscriberID, at).Scan(&price, &currency)
		if err == nil {
			return price, currency, true, nil
		}
		if err != pgx.ErrNoRows {
			return 0, "", false, err
		}
	}
	err = tx.QueryRow(ctx,
		`SELECT price, currency FROM reseller_prices
		  WHERE manager_id = $1::uuid AND profile_id = $2::uuid AND subscriber_id IS NULL
		    AND effective_from <= $3
		  ORDER BY effective_from DESC LIMIT 1`,
		resellerManagerID, profileID, at).Scan(&price, &currency)
	if err == pgx.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return price, currency, true, nil
}
