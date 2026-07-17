package billing

// Unified payment tickets (v2-2, FR-77-79, contracts C5-C9). Generalizes
// cardpay.go's card_payments flow to every method: a subscriber submits
// (transfer proof + attachments, or a scratch-card code) and gets an
// immediate 1-day provisional renewal via the EXACT SAME trial mechanism
// FR-59.1 always used — one renew() call, reused not reimplemented, so the
// "byte-identical trial timing" gate item has a mechanical reason to hold.
//
// Trial eligibility (FR-78.3) supersedes FR-59.4's old fixed cooldown: a
// rejected ticket may be resubmitted immediately, but earns a trial only if
// the subscriber's most recently DECIDED ticket (if any) was 'approved' —
// tickets are strictly sequential (one pending at a time, enforced by the
// partial unique index), so "most recent ticket" and "most recently decided
// ticket" are the same question.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
)

const ticketTrialDays = 1

var (
	errMethodNotAllowed   = errors.New("billing: payment method not enabled for this subscriber")
	errTicketPending      = errors.New("billing: a payment ticket is already pending")
	errTicketNotFound     = errors.New("billing: payment ticket not found")
	errTicketNotPending   = errors.New("billing: payment ticket is not pending")
	errCardTypeNotAllowed = errors.New("billing: card type not allowed")
)

const (
	keyCardTypes = "card_payments.types" // []string, default {"zain","asiacell"}
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

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// trialEligible reports whether subscriberID's next submission should grant
// a trial day (FR-78.3): true unless their most recently decided ticket is
// 'rejected'. Approval resets eligibility; rejection alone never blocks
// resubmission, only the trial grant.
func trialEligible(ctx context.Context, db dbQuerier, subscriberID string) (bool, error) {
	var state string
	err := db.QueryRow(ctx,
		`SELECT state FROM payment_tickets WHERE subscriber_id = $1::uuid AND state IN ('approved','rejected')
		  ORDER BY decided_at DESC LIMIT 1`, subscriberID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return state == "approved", nil
}

// dbQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so trialEligible
// can run either inside or outside a transaction.
type dbQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type uploadedFile struct {
	Filename    string
	ContentType string
	Data        []byte
}

type submitTicketParams struct {
	SubscriberID string
	MethodKey    string
	// Provider fields (kind="provider" only):
	Amount            int64
	TransferReference string
	TransferDate      *time.Time
	Note              string
	Attachments       []uploadedFile
	// Scratch-card fields (kind="scratch_card" only):
	CardType string
	CardCode string
}

type ticketSubmitResult struct {
	ID             string
	State          string
	TrialGranted   bool
	TrialExpiresAt *time.Time
}

// submitTicket is the portal-facing entry (via portal_seam.go): re-validates
// the method server-side (never trusts a client-claimed method the
// subscriber's manager didn't actually enable), atomically creates the
// pending row, and — when trial-eligible — runs the exact FR-59.1 trial
// renewal.
func (m *Module) submitTicket(ctx context.Context, p submitTicketParams) (ticketSubmitResult, error) {
	methods, err := resolvePayMethods(ctx, m.db, p.SubscriberID)
	if err != nil {
		return ticketSubmitResult{}, err
	}
	var matched *PayMethod
	for i := range methods {
		if methods[i].Key == p.MethodKey {
			matched = &methods[i]
			break
		}
	}
	if matched == nil {
		return ticketSubmitResult{}, errMethodNotAllowed
	}

	var profileID, profCurrency string
	var profPrice int64
	err = m.db.QueryRow(ctx, `SELECT profile_id::text FROM subscribers WHERE id = $1::uuid`, p.SubscriberID).Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketSubmitResult{}, errNoSubscriber
	}
	if err != nil {
		return ticketSubmitResult{}, err
	}
	if profileID == "" {
		return ticketSubmitResult{}, errNoProfile
	}
	if err := m.db.QueryRow(ctx, `SELECT price, currency FROM profiles WHERE id = $1::uuid`, profileID).
		Scan(&profPrice, &profCurrency); err != nil {
		return ticketSubmitResult{}, err
	}

	eligible, err := trialEligible(ctx, m.db, p.SubscriberID)
	if err != nil {
		return ticketSubmitResult{}, err
	}

	amount := profPrice
	currency := profCurrency
	var transferRef, transferDateSQL any
	var note string
	methodDetail := map[string]any{}

	switch matched.Kind {
	case "provider":
		if p.Amount > 0 {
			amount = p.Amount // the subscriber's own claim, informational — the
			// actual renewal charge always resolves from the profile (below),
			// never from this self-reported figure.
		}
		transferRef = p.TransferReference
		if p.TransferDate != nil {
			transferDateSQL = *p.TransferDate
		}
		note = p.Note
	case methodKeyScratchCard:
		if !containsStr(m.cardTypes(ctx), p.CardType) {
			return ticketSubmitResult{}, errCardTypeNotAllowed
		}
		codeEnc, err := crypto.Encrypt([]byte(p.CardCode))
		if err != nil {
			return ticketSubmitResult{}, err
		}
		methodDetail["card_type"] = p.CardType
		methodDetail["card_code_enc"] = base64.StdEncoding.EncodeToString(codeEnc)
	}

	var providerID *string
	if matched.Kind == "provider" {
		id := matched.Key
		providerID = &id
	}
	detailJSON, _ := json.Marshal(methodDetail)

	var ticketID string
	err = m.db.QueryRow(ctx,
		`INSERT INTO payment_tickets (subscriber_id, profile_id, method_key, provider_id, amount, currency,
		                              transfer_reference, transfer_date, note, method_detail)
		 VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, $8, $9, $10)
		 RETURNING id::text`,
		p.SubscriberID, profileID, p.MethodKey, providerID, amount, currency,
		transferRef, transferDateSQL, note, detailJSON).Scan(&ticketID)
	if isUniqueViolation(err) {
		return ticketSubmitResult{}, errTicketPending
	}
	if err != nil {
		return ticketSubmitResult{}, err
	}
	if err := m.insertTicketEvent(ctx, ticketID, "submitted", "", ""); err != nil {
		return ticketSubmitResult{}, err
	}

	if err := m.storeAttachments(ctx, ticketID, p.Attachments); err != nil {
		// NFR-7: a file-write failure after a committed ticket must not roll
		// back the ticket or block the trial below — logged as an event for
		// the reviewer to notice, never a fatal error for the subscriber.
		_ = m.insertTicketEvent(ctx, ticketID, "attachment_failed", "", err.Error())
	}

	res := ticketSubmitResult{ID: ticketID, State: "pending"}
	if !eligible {
		m.publishTicketEvent(ctx, p.SubscriberID, ticketID, "submitted", "")
		return res, nil
	}

	days := ticketTrialDays
	rr, err := m.renew(ctx, renewParams{
		subscriberID: p.SubscriberID, profileID: profileID,
		source: "ticket-trial", ledgerType: "adjustment", method: "ticket-trial",
		reference: ticketID, chargeBalance: false, enforceBalance: false,
		durationOverrideDays: &days,
		idemKey:              "ticket_trial:" + ticketID,
	})
	if err != nil {
		return ticketSubmitResult{}, err
	}
	if _, err := m.db.Exec(ctx,
		`UPDATE payment_tickets SET trial_ledger_tx_id = $2::uuid, updated_at = now() WHERE id = $1::uuid`,
		ticketID, rr.LedgerTxID); err != nil {
		return ticketSubmitResult{}, err
	}
	if err := m.insertTicketEvent(ctx, ticketID, "trial_granted", "", ""); err != nil {
		return ticketSubmitResult{}, err
	}
	res.TrialGranted = true
	exp := rr.NewExpiresAt
	res.TrialExpiresAt = &exp
	m.publishTicketEvent(ctx, p.SubscriberID, ticketID, "submitted", "")
	return res, nil
}

// latestTicketRow is subscriberID's single most recent ticket (any method,
// any state) — backs the portal's "pending ISP verification" banner
// (FR-42.3), generalizing cardpay.go's latestCardPayment. TrialExpiresAt is
// derived (created_at + the fixed trial constant), not stored — the fixed
// grant length was never a per-row fact.
type latestTicketRow struct {
	ID             string
	MethodKey      string
	State          string
	TrialGranted   bool
	TrialExpiresAt time.Time
	RejectReason   string
	CreatedAt      time.Time
}

func (m *Module) latestTicket(ctx context.Context, subscriberID string) (latestTicketRow, bool, error) {
	var v latestTicketRow
	var trialLedgerTxID *string
	var rejectReason *string
	err := m.db.QueryRow(ctx,
		`SELECT id::text, method_key, state, trial_ledger_tx_id::text, reject_reason, created_at
		   FROM payment_tickets WHERE subscriber_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, subscriberID).
		Scan(&v.ID, &v.MethodKey, &v.State, &trialLedgerTxID, &rejectReason, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return latestTicketRow{}, false, nil
	}
	if err != nil {
		return latestTicketRow{}, false, err
	}
	v.CreatedAt = v.CreatedAt.UTC()
	v.TrialGranted = trialLedgerTxID != nil
	v.TrialExpiresAt = v.CreatedAt.AddDate(0, 0, ticketTrialDays)
	v.RejectReason = strOr(rejectReason, "")
	return v, true, nil
}

// insertTicketEvent appends one payment_ticket_events row outside any
// caller-managed transaction (used by submitTicket/approveTicket, which
// don't hold a manual tx of their own — the renewal's own transaction has
// already committed by the time these run). actorManagerID "" -> NULL.
func (m *Module) insertTicketEvent(ctx context.Context, ticketID, eventType, actorManagerID, note string) error {
	_, err := m.db.Exec(ctx,
		`INSERT INTO payment_ticket_events (ticket_id, event_type, actor_manager_id, note)
		 VALUES ($1::uuid, $2, NULLIF($3,'')::uuid, $4)`,
		ticketID, eventType, actorManagerID, note)
	return err
}

// approveTicket runs the full FR-19 renewal anchored at the trial's start
// instant (FR-59.2's exact anchoring, generalized), now threading v2-9's
// wholesale/retail split (C7): the acting manager's balance debits their
// resolved wholesale price when one applies, while the subscriber's own
// payment/receipt still shows retail — the SAME renew() call a normal panel
// renewal makes, not a special case for this payment method.
func (m *Module) approveTicket(ctx context.Context, ticketID, approverID string) (renewResult, error) {
	var subscriberID, profileID, methodKey, state string
	var trialStart time.Time
	err := m.db.QueryRow(ctx,
		`SELECT subscriber_id::text, profile_id::text, method_key, state, created_at
		   FROM payment_tickets WHERE id = $1::uuid`, ticketID).
		Scan(&subscriberID, &profileID, &methodKey, &state, &trialStart)
	if errors.Is(err, pgx.ErrNoRows) {
		return renewResult{}, errTicketNotFound
	}
	if err != nil {
		return renewResult{}, err
	}
	if state != "pending" {
		return renewResult{}, errTicketNotPending
	}

	ts := trialStart.UTC()
	rr, err := m.renew(ctx, renewParams{
		subscriberID: subscriberID, profileID: profileID, actorManagerID: approverID,
		source: "ticket-" + methodKey, ledgerType: "renewal", method: "ticket-" + methodKey,
		reference: ticketID, chargeBalance: true, enforceBalance: false,
		baseOverride: &ts,
		idemKey:      "ticket_approve:" + ticketID,
	})
	if err != nil {
		return renewResult{}, err
	}

	ct, err := m.db.Exec(ctx,
		`UPDATE payment_tickets SET state = 'approved', approve_ledger_tx_id = $2::uuid,
		        decided_by = NULLIF($3,'')::uuid, decided_at = now(), updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`,
		ticketID, rr.LedgerTxID, approverID)
	if err != nil {
		return renewResult{}, err
	}
	if ct.RowsAffected() == 0 {
		return renewResult{}, errTicketNotPending
	}
	if err := m.insertTicketEvent(ctx, ticketID, "approved", approverID, ""); err != nil {
		return renewResult{}, err
	}
	m.publishTicketEventDecided(ctx, subscriberID, ticketID, "approved", "", approverID)
	return rr, nil
}

// rejectTicket reverses the trial (nets to 0 — AC-59b's original guarantee,
// generalized), rolls the expiry back by the trial grant floored at now, and
// re-applies FR-9 via CoA if the rollback lands the subscriber expired.
func (m *Module) rejectTicket(ctx context.Context, ticketID, approverID, reason string) error {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var subscriberID, state string
	var trialLedgerTxID *string
	err = tx.QueryRow(ctx,
		`SELECT subscriber_id::text, state, trial_ledger_tx_id::text FROM payment_tickets WHERE id = $1::uuid FOR UPDATE`,
		ticketID).Scan(&subscriberID, &state, &trialLedgerTxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errTicketNotFound
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return errTicketNotPending
	}

	if trialLedgerTxID != nil {
		var trialCurrency string
		if err := tx.QueryRow(ctx, `SELECT currency FROM ledger_transactions WHERE id = $1::uuid`, *trialLedgerTxID).
			Scan(&trialCurrency); err != nil {
			return err
		}
		if _, err := insertLedger(ctx, tx, ledgerEntry{
			Type: "refund", Amount: 0, Currency: trialCurrency, ActorManagerID: approverID, SubscriberID: subscriberID,
			Source: "ticket-reject", ReversesID: *trialLedgerTxID, Note: reason,
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
	newExpiry := rollbackExpiry(dbNow, curExpires, ticketTrialDays)
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
		`UPDATE payment_tickets SET state = 'rejected', decided_by = NULLIF($2,'')::uuid,
		        decided_at = now(), reject_reason = $3, updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`, ticketID, approverID, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errTicketNotPending
	}
	// The 'rejected' event is written in the SAME transaction as the state
	// change it records (contract C8: the timeline can never drift from the
	// state it describes) — unlike approve, which threads through renew()'s
	// own internal transaction and so records its event just after commit.
	if _, err := tx.Exec(ctx,
		`INSERT INTO payment_ticket_events (ticket_id, event_type, actor_manager_id, note)
		 VALUES ($1::uuid, 'rejected', NULLIF($2,'')::uuid, $3)`,
		ticketID, approverID, reason); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	_ = radius.InvalidatePolicy(subscriberID)
	if nowExpired {
		m.enforceExpiredCoA(ctx, subscriberID, behavior, strOr(expiredPool, ""))
	}
	m.publishTicketEventDecided(ctx, subscriberID, ticketID, "rejected", reason, approverID)
	return nil
}
