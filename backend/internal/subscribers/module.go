// Package subscribers is the subscriber domain module (Phase 2, Agent 4). It owns
// full subscriber CRUD (FR-1), global search (FR-2), the user-detail composition
// (FR-3), bulk actions (FR-4), MAC-lock/session-limit rules and per-user
// overrides (FR-5/FR-7), the FR-58/FR-55 toggles, and the C4 AuthView read-model
// B's authorize engine consumes on every Access-Request. Every mutation writes
// the audit log (C2) and invalidates B's policy cache (C4).
package subscribers

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	db   *pgxpool.Pool
	rdb  *redis.Client
	log  *slog.Logger
	jobs *jobRegistry
}

func (Module) Name() string { return "subscribers" }

// sweepOnce guards the single background expiry-sweep goroutine so repeated
// Register calls (each httptest server in the suite mounts the router afresh)
// don't spawn duplicate sweepers.
var sweepOnce sync.Once

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.rdb = d.Redis
	m.log = d.Log
	m.jobs = newJobRegistry()

	// Wire the C4 read-model into B's authorize path (once, at boot).
	radius.SetPolicyProvider(&policyProvider{db: d.DB, rdb: d.Redis})

	// Subscriber CRUD (C7-D). Deletes are admin-only; everything else follows the
	// hardcoded permission sets (operators create/edit, agents view their own).
	r.With(auth.Require("subscribers.view")).Get("/api/v1/subscribers", m.listHandler)
	r.With(auth.Require("subscribers.view")).Get("/api/v1/subscribers/{id}", m.detailHandler)
	r.With(auth.Require("subscribers.create")).Post("/api/v1/subscribers", m.createHandler)
	r.With(auth.Require("subscribers.edit")).Put("/api/v1/subscribers/{id}", m.updateHandler)
	r.With(auth.Require("subscribers.delete")).Delete("/api/v1/subscribers/{id}", m.deleteHandler)
	r.With(auth.Require("subscribers.edit")).Post("/api/v1/subscribers/{id}/reset-mac", m.resetMacHandler)

	// Bulk: the route is view-gated; per-action permission (edit / export) is
	// enforced inside so a single endpoint serves both mutating jobs and export.
	r.With(auth.Require("subscribers.view")).Post("/api/v1/subscribers/bulk", m.bulkHandler)
	r.With(auth.Require("subscribers.view")).Get("/api/v1/subscribers/bulk/{id}", m.bulkStatusHandler)

	// Global search (C7-D).
	r.With(auth.Require("subscribers.view")).Get("/api/v1/search", m.searchHandler)

	// Expiry sweep (FR-1.2): aligns the status column with expires_at so lists
	// agree with the auth-time authority. Started once; auth-time remains the
	// source of truth between sweeps.
	sweepOnce.Do(func() {
		go m.runSweep(context.Background())
	})
}

func init() { httpapi.Add(&Module{}) }

// internalError logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("subscribers: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}

// sweepInterval is how often the expiry sweep runs (≤ 5 min per sub-PRD 04 §7).
const sweepInterval = 5 * time.Minute
