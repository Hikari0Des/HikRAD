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
	r.With(auth.Require("")).Get("/api/v1/managers/{id}/balances", m.balancesHandler)
	r.With(auth.Require(auth.PermTopup)).Post("/api/v1/managers/{id}/topup", m.topupHandler)

	// v2 phase 4 (FR-68/69): currency catalog + admin rate table + the
	// exchange endpoint, the only path that ever moves value between a
	// manager's currencies.
	r.With(auth.Require("")).Get("/api/v1/currencies", m.currenciesHandler)
	r.With(auth.Require("")).Get("/api/v1/currency-rates", m.listCurrencyRatesHandler)
	r.With(auth.Require("currency_rates.manage")).Post("/api/v1/currency-rates", m.createCurrencyRateHandler)
	r.With(auth.Require("")).Post("/api/v1/managers/{id}/exchange", m.exchangeHandler)

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

	// Manual payment providers (v2-2, FR-77-80, contracts C1-C10). Replaces
	// the Phase-4 gateway surface entirely (C12) — no callback webhook, no
	// gateway config, no mock simulator; a human verifies every ticket.
	r.With(auth.Require("")).Get("/api/v1/payment-providers", m.listProvidersHandler)
	r.With(auth.Require(permProvidersManage)).Post("/api/v1/payment-providers", m.createProviderHandler)
	r.With(auth.Require(permProvidersManage)).Put("/api/v1/payment-providers/{id}", m.updateProviderHandler)

	r.With(auth.Require("")).Get("/api/v1/managers/{id}/provider-accounts", m.listProviderAccountsHandler)
	r.With(auth.Require("")).Put("/api/v1/managers/{id}/provider-accounts/{providerId}", m.putProviderAccountHandler)
	r.With(auth.Require("")).Get("/api/v1/managers/{id}/method-settings", m.listMethodSettingsHandler)
	r.With(auth.Require("")).Put("/api/v1/managers/{id}/method-settings", m.putMethodSettingHandler)

	r.With(auth.Require(permPaymentTicketsVerify)).Get("/api/v1/payment-tickets", m.listTicketsHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Get("/api/v1/payment-tickets/{id}", m.ticketDetailHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Post("/api/v1/payment-tickets/{id}/approve", m.approveTicketHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Post("/api/v1/payment-tickets/{id}/reject", m.rejectTicketHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Get("/api/v1/payment-tickets/{id}/attachments/{attachmentId}", m.getAttachmentHandler)

	// v2 phase 9 (FR-71/73/74, contract C7): plan cost, overheads, reseller
	// wholesale pricing. All three are admin-only writes; reads are admin-only
	// too (business-cost/reseller data, never exposed to a reseller — C8).
	r.With(auth.Require("profiles.edit")).Post("/api/v1/profiles/{id}/cost", m.createProfileCostHandler)
	r.With(auth.Require("profiles.edit")).Get("/api/v1/profiles/{id}/cost-history", m.listProfileCostHistoryHandler)
	r.With(auth.Require("overheads.manage")).Get("/api/v1/overheads", m.listOverheadsHandler)
	r.With(auth.Require("overheads.manage")).Post("/api/v1/overheads", m.createOverheadHandler)
	r.With(auth.Require("reseller_prices.manage")).Get("/api/v1/reseller-prices", m.listResellerPricesHandler)
	r.With(auth.Require("reseller_prices.manage")).Post("/api/v1/reseller-prices", m.createResellerPriceHandler)

	wireOnce.Do(func() {
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
