// Package importer is the SAS4/other-system CSV migration wizard (Phase 5,
// Agent 3, contract C3, FR-6). It never writes to the subscribers table
// directly — every created subscriber goes through the real
// POST /api/v1/subscribers handler (self-dispatched in-process onto the same
// chi router every other module mounts on), so audit logging and B's policy
// cache invalidation come for free and this package never re-implements
// subscriber validation. Upload/dry-run/state live in import_batches +
// import_rows (migrations 0400-0401); execute runs as an async job (the
// Phase-2 bulk-job pattern subscribers/bulk.go established) so a 10k-row
// batch never blocks the request.
package importer

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
	// router is the SAME chi.Router every module registers routes on
	// (httpapi.NewRouter passes its own root router into every Register
	// call). Captured so execute() can self-dispatch each row through the
	// real, fully-authenticated, fully-audited subscribers API instead of
	// writing subscribers rows with SQL (forbidden by this package's file
	// ownership: subscribers is read-only to us).
	router http.Handler
	jobs   *jobRegistry
}

func (Module) Name() string { return "importer" }

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.log = d.Log
	m.router = r
	m.jobs = newJobRegistry()

	// Upload/map/dry-run are read-only over subscriber data (no writes
	// happen until execute); gated the same as browsing subscribers.
	r.With(auth.Require("subscribers.view")).Post("/api/v1/import/subscribers", m.uploadHandler)
	r.With(auth.Require("subscribers.view")).Get("/api/v1/import/{batch}", m.batchStatusHandler)
	r.With(auth.Require("subscribers.view")).Post("/api/v1/import/{batch}/map", m.mapHandler)
	r.With(auth.Require("subscribers.view")).Post("/api/v1/import/{batch}/dry-run", m.dryRunHandler)
	// execute performs real creates: same permission the underlying
	// POST /api/v1/subscribers itself requires (defense in depth — even a
	// caller who reached this route without it would be 403'd on every
	// self-dispatched create).
	r.With(auth.Require("subscribers.create")).Post("/api/v1/import/{batch}/execute", m.executeHandler)
}

func init() { httpapi.Add(&Module{}) }

func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("importer: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
