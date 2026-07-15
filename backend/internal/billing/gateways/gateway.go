// Package gateways defines the pluggable e-wallet payment interface (Phase 4,
// contract C3; sub-PRD 05 FR-23.1) and the shared types every adapter
// (mock, zaincash, …) implements. It has no dependency on internal/billing —
// billing imports gateways, never the reverse — so adapters stay isolated
// packages that can ship independently (FR-23.5).
package gateways

import (
	"context"
	"net/http"
)

// State is a payment's lifecycle state as the gateway sees it. Adapters map
// their own vocabulary onto these four values; the pending->confirmed
// transition and everything after it (renewal) is billing's job, not the
// adapter's.
type State string

const (
	StatePending   State = "pending"
	StateConfirmed State = "confirmed"
	StateFailed    State = "failed"
	StateExpired   State = "expired"
)

// Intent is what billing asks a gateway to create a payment for. ID is
// billing's own payment_intents.id, passed to the gateway as the merchant
// order/reference id so the callback and QueryStatus can correlate back to
// this exact intent without a lookup table in the adapter.
type Intent struct {
	ID           string
	SubscriberID string
	ProfileID    string
	AmountIQD    int64
}

// CallbackResult is what VerifyCallback returns once a gateway's webhook
// request has been authenticated. OrderID is billing's intent id (echoed back
// by the gateway); GatewayRef is the gateway's own transaction reference,
// recorded for QueryStatus polling and support lookups. AmountIQD is the
// gateway-reported amount, cross-checked by billing against the intent's
// recorded amount before any renewal runs (tamper/mismatch guard).
type CallbackResult struct {
	OrderID    string
	GatewayRef string
	State      State
	AmountIQD  int64
}

// PaymentGateway is the frozen C3 interface (sub-PRD 05 FR-23.1). Every
// method is safe to call concurrently.
type PaymentGateway interface {
	// Name is the gateway's config/route key (e.g. "mock", "zaincash"); it
	// appears in payment_intents.gateway, the /payments/{gateway}/... routes,
	// and the FR-19.3 renewal source "portal-<gateway>".
	Name() string
	// CreatePayment starts a payment and returns where the subscriber's
	// browser/app should go next plus the gateway's own reference for status
	// polling.
	CreatePayment(ctx context.Context, in Intent) (redirectURL, gatewayRef string, err error)
	// VerifyCallback authenticates an inbound webhook request (signature per
	// the gateway's own spec) and extracts the outcome. It must be safe to
	// call repeatedly for the same underlying event (the gateway may retry
	// its own webhook) — billing's own idempotency guard is what prevents a
	// double-renewal, not this method.
	VerifyCallback(ctx context.Context, r *http.Request) (CallbackResult, error)
	// QueryStatus is the reconciliation poll for an intent whose callback
	// never arrived (sub-PRD 05 AC-23b).
	QueryStatus(ctx context.Context, gatewayRef string) (State, error)
}
