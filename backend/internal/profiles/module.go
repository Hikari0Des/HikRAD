// Package profiles is the service-profile domain module (Phase 2, Agent 4).
// It owns profile CRUD (FR-8), the expiry/quota behaviors [02] enforces at auth
// (FR-9/FR-10), the Hotspot rate fields (FR-58.1), and archive-not-delete. Every
// mutation writes the audit log (C2) and, when applied immediately, invalidates
// B's policy cache for the affected subscribers and returns the online ones so
// the panel can offer a CoA rate refresh.
package profiles

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	db  *pgxpool.Pool
	log *slog.Logger
}

func (Module) Name() string { return "profiles" }

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.log = d.Log

	r.With(auth.Require("profiles.view")).Get("/api/v1/profiles", m.listHandler)
	r.With(auth.Require("profiles.view")).Get("/api/v1/profiles/{id}", m.getHandler)
	r.With(auth.Require("profiles.create")).Post("/api/v1/profiles", m.createHandler)
	r.With(auth.Require("profiles.edit")).Put("/api/v1/profiles/{id}", m.updateHandler)
	r.With(auth.Require("profiles.edit")).Post("/api/v1/profiles/{id}/archive", m.archiveHandler)
	// Delete is only for a plan nothing has ever used (FR-7.4); anything in use
	// is archived instead, which is what the archive route above is for.
	r.With(auth.Require("profiles.delete")).Delete("/api/v1/profiles/{id}", m.deleteHandler)

	// Time-of-day windows (FR-11). B reads them via the provider seam below; the
	// panel manages them per profile.
	r.With(auth.Require("profiles.view")).Get("/api/v1/profiles/{id}/tod-windows", m.listTODHandler)
	r.With(auth.Require("profiles.edit")).Post("/api/v1/profiles/{id}/tod-windows", m.createTODHandler)
	r.With(auth.Require("profiles.edit")).Delete("/api/v1/profiles/{id}/tod-windows/{wid}", m.deleteTODHandler)

	// Wire D's TOD read-model into B's boundary-sweep engine (once, at boot).
	radius.SetTODProvider(&todProvider{db: d.DB})
}

func init() { httpapi.Add(&Module{}) }

// internalError logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("profiles: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
