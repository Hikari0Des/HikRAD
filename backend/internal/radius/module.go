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

	// Limits sweep engine (v4-4/FR-128/FR-129/FR-131): continuous uptime/
	// daily enforcement, speed-window live sync, and the daily reset. A
	// separate ticker from the two above — see limitsworker.go's package doc
	// for why it doesn't merge with either.
	go m.startLimitsSweep(context.Background())

	// Device worker (v4-6/FR-139-143): samples every NAS with saved API
	// credentials on its own ticker and is the only thing that ever dials a
	// router for the read-only device tabs — see device_worker.go.
	go m.startDeviceWorker(context.Background())

	// Firewall auto-revert worker (v4-7/FR-146): the safety net for the one
	// change class that can lock an operator out of their own router. Polls
	// for unconfirmed firewall changes past their revert deadline and
	// restores the pre-change values — see firewall_api.go.
	go m.startFirewallRevertWorker(context.Background())

	// Internal-only authorize route (C4): served on :8080, never proxied by
	// nginx.
	r.Post("/internal/radius/authorize", m.authorizeHandler())

	// Internal-only dynamic-client lookup (FR-118, v4-2, gate item 9): called
	// by deploy/freeradius/scripts/dynamic-client.pl from the dynamic-clients
	// virtual server so a NAS added through the panel authenticates with zero
	// FreeRADIUS restart. Same unproxied trust boundary as authorize above.
	r.Post("/internal/radius/dynamic-client", m.dynamicClientHandler())

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

	// Service discovery (FR-62.6): reads the router's real PPPoE/Hotspot server
	// instances (read-only) so the operator imports their exact names rather
	// than retyping them. Writes nothing — it proposes rows the operator saves
	// through the normal services[] contract — but it is nas.edit-gated because
	// it is only ever used while editing a NAS, and it dials the router with
	// stored credentials.
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/discover-services", m.discoverServicesHandler)

	// FR-62.7 health fix: applies HikRAD's known fix for ONE detected finding,
	// re-verified against the router first. nas.edit-gated and audited like any
	// other router write.
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/health/fix", m.healthFixHandler)

	// RouterOS API auto-setup (FR-56.2-56.4 / contract C6). Preview connects
	// read-only; apply is the scariest write path in the product (additive-
	// only, whole-apply-abort-on-conflict, hash-gated against the router
	// state changing between the two calls) and is nas.edit-gated same as any
	// other NAS mutation.
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/auto-setup/preview", m.autoSetupPreviewHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/auto-setup/apply", m.autoSetupApplyHandler)

	// v2 phase 3 / FR-65: read-only RADIUS-relevant router config, same
	// nas.view gate as every other read-only router inspection here.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/config", m.nasConfigHandler)

	// v4-6 (Router manager I, FR-139-143): monitor grid + per-tab device
	// reads, all served from the device worker's cache (never a synchronous
	// router call — module doc / device_api.go). Refresh kicks an off-cycle
	// worker tick in the background. Reboot/ping are the phase's one
	// exception: real, synchronous router actions, gated accordingly —
	// reboot needs its own destructive-action permission, separate from
	// nas.edit (a reboot is not a config edit).
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/monitor", m.listDeviceMonitorHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/device/{tab}", m.deviceTabHandler)
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/device/refresh", m.deviceRefreshHandler)
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/device/ping", m.devicePingHandler)
	r.With(auth.Require("nas.reboot")).Post("/api/v1/nas/{id}/device/reboot", m.deviceRebootHandler)

	// v2 phase 3 / FR-67: Hotspot/PPPoE server management. router-config is a
	// read (nas.view); plan/apply/adopt all write (nas.edit) — plan only to
	// the router via preview, apply/adopt persist to HikRAD too.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/services/{serviceId}/router-config", m.serviceRouterConfigHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/services/plan", m.servicePlanHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/services/apply", m.serviceApplyHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/services/{serviceId}/adopt", m.adoptServiceHandler)

	// v4-7 (Router manager II, FR-145): extended per-service parameters —
	// HikRAD-side metadata, no router write (see nas_services_store.go).
	r.With(auth.Require("nas.edit")).Put("/api/v1/nas/{id}/services/{serviceId}/params", m.serviceParamsHandler)

	// v4-7 / FR-144: the 10-step setup wizard. State read/write is nas.edit
	// (the panel saves after every step transition); detect/plan are
	// nas.view (read-only against the router); apply is nas.edit and the
	// phase's central write path.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/wizard/state", m.wizardStateGetHandler)
	r.With(auth.Require("nas.edit")).Put("/api/v1/nas/{id}/wizard/state", m.wizardStatePutHandler)
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/wizard/detect", m.wizardDetectHandler)
	r.With(auth.Require("nas.view")).Post("/api/v1/nas/{id}/wizard/plan", m.wizardPlanHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/wizard/apply", m.wizardApplyHandler)

	// v4-7 / FR-145: the Internet tab. Same read/plan/apply/adopt shape as
	// FR-108's pool push (pools_provision_api.go) applied to WAN uplinks.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/wan", m.wanListHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/wan/plan", m.wanPlanHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/wan/apply", m.wanApplyHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/wan/adopt", m.wanAdoptHandler)

	// v4-7 / FR-146: the Firewall tab. apply always snapshots the touched
	// rules BEFORE writing and schedules an auto-revert deadline
	// (firewall_api.go); confirm cancels it.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/firewall/rules", m.firewallRulesHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/firewall/plan", m.firewallPlanHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/firewall/apply", m.firewallApplyHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/firewall/{snapshotId}/confirm", m.firewallConfirmHandler)
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/firewall/pending", m.firewallPendingHandler)

	// v4-7 / FR-147: the Update tab. Read/check are nas.view; apply is its
	// own permission (nas.update) — separate from nas.edit and nas.reboot,
	// same posture as reboot's own dedicated permission (a firmware install
	// is neither a config edit nor the reboot action itself).
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/update", m.updateInfoHandler)
	r.With(auth.Require("nas.update")).Post("/api/v1/nas/{id}/update/check", m.updateCheckHandler)
	r.With(auth.Require("nas.update")).Post("/api/v1/nas/{id}/update/apply", m.updateApplyHandler)

	// v4-7 / FR-148: hotspot login page management, scoped to one service.
	// Save is a HikRAD-side design edit (no router write); push is the one
	// write, and the only place PushHotspotPage is ever called from.
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/services/{serviceId}/hotspot-page", m.hotspotPageGetHandler)
	r.With(auth.Require("nas.edit")).Put("/api/v1/nas/{id}/services/{serviceId}/hotspot-page", m.hotspotPageSaveHandler)
	r.With(auth.Require("nas.edit")).Post("/api/v1/nas/{id}/services/{serviceId}/hotspot-page/push", m.hotspotPagePushHandler)

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

	// v3-5 / FR-108: pool ownership sync (Decision 48). Pushing a HikRAD-OWNED
	// pool onto a router is a router write, hash-gated exactly like FR-66/67's
	// plan/apply; adopting imports the router's pools as read-only references
	// and writes nothing to the router. Both are pool edits (pools.edit) that
	// happen in the context of one NAS.
	r.With(auth.Require("pools.edit")).Post("/api/v1/nas/{id}/pools/{poolId}/push/plan", m.poolPushPlanHandler)
	r.With(auth.Require("pools.edit")).Post("/api/v1/nas/{id}/pools/{poolId}/push/apply", m.poolPushApplyHandler)
	r.With(auth.Require("pools.edit")).Post("/api/v1/nas/{id}/pools/adopt", m.poolAdoptHandler)
}

func init() { httpapi.Add(Module{}) }
