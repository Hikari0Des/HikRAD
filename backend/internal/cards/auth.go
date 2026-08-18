package cards

// radius.CardAuthenticator implementation (v4-5, FR-136). Builds a full
// radius.AuthView from a card + its group's settings and hands it to the
// EXISTING v4-4 limit engine (radius/limits.go's evaluateLimits, invoked
// from policy.go's decide() exactly like a subscriber's view) — this file
// contains NO accept/reject decision of its own; it only resolves data.
// Binding rule / gate item 10: there is no second enforcement
// implementation here, only a second AuthView SOURCE.
//
// SubscriberID is deliberately left EMPTY: a card is not a subscriber, and
// sessions.subscriber_id / usage_points.subscriber_id already have a
// documented "NULL means voucher/unknown login, still recorded" meaning
// (migrations 0130/0131) — a card's own usage is instead tracked via the
// nullable card_id column those tables gained in migration 0604, resolved by
// internal/accounting's resolver exactly like subscriber_id is, and summed
// here (attachCardUsage-style LATERAL queries) the same way a subscriber's
// live quota/uptime are derived. Mid-session enforcement (a live card
// crossing its limit) is intentionally out of this phase's scope — see the
// v4-5 gate result — auth-time re-evaluation on every reconnect is what
// actually enforces it, the same posture v4-4's own gate took for the
// packet-harness leg.

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cardAuthenticator struct{ db *pgxpool.Pool }

type cardAuthRow struct {
	cardID           string
	groupID          string
	state            string
	firstLoginAt     *time.Time
	usageCycleAnchor time.Time

	groupEnabled       bool
	validUntil         *time.Time
	rateDownKbps       int
	rateUpKbps         int
	transferMode       string
	transferTotalBytes *int64
	transferDownBytes  *int64
	transferUpBytes    *int64
	uptimeLimitSeconds *int64
	expireAfterSeconds *int64
}

// AuthenticateCard resolves code to its card+group, checks the group-level
// enable switch and the per-card disabled state, stamps first_login_at on
// the card's very first successful use, and builds the AuthView the v4-4
// engine evaluates. ok=false for anything that should read as
// "not a recognized card" (including a disabled group/card) so the caller
// falls through to unknown_user exactly like an unrecognized voucher.
func (a *cardAuthenticator) AuthenticateCard(ctx context.Context, code string) (radius.AuthView, bool, error) {
	h := hashCode(code)
	var r cardAuthRow
	err := a.db.QueryRow(ctx, `
		SELECT c.id::text, c.group_id::text, c.state, c.first_login_at, c.usage_cycle_anchor,
		       g.enabled, g.valid_until, g.rate_down_kbps, g.rate_up_kbps, g.transfer_mode,
		       g.transfer_total_bytes, g.transfer_down_bytes, g.transfer_up_bytes,
		       g.uptime_limit_seconds, g.expire_after_seconds
		  FROM cards c JOIN card_groups g ON g.id = c.group_id
		 WHERE c.code_hash = $1`, h).Scan(
		&r.cardID, &r.groupID, &r.state, &r.firstLoginAt, &r.usageCycleAnchor,
		&r.groupEnabled, &r.validUntil, &r.rateDownKbps, &r.rateUpKbps, &r.transferMode,
		&r.transferTotalBytes, &r.transferDownBytes, &r.transferUpBytes,
		&r.uptimeLimitSeconds, &r.expireAfterSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return radius.AuthView{}, false, nil
	}
	if err != nil {
		return radius.AuthView{}, false, err
	}
	if !r.groupEnabled || r.state == "disabled" {
		return radius.AuthView{}, false, nil
	}

	now := time.Now().UTC()
	firstLogin := r.firstLoginAt
	if firstLogin == nil {
		firstLogin = &now
		// First use: stamp it and move unused -> active. Best-effort — a
		// failure here must not deny a legitimate first login (mirrors
		// policy.go's own learn-MAC posture); the next attempt just re-stamps.
		if _, uerr := a.db.Exec(ctx,
			`UPDATE cards SET first_login_at = $2, state = 'active' WHERE id = $1::uuid AND first_login_at IS NULL`,
			r.cardID, now); uerr != nil {
			return radius.AuthView{}, false, uerr
		}
	}

	down, up, err := sumCardBytes(ctx, a.db, r.cardID, r.usageCycleAnchor)
	if err != nil {
		return radius.AuthView{}, false, err
	}
	uptimeUsed, err := sumCardUptime(ctx, a.db, r.cardID, r.usageCycleAnchor)
	if err != nil {
		return radius.AuthView{}, false, err
	}

	view := radius.AuthView{
		SubscriberID:   "", // not a subscriber; see file doc comment
		Status:         "active",
		ExpiryBehavior: "block", // cards have no configurable exhaust actions (brief scope)
		ExpiresAt:      effectiveExpiry(firstLogin, r.expireAfterSeconds, r.validUntil),
		QuotaBehavior:  "block",
		QuotaExhausted: quotaExhausted(r.transferMode, r.transferTotalBytes, r.transferDownBytes, r.transferUpBytes, down, up),
		RateLimit:      rateString(r.rateUpKbps, r.rateDownKbps),
		ServiceType:    "hotspot",

		UptimeLimitSeconds:  int64Or(r.uptimeLimitSeconds, 0),
		UptimeUsedSeconds:   uptimeUsed,
		UptimeExhaustAction: "block",
	}
	return view, true, nil
}

// effectiveExpiry folds BOTH card expiry constraints — the post-first-login
// window and the group's hard shelf date — onto the ONE AuthView.ExpiresAt
// field the existing engine already checks, by taking whichever is
// EARLIER. This is what makes gate item 7 ("a card inside its post-login
// window but past valid_until is refused") true through the engine's
// existing expiry check, with no new branch in evaluateLimits: valid_until
// is "checked independently" in the sense that it is never skipped just
// because the post-login window hasn't elapsed yet, which folding-to-the-
// minimum guarantees by construction.
func effectiveExpiry(firstLogin *time.Time, expireAfterSeconds *int64, validUntil *time.Time) time.Time {
	var windowEnd time.Time
	if expireAfterSeconds != nil && *expireAfterSeconds > 0 && firstLogin != nil {
		windowEnd = firstLogin.Add(time.Duration(*expireAfterSeconds) * time.Second)
	}
	switch {
	case !windowEnd.IsZero() && validUntil != nil:
		if validUntil.Before(windowEnd) {
			return *validUntil
		}
		return windowEnd
	case !windowEnd.IsZero():
		return windowEnd
	case validUntil != nil:
		return *validUntil
	default:
		return time.Time{} // no expiry at all
	}
}

// quotaExhausted mirrors accounting/quota.go's evaluator shape for the
// card's own transfer-limit modes.
func quotaExhausted(mode string, total, downLimit, upLimit *int64, usedDown, usedUp int64) bool {
	switch mode {
	case "combined":
		return total != nil && *total > 0 && usedDown+usedUp >= *total
	case "separate":
		return (downLimit != nil && *downLimit > 0 && usedDown >= *downLimit) ||
			(upLimit != nil && *upLimit > 0 && usedUp >= *upLimit)
	default: // "unlimited"
		return false
	}
}

func int64Or(p *int64, d int64) int64 {
	if p == nil {
		return d
	}
	return *p
}

// rateString mirrors subscribers/authview.go's rateString exactly (MikroTik's
// rx-rate limits upload, tx-rate limits download, so the abstract intent is
// upload-first) — duplicated rather than imported, matching the existing
// per-package small-pure-function convention (profiles/subscribers/billing
// each already carry their own copy).
func rateString(upKbps, downKbps int) string {
	if upKbps <= 0 && downKbps <= 0 {
		return ""
	}
	return rateToken(upKbps) + "/" + rateToken(downKbps)
}

func rateToken(kbps int) string {
	if kbps <= 0 {
		return "0"
	}
	if kbps%1024 == 0 {
		return strconv.Itoa(kbps/1024) + "M"
	}
	return strconv.Itoa(kbps) + "k"
}
