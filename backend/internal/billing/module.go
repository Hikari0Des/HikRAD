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
	"github.com/hikrad/hikrad/internal/subscribers"
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

	// Currency catalog + admin rate table survive as entry/display only (FR-101.4).
	// v3-2 (FR-100.3): the exchange endpoint is RETIRED — with one IQD book there
	// is nothing to exchange between; foreign amounts convert to IQD at entry.
	r.With(auth.Require("")).Get("/api/v1/currencies", m.currenciesHandler)
	r.With(auth.Require("")).Get("/api/v1/currency-rates", m.listCurrencyRatesHandler)
	r.With(auth.Require("currency_rates.manage")).Post("/api/v1/currency-rates", m.createCurrencyRateHandler)

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

	// Instance-level defaults for subscribers with no owning manager (owner
	// decision 2026-07-19, migration 0592). Admin-only: the same permission
	// that governs the provider catalog itself.
	r.With(auth.Require(permProvidersManage)).Get("/api/v1/instance/method-settings", m.listInstanceMethodSettingsHandler)
	r.With(auth.Require(permProvidersManage)).Put("/api/v1/instance/method-settings", m.putInstanceMethodSettingHandler)
	r.With(auth.Require(permProvidersManage)).Get("/api/v1/instance/provider-accounts", m.listInstanceProviderAccountsHandler)
	r.With(auth.Require(permProvidersManage)).Put("/api/v1/instance/provider-accounts/{providerId}", m.putInstanceProviderAccountHandler)

	r.With(auth.Require(permPaymentTicketsVerify)).Get("/api/v1/payment-tickets", m.listTicketsHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Get("/api/v1/payment-tickets/{id}", m.ticketDetailHandler)
	r.With(auth.Require(permPaymentTicketsVerify)).Post("/api/v1/payment-tickets/{id}/reveal", m.revealTicketCardHandler)
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

	// v4-9 (FR-153): chart of accounts.
	r.With(auth.Require(permAccountingView)).Get("/api/v1/accounts", m.listAccountsHandler)
	r.With(auth.Require(permAccountingManage)).Post("/api/v1/accounts", m.createAccountHandler)

	// v4-9 (FR-156): statements. All read-only; reseller isolation is a
	// field-level omission inside profitAndLossHandler, not a separate
	// permission (a reseller sees their own statements, just without
	// cost/margin — gate item 12).
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/pl", m.profitAndLossHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/balance-sheet", m.balanceSheetHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/cash-flow", m.cashFlowHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/aging/receivables", m.agingReceivablesHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/aging/payables", m.agingPayablesHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/trial-balance", m.trialBalanceHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/general-ledger", m.generalLedgerHandler)
	r.With(auth.Require(permAccountingView)).Get("/api/v1/statements/journal", m.journalHandler)

	// v4-9 (FR-154): invoices.
	r.With(auth.Require(permInvoicesView)).Get("/api/v1/invoices", m.listInvoicesHandler)
	r.With(auth.Require(permInvoicesView)).Get("/api/v1/invoices/{id}", m.getInvoiceHandler)
	r.With(auth.Require(permInvoicesCreate)).Post("/api/v1/invoices", m.createInvoiceHandler)
	r.With(auth.Require(permInvoicesManage)).Post("/api/v1/invoices/{id}/void", m.voidInvoiceHandler)
	r.With(auth.Require(permInvoicesCreate)).Post("/api/v1/invoices/{id}/receipts", m.createARReceiptHandler)

	// v4-9 (FR-155): expenses.
	r.With(auth.Require(permExpensesView)).Get("/api/v1/expenses", m.listExpensesHandler)
	r.With(auth.Require(permExpensesCreate)).Post("/api/v1/expenses", m.createExpenseHandler)
	r.With(auth.Require(permExpensesManage)).Post("/api/v1/expenses/{id}/void", m.voidExpenseHandler)
	r.With(auth.Require(permExpensesCreate)).Post("/api/v1/expenses/{id}/payments", m.createAPPaymentHandler)

	// v4-9 (FR-157): fixed assets.
	r.With(auth.Require(permAccountingView)).Get("/api/v1/fixed-assets", m.listFixedAssetsHandler)
	r.With(auth.Require(permAccountingManage)).Post("/api/v1/fixed-assets", m.createFixedAssetHandler)
	r.With(auth.Require(permAccountingManage)).Post("/api/v1/fixed-assets/{id}/depreciate", m.runDepreciationHandler)

	// v4-9 (FR-158): undo. Authenticated only — undoHandler enforces
	// ownership itself (a scoped agent may only undo their own actions).
	r.With(auth.Require("")).Post("/api/v1/undo/{id}", m.undoHandler)
	r.With(auth.Require("")).Get("/api/v1/undo/mine", m.listMyUndoableHandler)

	// v4-9 (FR-159): the approval queue.
	r.With(auth.Require("")).Get("/api/v1/approval-requests", m.listApprovalRequestsHandler)
	r.With(auth.Require("")).Get("/api/v1/approval-requests/{id}", m.getApprovalRequestHandler)
	r.With(auth.Require(permApprovalsManage)).Post("/api/v1/approval-requests/{id}/approve", m.approveApprovalRequestHandler)
	r.With(auth.Require(permApprovalsManage)).Post("/api/v1/approval-requests/{id}/reject", m.rejectApprovalRequestHandler)
	r.With(auth.Require("")).Post("/api/v1/approval-requests/{id}/withdraw", m.withdrawApprovalRequestHandler)

	wireOnce.Do(func() {
		// B's hotspot-voucher login redeems through this seam (C3 internal redeem
		// API); B never imports billing.
		radius.SetVoucherAuthenticator(&voucherAuthenticator{m: m})
		// Constrain B's voucher-shape detection to the house prefix when set, so a
		// real username is never mistaken for a voucher (edge case).
		if p := m.getString(context.Background(), keyVoucherPrefix, ""); p != "" {
			radius.SetVoucherPrefix(p)
		}
		// v4-5 FR-133: the batch-creation endpoint's invoicing choice runs
		// through THIS package's renewal path — subscribers never imports
		// billing (same seam discipline as the voucher authenticator above).
		subscribers.SetInvoicer(&batchInvoicer{m: m})
	})
}

func init() { httpapi.Add(&Module{}) }

// internalError logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("billing: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
