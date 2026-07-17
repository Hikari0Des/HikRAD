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
// F's client (frontend/portal/src/api/vouchers.ts RenewResult) since portalapi
// serializes this struct directly. v2 phase 4 (FR-69.1): price_iqd -> price +
// currency — a breaking rename, the frontend type updates in the same phase.
type RenewResult struct {
	LedgerTxID   string    `json:"ledger_tx_id"`
	ReceiptNo    string    `json:"receipt_no"`
	NewExpiresAt time.Time `json:"new_expires_at"`
	Price        int64     `json:"price"`
	Currency     string    `json:"currency"`
	CoAResult    string    `json:"coa_result"`
}

func fromInternal(r renewResult) RenewResult {
	return RenewResult{
		LedgerTxID: r.LedgerTxID, ReceiptNo: r.ReceiptNo,
		NewExpiresAt: r.NewExpiresAt, Price: r.price, Currency: r.Currency, CoAResult: r.CoAResult,
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

// PortalPaymentEntry is one row of GET /portal/payments (own ledger slice),
// shape matched to F's already-written client (frontend/portal/src/api/usage.ts).
type PortalPaymentEntry struct {
	ID        string
	At        time.Time
	Type      string
	Amount    int64
	Currency  string
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
		out[i] = PortalPaymentEntry{ID: it.ID, At: it.At, Type: it.Type, Amount: it.Amount, Currency: it.Currency, Source: it.Source, Reference: it.Reference}
	}
	return out, next, nil
}

// --- Unified payment tickets (v2-2, C4/C5/C13) ------------------------------

// UploadedFile is the cross-package attachment shape portalapi builds from a
// multipart form part.
type UploadedFile struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ResolvePayMethods is the portal Pay screen's tile list (C4/C13). PayMethod
// itself is method_settings.go's already-exported type — same package, no
// wrapping needed.
func ResolvePayMethods(ctx context.Context, subscriberID string) ([]PayMethod, error) {
	if singleton == nil {
		return nil, ErrNotReady
	}
	return resolvePayMethods(ctx, singleton.db, subscriberID)
}

// SubmitTicketRequest is the cross-package submission shape (C5/C13),
// assembled by portalapi from a multipart form.
type SubmitTicketRequest struct {
	SubscriberID string
	MethodKey    string
	// Provider fields (kind="provider" only):
	Amount            int64
	TransferReference string
	TransferDate      *time.Time
	Note              string
	Attachments       []UploadedFile
	// Scratch-card fields (kind="scratch_card" only):
	CardType string
	CardCode string
}

// TicketSubmitResult is the cross-package submission result shape.
type TicketSubmitResult struct {
	ID             string
	State          string
	TrialGranted   bool
	TrialExpiresAt *time.Time
}

var (
	ErrMethodNotAllowed   = errors.New("billing: payment method not enabled for this subscriber")
	ErrTicketPending      = errors.New("billing: a payment ticket is already pending")
	ErrCardTypeNotAllowed = errors.New("billing: card type not allowed")
)

// SubmitTicket is the portal-facing unified submission (FR-78.2, C5/C13).
func SubmitTicket(ctx context.Context, req SubmitTicketRequest) (TicketSubmitResult, error) {
	if singleton == nil {
		return TicketSubmitResult{}, ErrNotReady
	}
	files := make([]uploadedFile, len(req.Attachments))
	for i, f := range req.Attachments {
		files[i] = uploadedFile{Filename: f.Filename, ContentType: f.ContentType, Data: f.Data}
	}
	r, err := singleton.submitTicket(ctx, submitTicketParams{
		SubscriberID: req.SubscriberID, MethodKey: req.MethodKey,
		Amount: req.Amount, TransferReference: req.TransferReference, TransferDate: req.TransferDate,
		Note: req.Note, Attachments: files, CardType: req.CardType, CardCode: req.CardCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, errMethodNotAllowed):
			return TicketSubmitResult{}, ErrMethodNotAllowed
		case errors.Is(err, errTicketPending):
			return TicketSubmitResult{}, ErrTicketPending
		case errors.Is(err, errCardTypeNotAllowed):
			return TicketSubmitResult{}, ErrCardTypeNotAllowed
		default:
			return TicketSubmitResult{}, translateRenewErr(err)
		}
	}
	return TicketSubmitResult{ID: r.ID, State: r.State, TrialGranted: r.TrialGranted, TrialExpiresAt: r.TrialExpiresAt}, nil
}

// MyTicket is the portal's own latest-record view (C13's "pending — under
// review" banner, generalizing cardpay.go's MyCardPayment).
type MyTicket struct {
	ID             string
	MethodKey      string
	State          string
	TrialGranted   bool
	TrialExpiresAt time.Time
	RejectReason   string
	CreatedAt      time.Time
}

// LatestTicket returns subscriberID's single most recent ticket (any
// method/state), or ok=false when they have none.
func LatestTicket(ctx context.Context, subscriberID string) (MyTicket, bool, error) {
	if singleton == nil {
		return MyTicket{}, false, ErrNotReady
	}
	v, ok, err := singleton.latestTicket(ctx, subscriberID)
	if err != nil || !ok {
		return MyTicket{}, false, err
	}
	return MyTicket{
		ID: v.ID, MethodKey: v.MethodKey, State: v.State, TrialGranted: v.TrialGranted,
		TrialExpiresAt: v.TrialExpiresAt, RejectReason: v.RejectReason, CreatedAt: v.CreatedAt,
	}, true, nil
}
