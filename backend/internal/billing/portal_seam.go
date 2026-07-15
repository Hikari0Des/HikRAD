package billing

// Cross-package seam for portalapi (Phase 4, contracts C2/C3/C8). portalapi
// imports this package and calls these exported wrappers instead of the
// Module's unexported methods; everything still funnels through the single
// renewInTx/renewTx path (renew.go) so "every renewal source converges" holds
// for portal voucher redemption, e-wallet confirmation, and scratch-card
// trial/approve exactly as it already does for the panel and Hotspot vouchers.

import (
	"context"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

// ErrNotReady is returned by every seam function when the billing module has
// not finished Register yet (should not happen once the process is serving
// traffic — Register runs at boot before any route accepts a request).
var ErrNotReady = errors.New("billing: module not initialized")

// RenewRequest is the cross-package renewal request shape.
type RenewRequest struct {
	SubscriberID   string
	ProfileID      string // "" keeps the subscriber's current profile
	ActorManagerID string // "" for subscriber-initiated (portal) renewals
	Source         string // e.g. portal-mock, portal-zaincash, card-trial, card-zain
	LedgerType     string // renewal | adjustment
	Method         string
	Note           string
	Reference      string
	ChargeBalance  bool
	EnforceBalance bool
	IdemKey        string // required for anything replay-exposed (gateway callbacks)

	// DurationOverrideDays / BaseOverride: see renewParams in renew.go
	// (Phase 4 C8 scratch-card trial/approve anchoring).
	DurationOverrideDays *int
	BaseOverride         *time.Time
}

// RenewResult is the cross-package renewal result shape, JSON-tagged to match
// F's already-written client exactly (frontend/portal/src/api/vouchers.ts
// RenewResult) since portalapi serializes this struct directly.
type RenewResult struct {
	LedgerTxID   string    `json:"ledger_tx_id"`
	ReceiptNo    string    `json:"receipt_no"`
	NewExpiresAt time.Time `json:"new_expires_at"`
	PriceIQD     int64     `json:"price_iqd"`
	CoAResult    string    `json:"coa_result"`
}

func fromInternal(r renewResult) RenewResult {
	return RenewResult{
		LedgerTxID: r.LedgerTxID, ReceiptNo: r.ReceiptNo,
		NewExpiresAt: r.NewExpiresAt, PriceIQD: r.priceIQD, CoAResult: r.CoAResult,
	}
}

// Renew runs THE single renewal path (renew.go) for a cross-package caller.
func Renew(ctx context.Context, req RenewRequest) (RenewResult, error) {
	if singleton == nil {
		return RenewResult{}, ErrNotReady
	}
	res, err := singleton.renew(ctx, renewParams{
		subscriberID:         req.SubscriberID,
		profileID:            req.ProfileID,
		actorManagerID:       req.ActorManagerID,
		source:               req.Source,
		ledgerType:           req.LedgerType,
		method:               req.Method,
		note:                 req.Note,
		reference:            req.Reference,
		chargeBalance:        req.ChargeBalance,
		enforceBalance:       req.EnforceBalance,
		idemKey:              req.IdemKey,
		durationOverrideDays: req.DurationOverrideDays,
		baseOverride:         req.BaseOverride,
	})
	if err != nil {
		return RenewResult{}, translateRenewErr(err)
	}
	return fromInternal(res), nil
}

// RedeemOutcome mirrors the internal redeemOutcome for cross-package callers
// (portal voucher redemption, FR-42).
type RedeemOutcome int

const (
	RedeemOK RedeemOutcome = iota
	RedeemInvalid
	RedeemUsed
	RedeemExpired
	RedeemBatchVoid
)

func fromInternalOutcome(o redeemOutcome) RedeemOutcome {
	switch o {
	case redeemOK:
		return RedeemOK
	case redeemUsed:
		return RedeemUsed
	case redeemExpired:
		return RedeemExpired
	case redeemBatchVoid:
		return RedeemBatchVoid
	default:
		return RedeemInvalid
	}
}

// RedeemVoucher runs the self-targeted portal voucher redemption (FR-42,
// sub-PRD 05 FR-22.3): redeemerID is empty (no manager actor; the batch was
// already charged at generation, so this is balance-neutral) — same
// row-locked single-use path the operator/Hotspot paths use.
func RedeemVoucher(ctx context.Context, code, subscriberID string) (RenewResult, RedeemOutcome, error) {
	if singleton == nil {
		return RenewResult{}, RedeemInvalid, ErrNotReady
	}
	res, outcome, err := singleton.redeemVoucher(ctx, code, subscriberID, "")
	if err != nil {
		return RenewResult{}, fromInternalOutcome(outcome), translateRenewErr(err)
	}
	return fromInternal(res), fromInternalOutcome(outcome), nil
}

// Sentinel errors exposed for cross-package error mapping (HTTP status
// selection in portalapi mirrors billing's own renew_api.go).
var (
	ErrSubscriberNotFound = errors.New("billing: subscriber not found")
	ErrNoProfile          = errors.New("billing: no profile to renew")
	ErrProfileArchived    = errors.New("billing: profile is archived")
	ErrInsufficientFunds  = errors.New("billing: insufficient balance")
)

func translateRenewErr(err error) error {
	switch {
	case errors.Is(err, errNoSubscriber):
		return ErrSubscriberNotFound
	case errors.Is(err, errNoProfile):
		return ErrNoProfile
	case errors.Is(err, errProfileArchived):
		return ErrProfileArchived
	case errors.Is(err, errInsufficientFunds):
		return ErrInsufficientFunds
	default:
		return err
	}
}

// --- Gateway layer (C3) -----------------------------------------------------

// ListEnabledGateways is what the portal renewal screen offers.
func ListEnabledGateways(ctx context.Context) ([]string, error) {
	if singleton == nil {
		return nil, ErrNotReady
	}
	return singleton.listEnabledGateways(ctx)
}

var ErrGatewayUnavailable = errors.New("billing: gateway not available")

// CreatePaymentIntent starts an e-wallet payment (C3: POST /portal/payments/{gw}/create).
func CreatePaymentIntent(ctx context.Context, subscriberID, gateway, profileID string) (intentID, redirectURL string, err error) {
	if singleton == nil {
		return "", "", ErrNotReady
	}
	id, url, err := singleton.createIntent(ctx, subscriberID, gateway, profileID)
	if err != nil {
		if errors.Is(err, errGatewayDisabled) || errors.Is(err, errUnknownGateway) {
			return "", "", ErrGatewayUnavailable
		}
		return "", "", translateRenewErr(err)
	}
	return id, url, nil
}

// PaymentIntent is the cross-package poll response shape (C3: GET
// /portal/payments/intents/{id}).
type PaymentIntent struct {
	ID           string
	Gateway      string
	State        string
	AmountIQD    int64
	GatewayRef   string
	NewExpiresAt *time.Time
	CreatedAt    time.Time
}

var ErrIntentNotFound = errors.New("billing: payment intent not found")

// GetPaymentIntent is IDOR-scoped: the intent must belong to subscriberID.
func GetPaymentIntent(ctx context.Context, id, subscriberID string) (PaymentIntent, error) {
	if singleton == nil {
		return PaymentIntent{}, ErrNotReady
	}
	v, err := singleton.getIntent(ctx, id, subscriberID)
	if err != nil {
		return PaymentIntent{}, ErrIntentNotFound
	}
	return PaymentIntent{
		ID: v.ID, Gateway: v.Gateway, State: v.State, AmountIQD: v.AmountIQD, GatewayRef: v.GatewayRef,
		NewExpiresAt: v.NewExpiresAt, CreatedAt: v.CreatedAt,
	}, nil
}

// PortalPaymentEntry is one row of GET /portal/payments (own ledger slice),
// shape matched to F's already-written client (frontend/portal/src/api/usage.ts).
type PortalPaymentEntry struct {
	ID        string
	At        time.Time
	Type      string
	AmountIQD int64
	Source    string
	Reference string
}

// PortalPayments returns subscriberID's own payment history, paginated.
func PortalPayments(ctx context.Context, subscriberID string, page httpapi.PageRequest) ([]PortalPaymentEntry, string, error) {
	if singleton == nil {
		return nil, "", ErrNotReady
	}
	items, next, err := singleton.portalPayments(ctx, subscriberID, page)
	if err != nil {
		return nil, "", err
	}
	out := make([]PortalPaymentEntry, len(items))
	for i, it := range items {
		out[i] = PortalPaymentEntry{ID: it.ID, At: it.At, Type: it.Type, AmountIQD: it.AmountIQD, Source: it.Source, Reference: it.Reference}
	}
	return out, next, nil
}

// --- Scratch-card payments (C8) --------------------------------------------

// CardSubmitResult is the C8 submit response shape.
type CardSubmitResult struct {
	ID             string
	State          string
	TrialExpiresAt time.Time
}

var (
	ErrCardTypeNotAllowed = errors.New("billing: card type not allowed")
	ErrCardPending        = errors.New("billing: a card payment is already pending")
)

// CardCooldownError carries the exact instant card submissions unblock again
// (FR-59.4), so the portal can surface it rather than a vague message.
type CardCooldownError struct{ RetryAt time.Time }

func (e *CardCooldownError) Error() string {
	return "billing: card submissions are blocked until " + e.RetryAt.Format(time.RFC3339)
}

// SubmitCard is the portal-facing card-payment submission (FR-59.1).
func SubmitCard(ctx context.Context, subscriberID, cardType, code string) (CardSubmitResult, error) {
	if singleton == nil {
		return CardSubmitResult{}, ErrNotReady
	}
	r, err := singleton.submitCard(ctx, subscriberID, cardType, code)
	if err != nil {
		var cd *cardCooldownError
		switch {
		case errors.Is(err, errCardTypeNotAllowed):
			return CardSubmitResult{}, ErrCardTypeNotAllowed
		case errors.Is(err, errCardPending):
			return CardSubmitResult{}, ErrCardPending
		case errors.As(err, &cd):
			return CardSubmitResult{}, &CardCooldownError{RetryAt: cd.RetryAt}
		default:
			return CardSubmitResult{}, translateRenewErr(err)
		}
	}
	return CardSubmitResult{ID: r.ID, State: r.State, TrialExpiresAt: r.TrialExpiresAt}, nil
}

// CardTypes returns the settings-configurable list of accepted card types
// (portal's card-type picker; no frozen route in C8, narrowest addition —
// see status note).
func CardTypes(ctx context.Context) []string {
	if singleton == nil {
		return defaultCardTypes
	}
	return singleton.cardTypes(ctx)
}

// MyCardPayment is the portal's own latest-record view.
type MyCardPayment struct {
	ID             string
	CardType       string
	State          string
	TrialExpiresAt time.Time
	RejectReason   string
	CreatedAt      time.Time
}

// LatestCardPayment returns subscriberID's single most recent card payment
// (any state), or ok=false when they have none — backs the portal's "pending
// ISP verification" banner (no frozen route in C8, narrowest addition — see
// status note).
func LatestCardPayment(ctx context.Context, subscriberID string) (MyCardPayment, bool, error) {
	if singleton == nil {
		return MyCardPayment{}, false, ErrNotReady
	}
	v, ok, err := singleton.latestCardPayment(ctx, subscriberID)
	if err != nil || !ok {
		return MyCardPayment{}, false, err
	}
	return MyCardPayment{
		ID: v.ID, CardType: v.CardType, State: v.State, TrialExpiresAt: v.TrialExpiresAt,
		RejectReason: v.RejectReason, CreatedAt: v.CreatedAt,
	}, true, nil
}
