package billing

// Refund / cancel-renewal (FR-25). A refund is a REVERSING ledger entry linked to
// the original via reverses_id — the original row is never touched. It credits the
// acting manager's balance back, rolls the subscriber's expiry back by the granted
// duration (floor: now), requires a reason, is audited, and re-applies FR-9
// behavior via CoA when the rollback lands the subscriber in the expired state.
// A second refund of the same transaction is rejected (unique reverses_id index).

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

type refundRequest struct {
	LedgerTxID string `json:"ledger_tx_id"`
	Reason     string `json:"reason"`
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

	// Resolve settings BEFORE opening the transaction (pool-safety: reading
	// settings queries the pool, which would deadlock if done while holding a
	// transaction connection under concurrency — see billing/renew.go).
	receiptPrefix := m.getString(ctx, keyReceiptPrefix, "HR-")

	tx, err := m.db.Begin(ctx)
	if err != nil {
		m.internalError(w, "refund begin", err)
		return
	}
	defer tx.Rollback(ctx)

	// Load the original entry (must belong to this subscriber) and its gross.
	var (
		origType   string
		origAmount int64
		origActor  *string
		origSub    *string
		origGross  int64
	)
	err = tx.QueryRow(ctx,
		`SELECT l.type, l.amount_iqd, l.actor_manager_id::text, l.subscriber_id::text,
		        COALESCE((SELECT amount_iqd FROM payments WHERE ledger_tx_id = l.id LIMIT 1), 0)
		   FROM ledger_transactions l WHERE l.id = $1::uuid`, in.LedgerTxID).
		Scan(&origType, &origAmount, &origActor, &origSub, &origGross)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	if err != nil {
		m.internalError(w, "refund load", err)
		return
	}
	if origSub == nil || *origSub != subID {
		httpapi.Error(w, http.StatusUnprocessableEntity, "mismatch", "transaction does not belong to this subscriber")
		return
	}
	if origType == "refund" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "not_refundable", "a refund cannot itself be refunded")
		return
	}

	actor := ""
	if origActor != nil {
		actor = *origActor
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
		m.internalError(w, "refund subscriber", err)
		return
	}
	if actor != "" {
		if _, err := lockBalance(ctx, tx, actor); err != nil {
			m.internalError(w, "refund lock", err)
			return
		}
	}

	// Reversing entry: negate the original balance effect. The unique reverses_id
	// index rejects a second refund of the same tx (23505).
	refundID, err := insertLedger(ctx, tx, ledgerEntry{
		Type:           "refund",
		AmountIQD:      -origAmount,
		ActorManagerID: actor,
		SubscriberID:   subID,
		Source:         "panel",
		ReversesID:     in.LedgerTxID,
		Note:           in.Reason,
	})
	if isUniqueViolation(err) {
		httpapi.Error(w, http.StatusConflict, "already_refunded", "this transaction has already been refunded")
		return
	}
	if err != nil {
		m.internalError(w, "refund insert", err)
		return
	}
	if actor != "" {
		if err := recomputeBalance(ctx, tx, actor); err != nil {
			m.internalError(w, "refund recompute", err)
			return
		}
	}

	// Negative payment row so revenue_daily nets the reversal (gross of original).
	receiptNo, err := nextReceiptNo(ctx, tx, receiptPrefix)
	if err != nil {
		m.internalError(w, "refund receipt", err)
		return
	}
	if err := insertPayment(ctx, tx, paymentRow{
		ReceiptNo: receiptNo, LedgerTxID: refundID, SubscriberID: subID,
		AmountIQD: -origGross, Method: "refund", Source: "panel", ShareToken: randToken(),
	}); err != nil {
		m.internalError(w, "refund payment", err)
		return
	}

	// Expiry rollback: floor now. Roll back by the current profile's granted
	// duration (the cancelled renewal's grant in the common cancel-renewal case).
	var dbNow time.Time
	_ = tx.QueryRow(ctx, `SELECT now()`).Scan(&dbNow)
	dbNow = dbNow.UTC()
	newExpiry := rollbackExpiry(dbNow, curExpires, duration)
	nowExpired := !newExpiry.After(dbNow)
	status := "active"
	if nowExpired {
		status = "expired"
	}
	if _, err := tx.Exec(ctx,
		`UPDATE subscribers SET expires_at = $2, status = $3 WHERE id = $1::uuid`,
		subID, newExpiry, status); err != nil {
		m.internalError(w, "refund rollback", err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		m.internalError(w, "refund commit", err)
		return
	}

	// Post-commit: invalidate policy; if the rollback expired an online user,
	// re-apply FR-9 via CoA (move to expired pool or disconnect).
	_ = radius.InvalidatePolicy(subID)
	coa := "not_applicable"
	if nowExpired {
		coa = m.enforceExpiredCoA(ctx, subID, behavior, strOr(expiredPool, ""))
	}
	_ = auth.Audit(ctx, "subscriber.refund", "subscriber", subID, nil, map[string]any{
		"refund_ledger_tx_id": refundID, "reverses": in.LedgerTxID,
		"new_expires_at": newExpiry.UTC(), "reason": in.Reason, "coa_result": coa,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"refund_ledger_tx_id": refundID,
		"reverses_id":         in.LedgerTxID,
		"new_expires_at":      newExpiry.UTC(),
		"coa_result":          coa,
	})
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

// enforceExpiredCoA re-applies FR-9 to a now-expired online subscriber: move to
// the expired pool when the profile is configured for it and one exists,
// otherwise disconnect so the session re-auths under the expired policy. Returns
// the aggregate outcome (restored-to-expired-pool | disconnect | not_online).
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
	outcome := "expired_pool"
	for _, ref := range refs {
		if behavior == "expired_pool" && expiredPool != "" && radius.MovePool(ctx, ref, expiredPool).Ok() {
			continue
		}
		if !radius.Disconnect(ctx, ref).Ok() {
			return "failed"
		}
		outcome = "disconnect"
	}
	return outcome
}
