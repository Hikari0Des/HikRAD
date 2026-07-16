package radius

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// module holds the wired dependencies shared by the authorize engine, NAS/pool
// CRUD, CoA service and discovery.
type module struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	log      *slog.Logger
	settings platform.Settings
	eng      *engine
	nas      *nasRegistry
	coa      *coaService
	// dialROS is the RouterOS API connect seam for auto-setup (FR-56.2);
	// production wires vendor.DialROS, tests fake a router with no network.
	dialROS func(ctx context.Context, host string, port int, user, password string) (vendor.ROSConn, error)
}

// Module is the httpapi registration hook (contract C3).
type Module struct{}

func (Module) Name() string { return "radius" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	m := &module{db: d.DB, rdb: d.Redis, log: d.Log, settings: d.Settings, dialROS: vendor.DialROS}
	m.nas = newNASRegistry(d.DB, d.Log)
	m.eng = newEngine(d.Redis, d.Log, m.nas, d.DB)
	m.coa = newCoAService(d.DB, d.Redis, d.Log)
	setDefaultEngine(m.eng)
	setDefaultCoA(m.coa)
	pkgDB = d.DB

	// Refresh the FreeRADIUS clients file from the DB at boot (best-effort).
	go m.regenerateClients(context.Background())

	// Runtime enforcement worker (C4/FR-9/FR-10): consumes enforce.* pub/sub and
	// applies quota/expiry CoA to online sessions. Runs for the process lifetime.
	go m.startEnforcementWorker(context.Background())

	// Time-of-day sweep engine (C4/FR-11): boundary CoA + tod.window publish.
	go m.startTODSweeps(context.Background())

	// Internal-only authorize route (C4): served on :8080, never proxied by
	// Caddy.
	r.Post("/internal/radius/authorize", m.authorizeHandler())

	// NAS registry (C7-B). Mutations require the elevated permissions only
	// admins hold this phase; every mutation is audited (C2).
	r.With(auth.Require("nas.view")).Get("/api/v1/nas", m.listNASHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}", m.getNASHandler)
	r.With(auth.Require("nas.create")).Post("/api/v1/nas", m.createNASHandler)
	r.With(auth.Require("nas.edit")).Put("/api/v1/nas/{id}", m.updateNASHandler)
	r.With(auth.Require("nas.delete")).Delete("/api/v1/nas/{id}", m.deleteNASHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/config-snippet", m.configSnippetHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/hotspot-package", m.hotspotPackageHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/status", m.nasStatusHandler)
	r.With(auth.Require("nas.create")).Post("/api/v1/nas/discover", m.discoverHandler)
	// Version probe (item 8): reads version/board/identity over the RouterOS
	// API (read-only) and refreshes nas.ros_version. nas.edit because it
	// mutates the stored version.
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/probe", m.probeNASHandler)

	// RouterOS API auto-setup (FR-56.2-56.4 / contract C6). Preview connects
	// read-only; apply is the scariest write path in the product (additive-
	// only, whole-apply-abort-on-conflict, hash-gated against the router
	// state changing between the two calls) and is nas.edit-gated same as any
	// other NAS mutation.
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/auto-setup/preview", m.autoSetupPreviewHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/auto-setup/apply", m.autoSetupApplyHandler)

	// RADIUS debug tail (FR-39 / C6): SSE over radius:decisions, nas.view-gated.
	// Lives under /api/v1/live/ (E's debug view) but is B's backend per the
	// cross-assignment — a distinct path from live's own /live/sessions route.
	r.With(auth.Require("nas.view")).Get("/api/v1/live/debug", m.liveDebugSSE)

	// IP pools (C7-B).
	r.With(auth.Require("pools.view")).Get("/api/v1/pools", m.listPoolsHandler)
	r.With(auth.Require("pools.view")).Get("/api/v1/pools/{id}", m.getPoolHandler)
	r.With(auth.Require("pools.create")).Post("/api/v1/pools", m.createPoolHandler)
	r.With(auth.Require("pools.edit")).Put("/api/v1/pools/{id}", m.updatePoolHandler)
	r.With(auth.Require("pools.delete")).Delete("/api/v1/pools/{id}", m.deletePoolHandler)
}

func init() { httpapi.Add(Module{}) }
