package billing

// Scratch-card payments (contract C8, FR-59, amendment 2026-07-11). A
// subscriber submits a telecom airtime-card code and gets an immediate 1-day
// provisional renewal (source card-trial) while the card sits in a manual
// verification queue; approve anchors the FULL renewal at the trial's exact
// start instant (trial_started_at, via renewParams.baseOverride) so the trial
// day is included in, not added to, the paid duration (AC-59b). Reject
// reverses the trial's ledger entry (amount was always 0, so it nets to
// exactly 0) and rolls the expiry back by the 1-day trial grant, floored at
// now, re-applying FR-9 via CoA exactly like a panel refund.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
)

const (
	keyCardTypes        = "card_payments.types"                // []string, default {"zain","asiacell"}
	keyCardCooldownDays = "card_payments.reject_cooldown_days" // int, default 7
	cardTrialDays       = 1
)

var defaultCardTypes = []string{"zain", "asiacell"}

func (m *Module) cardTypes(ctx context.Context) []string {
	if m.settings == nil {
		return defaultCardTypes
	}
	v, err := platform.Get[[]string](ctx, m.settings, keyCardTypes)
	if err != nil || len(v) == 0 {
		return defaultCardTypes
	}
	return v
}

func (m *Module) cardCooldownDays(ctx context.Context) int {
	if m.settings == nil {
		return 7
	}
	v, err := platform.Get[int](ctx, m.settings, keyCardCooldownDays)
	if err != nil || v <= 0 {
		return 7
	}
	return v
}

var (
	errCardTypeNotAllowed = errors.New("billing: card type not allowed")
	errCardPending        = errors.New("billing: a card payment is already pending")
	errCardNotFound       = errors.New("billing: card payment not found")
	errCardNotPending     = errors.New("billing: card payment is not pending")
)

// cardCooldownError carries the exact instant submissions unblock again
// (FR-59.4) so callers can surface it rather than a vague message.
type cardCooldownError struct{ RetryAt time.Time }

func (e *cardCooldownError) Error() string {
	return "billing: card submissions are blocked until " + e.RetryAt.Format(time.RFC3339)
}

type cardSubmitResult struct {
	ID             string
	State          string
	TrialExpiresAt time.Time
}

// submitCard is the portal-facing entry (via portal_seam.go): atomically
// creates the pending row and runs the 1-day trial renewal.
func (m *Module) submitCard(ctx context.Context, subscriberID, cardType, code string) (cardSubmitResult, error) {
	if !containsStr(m.cardTypes(ctx), cardType) {
		return cardSubmitResult{}, errCardTypeNotAllowed
	}

	var lastRejectedAt *time.Time
	err := m.db.QueryRow(ctx,
		`SELECT decided_at FROM card_payments WHERE subscriber_id = $1::uuid AND state = 'rejected'
		  ORDER BY decided_at DESC LIMIT 1`, subscriberID).Scan(&lastRejectedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return cardSubmitResult{}, err
	}
	if lastRejectedAt != nil {
		cooldownUntil := lastRejectedAt.UTC().AddDate(0, 0, m.cardCooldownDays(ctx))
		if time.Now().UTC().Before(cooldownUntil) {
			return cardSubmitResult{}, &cardCooldownError{RetryAt: cooldownUntil}
		}
	}

	var profileID *string
	err = m.db.QueryRow(ctx, `SELECT profile_id::text FROM subscribers WHERE id = $1::uuid`, subscriberID).Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return cardSubmitResult{}, errNoSubscriber
	}
	if err != nil {
		return cardSubmitResult{}, err
	}
	if profileID == nil || *profileID == "" {
		return cardSubmitResult{}, errNoProfile
	}

	codeEnc, err := crypto.Encrypt([]byte(code))
	if err != nil {
		return cardSubmitResult{}, err
	}

	var cardID string
	err = m.db.QueryRow(ctx,
		`INSERT INTO card_payments (subscriber_id, profile_id, card_type, card_code_enc)
		 VALUES ($1::uuid, $2::uuid, $3, $4) RETURNING id::text`,
		subscriberID, *profileID, cardType, codeEnc).Scan(&cardID)
	if isUniqueViolation(err) {
		return cardSubmitResult{}, errCardPending
	}
	if err != nil {
		return cardSubmitResult{}, err
	}

	days := cardTrialDays
	rr, err := m.renew(ctx, renewParams{
		subscriberID: subscriberID, profileID: *profileID,
		source: "card-trial", ledgerType: "adjustment", method: "card-trial",
		reference: cardID, chargeBalance: false, enforceBalance: false,
		durationOverrideDays: &days,
		idemKey:              "card_trial:" + cardID,
	})
	if err != nil {
		return cardSubmitResult{}, err
	}
	if _, err := m.db.Exec(ctx,
		`UPDATE card_payments SET trial_ledger_tx_id = $2::uuid, updated_at = now() WHERE id = $1::uuid`,
		cardID, rr.LedgerTxID); err != nil {
		return cardSubmitResult{}, err
	}
	m.publishCardEvent(ctx, subscriberID, "pending", "")
	return cardSubmitResult{ID: cardID, State: "pending", TrialExpiresAt: rr.NewExpiresAt}, nil
}

// approveCard runs the full FR-19 renewal anchored at the trial's start
// instant (FR-59.2): expiry = trial_started_at + profile duration, the trial
// day included rather than added.
func (m *Module) approveCard(ctx context.Context, cardID, approverID string) (renewResult, error) {
	var subscriberID, profileID, cardType, state string
	var trialStart time.Time
	err := m.db.QueryRow(ctx,
		`SELECT subscriber_id::text, profile_id::text, card_type, state, trial_started_at
		   FROM card_payments WHERE id = $1::uuid`, cardID).
		Scan(&subscriberID, &profileID, &cardType, &state, &trialStart)
	if errors.Is(err, pgx.ErrNoRows) {
		return renewResult{}, errCardNotFound
	}
	if err != nil {
		return renewResult{}, err
	}
	if state != "pending" {
		return renewResult{}, errCardNotPending
	}

	ts := trialStart.UTC()
	rr, err := m.renew(ctx, renewParams{
		subscriberID: subscriberID, profileID: profileID, actorManagerID: approverID,
		source: "card-" + cardType, ledgerType: "renewal", method: "card-" + cardType,
		reference: cardID, chargeBalance: false, enforceBalance: false,
		baseOverride: &ts,
		idemKey:      "card_approve:" + cardID,
	})
	if err != nil {
		return renewResult{}, err
	}

	ct, err := m.db.Exec(ctx,
		`UPDATE card_payments SET state = 'approved', approve_ledger_tx_id = $2::uuid,
		        decided_by = NULLIF($3,'')::uuid, decided_at = now(), updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`,
		cardID, rr.LedgerTxID, approverID)
	if err != nil {
		return renewResult{}, err
	}
	if ct.RowsAffected() == 0 {
		return renewResult{}, errCardNotPending
	}
	m.publishCardEvent(ctx, subscriberID, "approved", "")
	return rr, nil
}

// rejectCard reverses the trial (amount was always 0, so this nets exactly to
// 0 — AC-59b), rolls the expiry back by the 1-day trial grant floored at now,
// and re-applies FR-9 via CoA if the rollback lands the subscriber expired.
func (m *Module) rejectCard(ctx context.Context, cardID, approverID, reason string) error {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var subscriberID, state string
	var trialLedgerTxID *string
	err = tx.QueryRow(ctx,
		`SELECT subscriber_id::text, state, trial_ledger_tx_id::text FROM card_payments WHERE id = $1::uuid FOR UPDATE`,
		cardID).Scan(&subscriberID, &state, &trialLedgerTxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errCardNotFound
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return errCardNotPending
	}

	if trialLedgerTxID != nil {
		// The reversal always nets to 0 (the trial itself was a 0-amount entry,
		// AC-59b) but still carries the trial entry's own currency for an
		// honest ledger reversal (v2 phase 4, mirrors FR-69.5's refund rule).
		var trialCurrency string
		if err := tx.QueryRow(ctx, `SELECT currency FROM ledger_transactions WHERE id = $1::uuid`, *trialLedgerTxID).
			Scan(&trialCurrency); err != nil {
			return err
		}
		if _, err := insertLedger(ctx, tx, ledgerEntry{
			Type: "refund", Amount: 0, Currency: trialCurrency, ActorManagerID: approverID, SubscriberID: subscriberID,
			Source: "card-reject", ReversesID: *trialLedgerTxID, Note: reason,
		}); err != nil && !isUniqueViolation(err) {
			return err
		}
	}

	var curExpires *time.Time
	var expiredPool *string
	var behavior string
	if err := tx.QueryRow(ctx,
		`SELECT s.expires_at, COALESCE(p.expiry_behavior,'block'),
		        (SELECT name FROM ip_pools WHERE purpose = 'expired' ORDER BY name LIMIT 1)
		   FROM subscribers s LEFT JOIN profiles p ON p.id = s.profile_id
		  WHERE s.id = $1::uuid FOR UPDATE OF s`, subscriberID).
		Scan(&curExpires, &behavior, &expiredPool); err != nil {
		return err
	}
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
		return err
	}
	dbNow = dbNow.UTC()
	newExpiry := rollbackExpiry(dbNow, curExpires, cardTrialDays)
	nowExpired := !newExpiry.After(dbNow)
	status := "active"
	if nowExpired {
		status = "expired"
	}
	if _, err := tx.Exec(ctx, `UPDATE subscribers SET expires_at = $2, status = $3 WHERE id = $1::uuid`,
		subscriberID, newExpiry, status); err != nil {
		return err
	}

	ct, err := tx.Exec(ctx,
		`UPDATE card_payments SET state = 'rejected', decided_by = NULLIF($2,'')::uuid,
		        decided_at = now(), reject_reason = $3, updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`, cardID, approverID, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errCardNotPending
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	_ = radius.InvalidatePolicy(subscriberID)
	if nowExpired {
		m.enforceExpiredCoA(ctx, subscriberID, behavior, strOr(expiredPool, ""))
	}
	m.publishCardEvent(ctx, subscriberID, "rejected", reason)
	return nil
}

// revealCard decrypts a card's code (FR-59.4: explicit, audited action —
// callers MUST audit-log the reveal themselves with the acting manager).
func (m *Module) revealCard(ctx context.Context, cardID string) (string, error) {
	var codeEnc []byte
	err := m.db.QueryRow(ctx, `SELECT card_code_enc FROM card_payments WHERE id = $1::uuid`, cardID).Scan(&codeEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errCardNotFound
	}
	if err != nil {
		return "", err
	}
	plain, err := crypto.Decrypt(codeEnc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// myCardPaymentRow is the portal's own latest-record view (F's "pending ISP
// verification" banner / decision notification) — card_code_enc never enters
// this struct.
type myCardPaymentRow struct {
	ID             string
	CardType       string
	State          string
	TrialExpiresAt time.Time
	RejectReason   string
	CreatedAt      time.Time
}

// latestCardPayment returns the subscriber's single most recent card payment
// (any state), or ok=false when they have none.
func (m *Module) latestCardPayment(ctx context.Context, subscriberID string) (myCardPaymentRow, bool, error) {
	var v myCardPaymentRow
	var rejectReason *string
	err := m.db.QueryRow(ctx,
		`SELECT id::text, card_type, state, trial_started_at, reject_reason, created_at
		   FROM card_payments WHERE subscriber_id = $1::uuid
		  ORDER BY created_at DESC LIMIT 1`, subscriberID).
		Scan(&v.ID, &v.CardType, &v.State, &v.TrialExpiresAt, &rejectReason, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return myCardPaymentRow{}, false, nil
	}
	if err != nil {
		return myCardPaymentRow{}, false, err
	}
	v.TrialExpiresAt = v.TrialExpiresAt.AddDate(0, 0, cardTrialDays).UTC()
	v.RejectReason = strOr(rejectReason, "")
	return v, true, nil
}

// cardPaymentSummary is one row of the admin verification queue (FR-59.2).
// card_code_enc is deliberately never selected here.
type cardPaymentSummary struct {
	ID              string     `json:"id"`
	SubscriberID    string     `json:"subscriber_id"`
	Username        string     `json:"username"`
	ProfileID       string     `json:"profile_id"`
	ProfileName     string     `json:"profile_name"`
	RequestedAmount int64      `json:"requested_amount"`
	Currency        string     `json:"currency"`
	CardType        string     `json:"card_type"`
	State           string     `json:"state"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	RejectReason    string     `json:"reject_reason,omitempty"`
}

func (m *Module) listCardPayments(ctx context.Context, state string) ([]cardPaymentSummary, error) {
	rows, err := m.db.Query(ctx,
		`SELECT cp.id::text, cp.subscriber_id::text, s.username, cp.profile_id::text, p.name,
		        p.price, p.currency, cp.card_type, cp.state, cp.created_at, cp.decided_at, COALESCE(cp.reject_reason,'')
		   FROM card_payments cp
		   JOIN subscribers s ON s.id = cp.subscriber_id
		   JOIN profiles p ON p.id = cp.profile_id
		  WHERE ($1 = '' OR cp.state = $1)
		  ORDER BY cp.created_at DESC`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cardPaymentSummary{}
	for rows.Next() {
		var s cardPaymentSummary
		if err := rows.Scan(&s.ID, &s.SubscriberID, &s.Username, &s.ProfileID, &s.ProfileName,
			&s.RequestedAmount, &s.Currency, &s.CardType, &s.State, &s.CreatedAt, &s.DecidedAt, &s.RejectReason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// publishCardEvent fires billing.card_payment {subscriber_id, state, reason?}
// (contract C8) for C to deliver portal push + FR-55 WhatsApp status
// messages. Publish failure is logged, never blocks the decision (NFR-7).
type cardPaymentEvent struct {
	SubscriberID string `json:"subscriber_id"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
}

func (m *Module) publishCardEvent(ctx context.Context, subscriberID, state, reason string) {
	if m.rdb == nil {
		return
	}
	buf, err := json.Marshal(cardPaymentEvent{SubscriberID: subscriberID, State: state, Reason: reason})
	if err != nil {
		m.log.Error("billing: marshal billing.card_payment event failed", "error", err)
		return
	}
	if err := m.rdb.Publish(ctx, "billing.card_payment", buf).Err(); err != nil {
		m.log.Warn("billing: publish billing.card_payment failed", "error", err)
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
