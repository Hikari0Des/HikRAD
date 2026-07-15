package billing

// Payment intent lifecycle (contract C3, FR-23.2): pending -> confirmed ->
// renewed (terminal success) or -> failed / expired (terminal failure).
// Idempotency against double-renewal does NOT rely on the state column alone
// (state transitions race just like the callback itself) — it rides on the
// existing renewal_idempotency table (0204) keyed by the intent id, exactly
// the mechanism the panel/voucher renewal path already uses. So a callback
// replayed N times, or a callback racing the reconciliation poll, always
// converges on exactly one renewal: every racer calls billing's renew with
// the same idempotency key, and only the first one to win the underlying
// PK-insert actually charges; the rest read back its stored result.

import (
	"context"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/billing/gateways"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

var (
	errUnknownIntent  = errors.New("billing: unknown payment intent")
	errTerminalIntent = errors.New("billing: payment intent already finalized")
	errAmountMismatch = errors.New("billing: callback amount does not match the intent")
)

// resolvePortalPrice mirrors renewInTx's price resolution (FR-19.2) as a
// read-only lookup — the actual lock + charge happens inside renewInTx itself
// when the intent later confirms. profileID "" keeps the subscriber's current
// profile.
func (m *Module) resolvePortalPrice(ctx context.Context, subscriberID, profileID string) (price int64, resolvedProfileID string, err error) {
	var curProfile *string
	var priceOverride *int64
	err = m.db.QueryRow(ctx, `SELECT profile_id::text, price_override FROM subscribers WHERE id = $1::uuid`, subscriberID).
		Scan(&curProfile, &priceOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", errNoSubscriber
	}
	if err != nil {
		return 0, "", err
	}
	resolvedProfileID = profileID
	if resolvedProfileID == "" {
		resolvedProfileID = strOr(curProfile, "")
	}
	if resolvedProfileID == "" {
		return 0, "", errNoProfile
	}
	var profPrice int64
	var archived bool
	err = m.db.QueryRow(ctx, `SELECT price_iqd, archived FROM profiles WHERE id = $1::uuid`, resolvedProfileID).
		Scan(&profPrice, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", errNoProfile
	}
	if err != nil {
		return 0, "", err
	}
	if archived {
		return 0, "", errProfileArchived
	}
	return resolvePrice(priceOverride, profPrice), resolvedProfileID, nil
}

// createIntent starts a new payment attempt (C3: POST /portal/payments/{gw}/create).
func (m *Module) createIntent(ctx context.Context, subscriberID, gatewayName, profileID string) (intentID, redirectURL string, err error) {
	gw, err := m.resolveGateway(ctx, gatewayName)
	if err != nil {
		return "", "", err
	}
	price, resolvedProfile, err := m.resolvePortalPrice(ctx, subscriberID, profileID)
	if err != nil {
		return "", "", err
	}
	err = m.db.QueryRow(ctx,
		`INSERT INTO payment_intents (subscriber_id, profile_id, gateway, amount_iqd)
		 VALUES ($1::uuid, $2::uuid, $3, $4) RETURNING id::text`,
		subscriberID, resolvedProfile, gatewayName, price).Scan(&intentID)
	if err != nil {
		return "", "", err
	}
	redirectURL, gatewayRef, cpErr := gw.CreatePayment(ctx, gateways.Intent{
		ID: intentID, SubscriberID: subscriberID, ProfileID: resolvedProfile, AmountIQD: price,
	})
	if cpErr != nil {
		_, _ = m.db.Exec(ctx, `UPDATE payment_intents SET state = 'failed', updated_at = now() WHERE id = $1::uuid`, intentID)
		return "", "", cpErr
	}
	if _, err := m.db.Exec(ctx, `UPDATE payment_intents SET gateway_ref = $2, updated_at = now() WHERE id = $1::uuid`,
		intentID, gatewayRef); err != nil {
		return "", "", err
	}
	return intentID, redirectURL, nil
}

// intentView is the C3 poll response shape (GET /portal/payments/intents/{id}).
type intentView struct {
	ID           string     `json:"id"`
	Gateway      string     `json:"gateway"`
	State        string     `json:"state"`
	AmountIQD    int64      `json:"amount_iqd"`
	GatewayRef   string     `json:"gateway_ref"`
	NewExpiresAt *time.Time `json:"new_expires_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// getIntent is IDOR-scoped: the row must belong to subscriberID.
func (m *Module) getIntent(ctx context.Context, id, subscriberID string) (intentView, error) {
	var v intentView
	var ledgerTxID, gatewayRef *string
	err := m.db.QueryRow(ctx,
		`SELECT id::text, gateway, state, amount_iqd, created_at, ledger_tx_id::text, gateway_ref
		   FROM payment_intents WHERE id = $1::uuid AND subscriber_id = $2::uuid`,
		id, subscriberID).Scan(&v.ID, &v.Gateway, &v.State, &v.AmountIQD, &v.CreatedAt, &ledgerTxID, &gatewayRef)
	if err != nil {
		return intentView{}, err
	}
	v.GatewayRef = strOr(gatewayRef, "")
	if v.State == "renewed" && ledgerTxID != nil {
		var exp time.Time
		if err := m.db.QueryRow(ctx, `SELECT s.expires_at FROM subscribers s
			JOIN payment_intents pi ON pi.subscriber_id = s.id WHERE pi.id = $1::uuid`, id).Scan(&exp); err == nil {
			exp = exp.UTC()
			v.NewExpiresAt = &exp
		}
	}
	return v, nil
}

// portalPaymentSummary is one row of GET /portal/payments (own ledger slice,
// C2 FR-41.3) — the customer-facing gross payment record, not the internal
// balance-movement ledger. id/type/reference are not frozen by C2 (only the
// route is); this shape mirrors F's already-written client exactly (see
// frontend/portal/src/api/usage.ts) rather than inventing a divergent one.
type portalPaymentSummary struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Type      string    `json:"type"` // payments.method: renewal|voucher_redeem|portal-<gw>|card-trial|card-<type>|refund
	AmountIQD int64     `json:"amount_iqd"`
	Source    string    `json:"source"`
	Reference string    `json:"reference"`
}

func (m *Module) portalPayments(ctx context.Context, subscriberID string, page httpapi.PageRequest) ([]portalPaymentSummary, string, error) {
	var cursorTS *time.Time
	var cursorNo *string
	if len(page.Cursor) == 2 {
		t, terr := time.Parse(time.RFC3339Nano, page.Cursor[0])
		if terr != nil {
			return nil, "", httpapi.ErrBadCursor
		}
		cursorTS, cursorNo = &t, &page.Cursor[1]
	} else if page.Cursor != nil {
		return nil, "", httpapi.ErrBadCursor
	}
	rows, err := m.db.Query(ctx,
		`SELECT pay.receipt_no, pay.at, pay.method, pay.amount_iqd, pay.source, COALESCE(l.reference,'')
		   FROM payments pay
		   LEFT JOIN ledger_transactions l ON l.id = pay.ledger_tx_id
		  WHERE pay.subscriber_id = $1::uuid
		    AND ($2::timestamptz IS NULL OR (pay.at, pay.receipt_no) < ($2::timestamptz, $3))
		  ORDER BY pay.at DESC, pay.receipt_no DESC
		  LIMIT $4`,
		subscriberID, cursorTS, cursorNo, page.Limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]portalPaymentSummary, 0, page.Limit)
	for rows.Next() {
		var s portalPaymentSummary
		if err := rows.Scan(&s.ID, &s.At, &s.Type, &s.AmountIQD, &s.Source, &s.Reference); err != nil {
			return nil, "", err
		}
		s.At = s.At.UTC()
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = httpapi.EncodeCursor(last.At.Format(time.RFC3339Nano), last.ID)
	}
	return items, next, nil
}

// callbackResultFrom builds a gateways.CallbackResult from the mock adapter's
// signed simulate payload (payments_api.go's mockSimulateHandler).
func callbackResultFrom(orderID, gatewayRef, state string, amountIQD int64) gateways.CallbackResult {
	return gateways.CallbackResult{OrderID: orderID, GatewayRef: gatewayRef, State: gateways.State(state), AmountIQD: amountIQD}
}

// processCallback is the confirm+renew core, shared by the real webhook
// handler and the reconciliation worker's QueryStatus poll. It is safe to
// call any number of times for the same intent — see the package doc above.
func (m *Module) processCallback(ctx context.Context, gatewayName string, res gateways.CallbackResult) error {
	var intent struct {
		ID           string
		SubscriberID string
		ProfileID    string
		AmountIQD    int64
		State        string
	}
	err := m.db.QueryRow(ctx,
		`SELECT id::text, subscriber_id::text, profile_id::text, amount_iqd, state
		   FROM payment_intents WHERE id = $1::uuid AND gateway = $2`,
		res.OrderID, gatewayName).
		Scan(&intent.ID, &intent.SubscriberID, &intent.ProfileID, &intent.AmountIQD, &intent.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return errUnknownIntent
	}
	if err != nil {
		return err
	}

	if intent.State == "renewed" {
		return nil // idempotent no-op (edge case: confirmed for an already-renewed intent)
	}
	if intent.State == "failed" || intent.State == "expired" {
		return errTerminalIntent
	}

	if res.State == gateways.StateFailed {
		_, err := m.db.Exec(ctx,
			`UPDATE payment_intents SET state = 'failed', gateway_ref = COALESCE(NULLIF($2,''), gateway_ref), updated_at = now()
			  WHERE id = $1::uuid AND state <> 'renewed'`, intent.ID, res.GatewayRef)
		return err
	}
	if res.State != gateways.StateConfirmed {
		return nil // still pending; nothing to do yet
	}

	// Tamper/mismatch guard (edge case): a gateway that reports an amount is
	// cross-checked; adapters that cannot report one send 0 and are trusted on
	// the intent's own recorded, pre-negotiated amount instead.
	if res.AmountIQD != 0 && res.AmountIQD != intent.AmountIQD {
		return errAmountMismatch
	}

	// First writer wins this CAS; replays/races just fall through to the
	// idempotent renew below regardless of who won it.
	if _, err := m.db.Exec(ctx,
		`UPDATE payment_intents SET state = 'confirmed', gateway_ref = COALESCE(NULLIF($2,''), gateway_ref), updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`, intent.ID, res.GatewayRef); err != nil {
		return err
	}

	rr, err := m.renew(ctx, renewParams{
		subscriberID:   intent.SubscriberID,
		profileID:      intent.ProfileID,
		source:         "portal-" + gatewayName,
		ledgerType:     "renewal",
		method:         "portal-" + gatewayName,
		reference:      intent.ID,
		chargeBalance:  false, // prepaid via the gateway itself, not a manager balance
		enforceBalance: false,
		idemKey:        "payment_intent:" + intent.ID,
	})
	if err != nil {
		return err
	}
	_, err = m.db.Exec(ctx,
		`UPDATE payment_intents SET state = 'renewed', ledger_tx_id = $2::uuid, updated_at = now()
		  WHERE id = $1::uuid AND state <> 'renewed'`, intent.ID, rr.LedgerTxID)
	return err
}

// --- Reconciliation worker (C3: QueryStatus for intents pending > 10 min,
// then hourly, expiring after 48 h) ---------------------------------------

const (
	reconcileTick     = 2 * time.Minute
	reconcileFirstAge = 10 * time.Minute
	reconcileInterval = 1 * time.Hour
	reconcileExpireAt = 48 * time.Hour
)

func (m *Module) runReconciliation(ctx context.Context) {
	t := time.NewTicker(reconcileTick)
	defer t.Stop()
	for {
		m.reconcileOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Module) reconcileOnce(ctx context.Context) {
	rows, err := m.db.Query(ctx,
		`SELECT id::text, gateway, gateway_ref, created_at, last_query_at
		   FROM payment_intents
		  WHERE state IN ('pending','confirmed') AND gateway_ref IS NOT NULL`)
	if err != nil {
		m.log.Warn("billing: reconciliation query failed", "error", err)
		return
	}
	type row struct {
		id, gateway, ref string
		createdAt        time.Time
		lastQueryAt      *time.Time
	}
	var due []row
	now := time.Now().UTC()
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.gateway, &r.ref, &r.createdAt, &r.lastQueryAt); err != nil {
			rows.Close()
			m.log.Warn("billing: reconciliation scan failed", "error", err)
			return
		}
		due = append(due, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		m.log.Warn("billing: reconciliation rows failed", "error", err)
		return
	}

	for _, r := range due {
		age := now.Sub(r.createdAt.UTC())
		if age >= reconcileExpireAt {
			_, _ = m.db.Exec(ctx,
				`UPDATE payment_intents SET state = 'expired', updated_at = now() WHERE id = $1::uuid AND state IN ('pending','confirmed')`, r.id)
			continue
		}
		if age < reconcileFirstAge {
			continue
		}
		if r.lastQueryAt != nil && now.Sub(r.lastQueryAt.UTC()) < reconcileInterval {
			continue
		}
		gw, err := m.resolveGatewayForCallback(ctx, r.gateway)
		if err != nil {
			continue
		}
		state, err := gw.QueryStatus(ctx, r.ref)
		_, _ = m.db.Exec(ctx, `UPDATE payment_intents SET last_query_at = now() WHERE id = $1::uuid`, r.id)
		if err != nil {
			continue
		}
		switch state {
		case gateways.StateConfirmed:
			if err := m.processCallback(ctx, r.gateway, gateways.CallbackResult{OrderID: r.id, GatewayRef: r.ref, State: gateways.StateConfirmed}); err != nil {
				m.log.Warn("billing: reconciliation confirm failed", "error", err, "intent", r.id)
			}
		case gateways.StateFailed:
			_, _ = m.db.Exec(ctx, `UPDATE payment_intents SET state = 'failed', updated_at = now() WHERE id = $1::uuid AND state <> 'renewed'`, r.id)
		}
	}
}
