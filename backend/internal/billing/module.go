// Package billing is the money core (Phase 3, Agent 4). It owns the single
// atomic renewal path every source converges on (FR-19), the append-only
// transaction ledger balances are DERIVED from (FR-24/FR-20), agent top-ups,
// printable receipts (FR-21), one-time voucher batches with race-proof single
// use (FR-22), and refunds as reversing entries (FR-25). The money invariants
// are absolute: no edits, only entries — enforced at the DB level (migrations
// 0200/0201) and never worked around here.
package billing

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	log      *slog.Logger
	settings platform.Settings
}

func (Module) Name() string { return "billing" }

// wireOnce guards the boot-time seam wiring (voucher authenticator, voucher
// prefix) so the repeated Register calls each httptest server makes don't
// re-install the seam or spawn duplicate goroutines.
var wireOnce sync.Once

// singleton is the cross-package seam (Phase 4, contract C2/C3/C8): portalapi
// imports this package directly (no cycle — billing never imports portalapi)
// and calls the exported wrappers in portal_seam.go, which dispatch onto this
// pointer. Same avoid-a-second-wiring-mechanism reasoning as radius's
// SetPolicyProvider/SetVoucherAuthenticator seams, just without an interface
// indirection since there's only ever one billing Module.
var singleton *Module

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.rdb = d.Redis
	m.log = d.Log
	m.settings = d.Settings
	singleton = m

	// Renewal — THE single money path (C2, FR-19.3). Every source (panel now;
	// voucher redeem below; portal/e-wallet Phase 4) converges on m.renew.
	r.With(auth.Require(auth.PermRenew)).Post("/api/v1/subscribers/{id}/renew", m.renewHandler)
	r.With(auth.Require(permRefund)).Post("/api/v1/subscribers/{id}/refund", m.refundHandler)

	// Balances (FR-20). Reading own balance needs only authentication; reading
	// another manager's or topping up needs the topup permission (checked inside).
	r.With(auth.Require("")).Get("/api/v1/managers/{id}/balance", m.balanceHandler)
	r.With(auth.Require(auth.PermTopup)).Post("/api/v1/managers/{id}/topup", m.topupHandler)

	// Vouchers (FR-22). Generation/void are batch-admin actions; the operator
	// redeem path reuses the renewal permission (agents redeem for subscribers).
	r.With(auth.Require(permVouchersView)).Get("/api/v1/vouchers/batches", m.listBatchesHandler)
	r.With(auth.Require(permVouchersView)).Get("/api/v1/vouchers/batches/{id}", m.batchDetailHandler)
	r.With(auth.Require(permVouchersCreate)).Post("/api/v1/vouchers/batches", m.createBatchHandler)
	r.With(auth.Require(permVouchersCreate)).Post("/api/v1/vouchers/batches/{id}/void", m.voidBatchHandler)
	r.With(auth.Require(auth.PermRenew)).Post("/api/v1/vouchers/redeem", m.redeemHandler)

	// Ledger (FR-24) + receipts (FR-21).
	r.With(auth.Require("reports.view")).Get("/api/v1/ledger", m.ledgerListHandler)
	r.With(auth.Require(auth.PermExport)).Get("/api/v1/ledger/export", m.ledgerExportHandler)
	r.With(auth.Require("reports.view")).Get("/api/v1/payments/{receipt_no}/receipt", m.receiptHandler)
	// Shareable print-only receipt (FR-21): unauthenticated, unguessable token.
	r.Get("/api/v1/receipts/{token}", m.sharedReceiptHandler)

	// Internal service surface (unproxied): subscriber counts for C's dashboard
	// and digests (frozen GET /internal/stats/subscribers).
	r.Get("/internal/stats/subscribers", m.subscriberStatsHandler)

	// Payment gateway layer (Phase 4, C3, FR-23): the public webhook every
	// gateway posts to, admin config, and the mock adapter's dev-only
	// simulator. Portal-facing create/poll/list-gateways routes live in
	// portalapi (subscriber-token auth) via the portal_seam.go wrappers.
	r.Post("/api/v1/payments/{gateway}/callback", m.paymentCallbackHandler)
	r.With(auth.Require(permGatewaysManage)).Get("/api/v1/payment-gateways", m.listGatewayConfigsHandler)
	r.With(auth.Require(permGatewaysManage)).Put("/api/v1/payment-gateways/{gateway}", m.putGatewayConfigHandler)
	r.Post("/api/v1/dev/mock-gateway/simulate", m.mockSimulateHandler)

	// Scratch-card payments (Phase 4, C8, FR-59): admin verification queue.
	// The portal-facing submit endpoint lives in portalapi.
	r.With(auth.Require(permCardPaymentsVerify)).Get("/api/v1/card-payments", m.listCardPaymentsHandler)
	r.With(auth.Require(permCardPaymentsVerify)).Post("/api/v1/card-payments/{id}/reveal", m.revealCardPaymentHandler)
	r.With(auth.Require(permCardPaymentsVerify)).Post("/api/v1/card-payments/{id}/approve", m.approveCardPaymentHandler)
	r.With(auth.Require(permCardPaymentsVerify)).Post("/api/v1/card-payments/{id}/reject", m.rejectCardPaymentHandler)

	wireOnce.Do(func() {
		// Reconciliation worker (C3): polls gateway QueryStatus for intents
		// stuck pending/confirmed. Runs for the process lifetime.
		go m.runReconciliation(context.Background())
		// B's hotspot-voucher login redeems through this seam (C3 internal redeem
		// API); B never imports billing.
		radius.SetVoucherAuthenticator(&voucherAuthenticator{m: m})
		// Constrain B's voucher-shape detection to the house prefix when set, so a
		// real username is never mistaken for a voucher (edge case).
		if p := m.getString(context.Background(), keyVoucherPrefix, ""); p != "" {
			radius.SetVoucherPrefix(p)
		}
	})
}

func init() { httpapi.Add(&Module{}) }

// internalError logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("billing: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
