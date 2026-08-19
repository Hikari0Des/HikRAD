package billing

// Refund / cancel-renewal (FR-25). A refund is a REVERSING ledger entry linked to
// the original via reverses_id — the original row is never touched. It credits the
// acting manager's balance back, rolls the subscriber's expiry back by the granted
// duration (floor: now), requires a reason, is audited, and re-applies FR-9
// behavior via CoA when the rollback lands the subscriber in the expired state.
// A second refund of the same transaction is rejected (unique reverses_id index).
//
// refundCore is the ONE transactional implementation (FR-159's "approval
// replays the original code path" discipline, applied even though a refund
// itself isn't gated for approval — undo.go's undoRenewal calls this exact
// function too, so "undo a renewal" and "refund a renewal" can never drift
// into two different reversal behaviors). refundHandler is its HTTP shell.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/live"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
)

var (
	errTxNotFound      = errors.New("billing: transaction not found")
	errTxMismatch      = errors.New("billing: transaction does not belong to this subscriber")
	errNotRefundable   = errors.New("billing: a refund cannot itself be refunded")
	errAlreadyRefunded = errors.New("billing: this transaction has already been refunded")
)

type refundRequest struct {
	LedgerTxID string `json:"ledger_tx_id"`
	Reason     string `json:"reason"`
}

type refundCoreResult struct {
	RefundLedgerTxID string
	NewExpiresAt     time.Time
	CoAResult        string
}

func (m *Module) refundHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subID := chi.URLParam(r, "id")
	var in refundRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.LedgerTxID == "" {
		fe = append(fe, httpapi.FieldError{Field: "ledger_tx_id", Message: "this field is required"})
	}
	if in.Reason == "" {
		fe = append(fe, httpapi.FieldError{Field: "reason", Message: "a reason is required"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	if !m.subscriberVisible(ctx, subID, auth.ScopeFilter(ctx)) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "subscriber not found")
		return
	}

	res, err := m.refundCore(ctx, subID, in.LedgerTxID, in.Reason, "panel", "billing.refund")
	if m.writeRefundError(w, err) {
		return
	}
	_ = auth.Audit(ctx, "subscriber.refund", "subscriber", subID, nil, map[string]any{
		"refund_ledger_tx_id": res.RefundLedgerTxID, "reverses": in.LedgerTxID,
		"new_expires_at": res.NewExpiresAt, "reason": in.Reason, "coa_result": res.CoAResult,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"refund_ledger_tx_id": res.RefundLedgerTxID,
		"reverses_id":         in.LedgerTxID,
		"new_expires_at":      res.NewExpiresAt,
		"coa_result":          res.CoAResult,
	})
}

func (m *Module) writeRefundError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errTxNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "transaction not found")
	case errors.Is(err, errTxMismatch):
		httpapi.Error(w, http.StatusUnprocessableEntity, "mismatch", "transaction does not belong to this subscriber")
	case errors.Is(err, errNotRefundable):
		httpapi.Error(w, http.StatusUnprocessableEntity, "not_refundable", "a refund cannot itself be refunded")
	case errors.Is(err, errAlreadyRefunded):
		httpapi.Error(w, http.StatusConflict, "already_refunded", "this transaction has already been refunded")
	case errors.Is(err, errNoRate):
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_rate", err.Error())
	default:
		m.internalError(w, "refund", err)
	}
	return true
}

// refundCore performs the entire reversing-entry transaction: load + validate
// the original entry, reverse the ledger (+ journal, §3.1), reverse the
// payment/receipt, roll back expiry, and (post-commit) re-apply FR-9 via CoA.
// source/journalSource let a caller other than the panel (undo.go) attribute
// the reversal accurately without duplicating this logic.
func (m *Module) refundCore(ctx context.Context, subID, ledgerTxID, reason, source, journalSource string) (refundCoreResult, error) {
	receiptPrefix := m.getString(ctx, keyReceiptPrefix, "HR-")

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return refundCoreResult{}, err
	}
	defer tx.Rollback(ctx)

	var (
		origType       string
		origAmount     int64
		origCurrency   string
		origActor      *string
		origSub        *string
		origGross      int64
		origCostAtSale *int64
	)
	err = tx.QueryRow(ctx,
		`SELECT l.type, l.amount, l.currency, l.actor_manager_id::text, l.subscriber_id::text,
		        COALESCE((SELECT amount FROM payments WHERE ledger_tx_id = l.id LIMIT 1), 0),
		        l.cost_at_sale
		   FROM ledger_transactions l WHERE l.id = $1::uuid`, ledgerTxID).
		Scan(&origType, &origAmount, &origCurrency, &origActor, &origSub, &origGross, &origCostAtSale)
	if errors.Is(err, pgx.ErrNoRows) {
		return refundCoreResult{}, errTxNotFound
	}
	if err != nil {
		return refundCoreResult{}, err
	}
	if origSub == nil || *origSub != subID {
		return refundCoreResult{}, errTxMismatch
	}
	if origType == "refund" {
		return refundCoreResult{}, errNotRefundable
	}

	actor := ""
	if origActor != nil {
		actor = *origActor
	}

	dbNow, err := nowIn(ctx, tx)
	if err != nil {
		return refundCoreResult{}, err
	}

	// Lock the subscriber FIRST, then the balance — the same lock order as the
	// renewal path (subscriber → manager_balances) so a concurrent renew+refund of
	// the same subscriber can never deadlock. Loads the rollback fields too.
	var (
		curExpires  *time.Time
		duration    int
		behavior    string
		expiredPool *string
	)
	err = tx.QueryRow(ctx,
		`SELECT s.expires_at, COALESCE(p.duration_days,0), COALESCE(p.expiry_behavior,'block'),
		        (SELECT name FROM ip_pools WHERE purpose = 'expired' ORDER BY name LIMIT 1)
		   FROM subscribers s LEFT JOIN profiles p ON p.id = s.profile_id
		  WHERE s.id = $1::uuid FOR UPDATE OF s`, subID).
		Scan(&curExpires, &duration, &behavior, &expiredPool)
	if err != nil {
		return refundCoreResult{}, err
	}
	if actor != "" {
		if _, err := lockBalance(ctx, tx, actor); err != nil {
			return refundCoreResult{}, err
		}
	}

	// v3-2 (FR-101.2): the refund lands in IQD. For a NEW (IQD) original it
	// reverses the stored amount EXACTLY — never re-converted (the whole point).
	// For a rare pre-v3-2 FOREIGN original (the foreign book no longer exists),
	// the reversal is converted to IQD at the current rate and stamped, since
	// there is no IQD figure to reverse to. `entered_*` records the original.
	refundAmount, refundRateID, err := signedToIQD(ctx, tx, -origAmount, origCurrency)
	if err != nil {
		return refundCoreResult{}, err
	}
	refundGross, _, err := signedToIQD(ctx, tx, -origGross, origCurrency)
	if err != nil && !errors.Is(err, errNoRate) {
		return refundCoreResult{}, err
	}

	// Reversing entry: negate the original balance effect, and (v2 phase 9,
	// FR-72.2) the original cost_at_sale too, so a refunded renewal's margin
	// nets to zero the same way its amount already does — read from the
	// original row, never re-resolved from today's cost history. The unique
	// reverses_id index rejects a second refund of the same tx (23505).
	var refundCostAtSale *int64
	if origCostAtSale != nil {
		neg := -*origCostAtSale
		refundCostAtSale = &neg
	}
	entry := ledgerEntry{
		Type:           "refund",
		Amount:         refundAmount,
		ActorManagerID: actor,
		SubscriberID:   subID,
		Source:         source,
		ReversesID:     ledgerTxID,
		Note:           reason,
		CostAtSale:     refundCostAtSale,
		CurrencyRateID: refundRateID,
	}
	if origCurrency != "" && origCurrency != "IQD" {
		ea := -origAmount
		entry.EnteredAmount = &ea
		entry.EnteredCurrency = origCurrency
	}
	refundID, err := insertLedger(ctx, tx, entry)
	if isUniqueViolation(err) {
		return refundCoreResult{}, errAlreadyRefunded
	}
	if err != nil {
		return refundCoreResult{}, err
	}
	if actor != "" {
		if err := recomputeBalance(ctx, tx, actor); err != nil {
			return refundCoreResult{}, err
		}
	}

	// FR-153, §3.1: staged dual-write, same transaction as the reversing
	// ledger row above. signedPair (journal.go) reverses the ORIGINAL
	// entry's direction automatically because refundAmount/refundGross are
	// already the signed negation of the original — no special-casing.
	if err := postLedgerJournal(ctx, tx, dbNow, entry, refundID, refundGross, true, journalSource, actor); err != nil {
		return refundCoreResult{}, err
	}

	// Negative IQD payment row so revenue_daily nets the reversal.
	receiptNo, err := nextReceiptNo(ctx, tx, receiptPrefix)
	if err != nil {
		return refundCoreResult{}, err
	}
	if err := insertPayment(ctx, tx, paymentRow{
		ReceiptNo: receiptNo, LedgerTxID: refundID, SubscriberID: subID,
		Amount: refundGross, Currency: "IQD", Method: "refund", Source: source, ShareToken: randToken(),
	}); err != nil {
		return refundCoreResult{}, err
	}

	// Expiry rollback: floor now. Roll back by the current profile's granted
	// duration (the cancelled renewal's grant in the common cancel-renewal case).
	newExpiry := rollbackExpiry(dbNow, curExpires, duration)
	nowExpired := !newExpiry.After(dbNow)
	status := "active"
	if nowExpired {
		status = "expired"
	}
	if _, err := tx.Exec(ctx,
		`UPDATE subscribers SET expires_at = $2, status = $3 WHERE id = $1::uuid`,
		subID, newExpiry, status); err != nil {
		return refundCoreResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return refundCoreResult{}, err
	}

	// Post-commit: invalidate policy; if the rollback expired an online user,
	// re-apply FR-9 via CoA (move to expired pool or disconnect).
	_ = radius.InvalidatePolicy(subID)
	coa := "not_applicable"
	if nowExpired {
		coa = m.enforceExpiredCoA(ctx, subID, behavior, strOr(expiredPool, ""))
	}
	return refundCoreResult{RefundLedgerTxID: refundID, NewExpiresAt: newExpiry.UTC(), CoAResult: coa}, nil
}

// rollbackExpiry rolls a subscriber's expiry back by durationDays, floored at now
// (FR-25). A nil current expiry (never expired) floors to now.
func rollbackExpiry(now time.Time, current *time.Time, durationDays int) time.Time {
	if current == nil {
		return now
	}
	rolled := current.AddDate(0, 0, -durationDays)
	if rolled.Before(now) {
		return now
	}
	return rolled
}

// enforceExpiredCoA re-applies FR-9 to a now-expired online subscriber by
// ENQUEUEING the CoA (v4-8, FR-150): move to the expired pool when the
// profile is configured for it and one exists, otherwise disconnect so the
// session re-auths under the expired policy. Returns the enqueue outcome
// (enqueued | failed | not_online) — the real ack/nak/disconnect-fallback
// resolves asynchronously on the CoA Outbox screen, not here.
func (m *Module) enforceExpiredCoA(ctx context.Context, subID, behavior, expiredPool string) string {
	sessions, err := live.List(ctx, live.Filter{}, nil)
	if err != nil {
		return "not_online"
	}
	var refs []radius.SessionRef
	for _, s := range sessions {
		if s.SubscriberID == subID {
			refs = append(refs, radius.SessionRef{
				NASID: s.NASID, AcctSessionID: s.AcctSessionID, Username: s.Username, FramedIP: s.IP,
				Service: s.Service,
			})
		}
	}
	if len(refs) == 0 {
		return "not_online"
	}
	for _, ref := range refs {
		var enqErr error
		if behavior == "expired_pool" && expiredPool != "" {
			_, enqErr = radius.EnqueueMovePool(ctx, ref, expiredPool, "billing.refund_rollback_expired", subID)
		} else {
			_, enqErr = radius.EnqueueDisconnect(ctx, ref, "billing.refund_rollback_expired", subID)
		}
		if enqErr != nil {
			return "failed"
		}
	}
	return "enqueued"
}
