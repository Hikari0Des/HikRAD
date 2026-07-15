// Package reports is the read-only analytical layer (Phase 5, Agent 3,
// contract C2, FR-45/46/47/48). It owns no primary tables — every figure is
// an aggregation over data other modules own (ledger/payments from billing,
// subscribers/profiles, usage_daily from accounting), so a report can never
// disagree with the balances/lists it summarizes. Every endpoint applies
// auth.ScopeFilter; CSV export additionally requires auth.PermExport.
package reports

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	db  *pgxpool.Pool
	log *slog.Logger
}

func (Module) Name() string { return "reports" }

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.log = d.Log

	r.With(auth.Require("reports.view")).Get("/api/v1/reports/revenue", m.revenueHandler)
	r.With(auth.Require("reports.view")).Get("/api/v1/reports/settlement", m.settlementHandler)
	r.With(auth.Require("reports.view")).Get("/api/v1/reports/subscribers", m.subscribersReportHandler)
	r.With(auth.Require("reports.view")).Get("/api/v1/reports/usage", m.usageReportHandler)

	// Internal service surface (unproxied, like billing's /internal/stats):
	// the FR-48 daily-digest composition C's scheduler consumes.
	r.Get("/internal/reports/digest", m.digestHandler)
}

func init() { httpapi.Add(&Module{}) }

func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("reports: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}

// requireExport 403s unless the caller holds the export permission (checked
// inline, not at the route level, since format=csv is a query flag on the
// same route as the JSON view — mirrors subscribers/bulk.go's export gate).
func requireExport(w http.ResponseWriter, r *http.Request) bool {
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || !mgr.Can(auth.PermExport) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to export")
		return false
	}
	return true
}
