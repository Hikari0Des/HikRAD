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
// audit-lint: delegated — every created subscriber/profile goes through the real,
// audited create handler in-process (asserted by db_test.go's subscriber.create audit check).

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

	// Per-entity upload (FR-111.3): the entity is bound at registration and the
	// route guard is that entity's view permission. A subscribers upload still
	// requires subscribers.view exactly as before.
	r.With(auth.Require("subscribers.view")).Post("/api/v1/import/subscribers", m.uploadHandler("subscribers"))
	r.With(auth.Require("profiles.view")).Post("/api/v1/import/profiles", m.uploadHandler("profiles"))

	// The {batch} routes are SHARED across entities: they authenticate at the
	// route (Require("") = any valid token) and then enforce the batch's
	// entity-appropriate permission inside the handler, since a single route
	// guard cannot know the batch's entity before loading it. A subscribers
	// batch therefore still requires subscribers.view/create; a profiles batch
	// requires profiles.view/create — behavior for the pre-existing subscriber
	// import is unchanged.
	r.With(auth.Require("")).Get("/api/v1/import/{batch}", m.batchStatusHandler)
	r.With(auth.Require("")).Post("/api/v1/import/{batch}/map", m.mapHandler)
	r.With(auth.Require("")).Post("/api/v1/import/{batch}/dry-run", m.dryRunHandler)
	r.With(auth.Require("")).Post("/api/v1/import/{batch}/execute", m.executeHandler)
}

func init() { httpapi.Add(&Module{}) }

// entityViewPerm / entityCreatePerm map a batch's entity onto the permission its
// browse / create paths require — the SAME permissions the underlying
// GET/POST endpoints enforce, so the import wizard can never be a route around
// them (defense in depth: execute also re-checks per self-dispatched create).
func entityViewPerm(entity string) string {
	if entity == "profiles" {
		return "profiles.view"
	}
	return "subscribers.view"
}

func entityCreatePerm(entity string) string {
	if entity == "profiles" {
		return "profiles.create"
	}
	return "subscribers.create"
}

// authorize enforces a permission inside a handler (for the shared {batch}
// routes, whose guard only authenticates). Returns false and writes a 403 when
// the caller lacks it.
func (m *Module) authorize(w http.ResponseWriter, r *http.Request, perm string) bool {
	mgr, ok := auth.ManagerFrom(r.Context())
	if !ok || !mgr.Can(perm) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to perform this action")
		return false
	}
	return true
}

func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("importer: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
