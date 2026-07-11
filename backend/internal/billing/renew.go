package billing

// THE single atomic renewal path (contract C2, FR-19.3). Panel, agent, voucher
// redemption and (Phase 4) portal e-wallet all converge on renew: no other code
// extends an expiry or bills a subscriber. The transaction is: lock subscriber →
// resolve target profile (reject if archived) → resolve price (override→profile)
// → balance check (agents hard-enforced; admins per settings) → ledger entry +
// payment/receipt → extend expiry per the anchor rule → quota-cycle reset →
// status active → commit. After commit (never inside it): InvalidatePolicy, clear
// the quota-exhausted flag + publish quota:reset, and CoA-restore online sessions
// to full speed with a NAK→Disconnect fallback surfaced in the result.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/live"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel outcomes the API layer maps to HTTP codes.
var (
	errNoSubscriber      = errors.New("billing: subscriber not found")
	errNoProfile         = errors.New("billing: no profile to renew")
	errProfileArchived   = errors.New("billing: profile is archived")
	errInsufficientFunds = errors.New("billing: insufficient balance")
)

// renewParams configures one run of the single renewal path.
type renewParams struct {
	subscriberID   string
	profileID      string // "" keeps the subscriber's current profile
	actorManagerID string // whose balance the entry moves (renewing agent / batch creator)
	source         string // panel | agent | voucher | portal-<gw>
	ledgerType     string // renewal | voucher_redeem
	method         string // payment method label: renewal | cash | voucher
	note           string
	reference      string // receipt no is filled in; caller ref (voucher id) optional
	chargeBalance  bool   // false when prepaid (voucher charged at generation)
	enforceBalance bool   // block on insufficient balance (agents always true)
	idemKey        string // Idempotency-Key header (empty = none)
	// Settings resolved by the caller BEFORE opening the transaction. Reading
	// settings (which query the pool) inside an open transaction risks a
	// pool-exhaustion deadlock under concurrency, so renewInTx never touches the
	// settings service — it takes the resolved values here.
	anchorRule    string
	receiptPrefix string
}

// fillSettings resolves the settings renewInTx needs, before any transaction is
// opened (avoids a pool-exhaustion deadlock — see renewParams).
func (m *Module) fillSettings(ctx context.Context, p *renewParams) {
	if p.anchorRule == "" {
		p.anchorRule = m.anchor(ctx)
	}
	if p.receiptPrefix == "" {
		p.receiptPrefix = m.getString(ctx, keyReceiptPrefix, "HR-")
	}
}

// renewResult is the C2 response shape.
type renewResult struct {
	LedgerTxID   string    `json:"ledger_tx_id"`
	ReceiptNo    string    `json:"receipt_no"`
	NewExpiresAt time.Time `json:"new_expires_at"`
	CoAResult    string    `json:"coa_result"` // restored | disconnect_fallback | failed | not_online
	// internal, not serialized: needed for the post-commit CoA restore.
	priceIQD int64
	rate     string
	poolName string
}

// renew executes the transactional renewal then the post-commit side effects. It
// is the ONLY writer of expiry+ledger+payment.
func (m *Module) renew(ctx context.Context, p renewParams) (renewResult, error) {
	// Idempotency fast path: a replay of the same key returns the stored result
	// without charging again (handled transactionally below via the PK reserve).
	res, dup, err := m.renewTx(ctx, p)
	if err != nil {
		return renewResult{}, err
	}
	if dup {
		return res, nil // replay: side effects already ran on the original call
	}

	// Post-commit side effects (never inside the txn): a CoA failure must not roll
	// back a committed renewal.
	_ = radius.InvalidatePolicy(p.subscriberID)
	m.resetQuota(ctx, p.subscriberID)
	res.CoAResult = m.restoreCoA(ctx, p.subscriberID, res.rate, res.poolName)
	return res, nil
}

// renewTx runs the atomic transaction. dup=true means an idempotent replay whose
// stored response is returned (no new charge).
func (m *Module) renewTx(ctx context.Context, p renewParams) (renewResult, bool, error) {
	m.fillSettings(ctx, &p) // resolve settings BEFORE opening the transaction
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return renewResult{}, false, err
	}
	defer tx.Rollback(ctx)

	// Idempotency reserve — FIRST write so a concurrent duplicate blocks on the PK
	// then reads the committed response (edge case: double-submit renewal).
	if p.idemKey != "" {
		_, err := tx.Exec(ctx,
			`INSERT INTO renewal_idempotency (idem_key, subscriber_id) VALUES ($1, $2::uuid)`,
			p.idemKey, p.subscriberID)
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			return m.readIdempotent(ctx, p.idemKey)
		}
		if err != nil {
			return renewResult{}, false, err
		}
	}

	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
		return renewResult{}, false, err
	}

	res, err := m.renewInTx(ctx, tx, dbNow.UTC(), p)
	if err != nil {
		return renewResult{}, false, err
	}

	// Store the idempotent response before commit so a blocked duplicate reads it.
	if p.idemKey != "" {
		buf, _ := json.Marshal(res)
		if _, err := tx.Exec(ctx,
			`UPDATE renewal_idempotency SET response = $2 WHERE idem_key = $1`,
			p.idemKey, buf); err != nil {
			return renewResult{}, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return renewResult{}, false, err
	}
	return res, false, nil
}

// renewInTx is the money work of a renewal against an already-open transaction:
// lock subscriber → resolve profile/price → balance check → ledger + payment →
// extend expiry → quota reset → status active. It does NOT commit or run any
// post-commit side effect. The voucher-redeem path calls this directly so the
// single-use voucher lock and the renewal share one atomic transaction.
func (m *Module) renewInTx(ctx context.Context, tx pgx.Tx, dbNow time.Time, p renewParams) (renewResult, error) {
	// Lock the subscriber row (serializes concurrent renewals of the same user).
	var (
		curExpires    *time.Time
		curProfile    *string
		priceOverride *int64
		rateOverride  *string
	)
	err := tx.QueryRow(ctx,
		`SELECT expires_at, profile_id::text, price_override, rate_override
		   FROM subscribers WHERE id = $1::uuid FOR UPDATE`, p.subscriberID).
		Scan(&curExpires, &curProfile, &priceOverride, &rateOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return renewResult{}, errNoSubscriber
	}
	if err != nil {
		return renewResult{}, err
	}

	targetProfile := p.profileID
	if targetProfile == "" && curProfile != nil {
		targetProfile = *curProfile
	}
	if targetProfile == "" {
		return renewResult{}, errNoProfile
	}

	// Load the target profile (reject a concurrent archive cleanly).
	var (
		profPrice int64
		duration  int
		archived  bool
		rateUp    int
		rateDown  int
		poolName  *string
	)
	err = tx.QueryRow(ctx,
		`SELECT price_iqd, duration_days, archived, rate_up_kbps, rate_down_kbps,
		        (SELECT name FROM ip_pools WHERE id = p.pool_id)
		   FROM profiles p WHERE id = $1::uuid`, targetProfile).
		Scan(&profPrice, &duration, &archived, &rateUp, &rateDown, &poolName)
	if errors.Is(err, pgx.ErrNoRows) {
		return renewResult{}, errNoProfile
	}
	if err != nil {
		return renewResult{}, err
	}
	if archived {
		return renewResult{}, errProfileArchived
	}

	// Price resolution: subscriber override → profile (FR-19.2).
	price := resolvePrice(priceOverride, profPrice)

	// Balance: lock the actor's balance row, enforce when required.
	balanceDelta := int64(0)
	if p.chargeBalance {
		balanceDelta = -price
		bal, err := lockBalance(ctx, tx, p.actorManagerID)
		if err != nil {
			return renewResult{}, err
		}
		if p.enforceBalance && bal < price {
			return renewResult{}, errInsufficientFunds
		}
	}

	// Ledger entry.
	txID, err := insertLedger(ctx, tx, ledgerEntry{
		Type:           p.ledgerType,
		AmountIQD:      balanceDelta,
		ActorManagerID: p.actorManagerID,
		SubscriberID:   p.subscriberID,
		Source:         p.source,
		Reference:      p.reference,
		Note:           p.note,
	})
	if err != nil {
		return renewResult{}, err
	}
	if p.chargeBalance {
		if err := recomputeBalance(ctx, tx, p.actorManagerID); err != nil {
			return renewResult{}, err
		}
	}

	// Expiry extension per the anchor rule (FR-19.1 / AC-19a). anchorRule was
	// resolved before the transaction opened (pool-safety, see renewParams).
	newExpiry := computeExpiry(dbNow, curExpires, p.anchorRule, duration)

	// Persist: extend expiry, switch profile if requested, quota-cycle reset,
	// status active, consume a pending profile switch if it matched.
	if _, err := tx.Exec(ctx,
		`UPDATE subscribers
		    SET expires_at = $2, profile_id = $3::uuid, status = 'active',
		        quota_cycle_anchor = now(),
		        pending_profile_id = CASE WHEN pending_profile_id = $3::uuid THEN NULL ELSE pending_profile_id END
		  WHERE id = $1::uuid`,
		p.subscriberID, newExpiry, targetProfile); err != nil {
		return renewResult{}, err
	}

	// Receipt + payment (gross revenue recorded here, decoupled from balance).
	receiptNo, err := nextReceiptNo(ctx, tx, p.receiptPrefix)
	if err != nil {
		return renewResult{}, err
	}
	if err := insertPayment(ctx, tx, paymentRow{
		ReceiptNo:    receiptNo,
		LedgerTxID:   txID,
		SubscriberID: p.subscriberID,
		AmountIQD:    price,
		Method:       p.method,
		Source:       p.source,
		ShareToken:   randToken(),
	}); err != nil {
		return renewResult{}, err
	}

	return renewResult{
		LedgerTxID:   txID,
		ReceiptNo:    receiptNo,
		NewExpiresAt: newExpiry.UTC(),
		priceIQD:     price,
		rate:         resolveRate(rateOverride, rateUp, rateDown),
		poolName:     strOr(poolName, ""),
	}, nil
}

// readIdempotent returns the stored response for an already-processed key. The
// row is guaranteed committed (our conflicting INSERT only 23505s after the
// original transaction committed).
func (m *Module) readIdempotent(ctx context.Context, key string) (renewResult, bool, error) {
	var raw []byte
	if err := m.db.QueryRow(ctx,
		`SELECT response FROM renewal_idempotency WHERE idem_key = $1`, key).Scan(&raw); err != nil {
		return renewResult{}, false, err
	}
	var res renewResult
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &res)
	}
	return res, true, nil
}

// resetQuota clears the quota-exhausted flag and publishes quota:reset so C (and
// B's enforcement) drop any stale throttle for the fresh cycle (contract C4/C8).
func (m *Module) resetQuota(ctx context.Context, subscriberID string) {
	if m.rdb == nil {
		return
	}
	_ = m.rdb.Del(ctx, "quota:exhausted:"+subscriberID).Err()
	_ = m.rdb.Publish(ctx, "quota:reset:"+subscriberID, subscriberID).Err()
}

// restoreCoA pushes the renewed full-speed policy to every live session (key flow
// 2 step 4 / AC-19b): MovePool out of the walled garden + ApplyRate to full, with
// a NAK/timeout → Disconnect fallback (forces a redial that re-reads policy). The
// aggregate result is surfaced in the response, never silent (C2).
func (m *Module) restoreCoA(ctx context.Context, subscriberID, rate, poolName string) string {
	sessions, err := live.List(ctx, live.Filter{}, nil)
	if err != nil {
		return "not_online"
	}
	var refs []radius.SessionRef
	for _, s := range sessions {
		if s.SubscriberID == subscriberID {
			refs = append(refs, radius.SessionRef{
				NASID: s.NASID, AcctSessionID: s.AcctSessionID, Username: s.Username, FramedIP: s.IP,
			})
		}
	}
	if len(refs) == 0 {
		return "not_online"
	}

	usedFallback := false
	for _, ref := range refs {
		ok := true
		if poolName != "" {
			ok = radius.MovePool(ctx, ref, poolName).Ok() && ok
		}
		if rate != "" {
			ok = radius.ApplyRate(ctx, ref, rate).Ok() && ok
		}
		if !ok {
			// Fallback: drop the session so it redials into the renewed policy.
			if !radius.Disconnect(ctx, ref).Ok() {
				return "failed"
			}
			usedFallback = true
		}
	}
	if usedFallback {
		return "disconnect_fallback"
	}
	return "restored"
}

// computeExpiry applies the anchor rule (FR-19.1): from_expiry extends from the
// current expiry while the account is still active, otherwise from now; from_now
// always extends from now. An already-expired account always renews from now.
func computeExpiry(now time.Time, current *time.Time, anchor string, durationDays int) time.Time {
	base := now
	if anchor == anchorFromExpiry && current != nil && current.After(now) {
		base = *current
	}
	return base.AddDate(0, 0, durationDays)
}

// resolvePrice applies FR-19.2 price resolution: a subscriber price override wins
// over the profile price (promo pricing is a Could/FR-26, not resolved here).
func resolvePrice(override *int64, profilePrice int64) int64 {
	if override != nil {
		return *override
	}
	return profilePrice
}

// resolveRate renders the CoA restore rate: subscriber override wins, else the
// profile's up/down kbps rendered as the abstract "rx/tx" intent B expects.
func resolveRate(override *string, upKbps, downKbps int) string {
	if override != nil && *override != "" {
		return *override
	}
	return rateString(upKbps, downKbps)
}

// rateString / rateToken mirror subscribers/authview.go so the CoA restore rate
// matches exactly what the authorize path emits (upload-first, download-second).
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

func strOr(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

// isUniqueViolation reports a 23505 (idempotency key / voucher hash collision).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// enforceBalanceFor decides whether a renewal blocks on balance: scoped managers
// (agents) are always enforced; unscoped (admin) callers are bypassed only when
// the admin-bypass setting is on (FR-20.3). Uses the Scoped flag, never a role
// name (frozen contract).
func (m *Module) enforceBalanceFor(ctx context.Context, mgr *auth.Manager) bool {
	if mgr != nil && mgr.Scoped {
		return true
	}
	return !m.getBool(ctx, keyAdminBypass, true)
}
