package monitorsvc

// httpapi registration (contract C3/C5/C7). The monitoring READ + CRUD surface
// is served by hikrad-api via this module; the probe/alert background loops run
// in the separate hikrad-monitor process (runner.go). Both share the same DB and
// Redis. Mounting is one blank import in cmd/hikrad-api/modules.go.

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Permission strings (contract C5: monitoring.*). Checked by string like every
// other module; admin's wildcard covers them, operators are granted them via A's
// editable role matrix.
const (
	PermView = "monitoring.view"
	PermEdit = "monitoring.edit"
)

// Package-level handles shared by the HTTP handlers (mirrors internal/live).
var (
	pkgDB       *pgxpool.Pool
	pkgRDB      *redis.Client
	pkgSettings platform.Settings
	pkgLog      *slog.Logger
)

// Module is the httpapi registration hook.
type Module struct{}

func (Module) Name() string { return "monitorsvc" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	pkgDB = d.DB
	pkgRDB = d.Redis
	pkgSettings = d.Settings
	pkgLog = d.Log

	// FR-32 dashboard (v2-10: per-widget access via dashboardAccess, C3) + FR-35/40 health.
	r.With(dashboardAccess).Get("/api/v1/dashboard", handleDashboard)
	r.With(auth.Require(PermView)).Get("/api/v1/health", handleHealth)

	// FR-60 monitored-device CRUD.
	r.With(auth.Require(PermView)).Get("/api/v1/devices", listDevices)
	r.With(auth.Require(PermView)).Get("/api/v1/devices/{id}", getDevice)
	r.With(auth.Require(PermEdit)).Post("/api/v1/devices", createDevice)
	r.With(auth.Require(PermEdit)).Put("/api/v1/devices/{id}", updateDevice)
	r.With(auth.Require(PermEdit)).Delete("/api/v1/devices/{id}", deleteDevice)

	// Probe history — same shape for both target kinds (contract C5 / FR-60).
	r.With(auth.Require("nas.view")).Get("/api/v1/nas/{id}/probes", nasProbeHistory)
	r.With(auth.Require(PermView)).Get("/api/v1/devices/{id}/probes", deviceProbeHistory)

	// FR-36 alert rules + events + in-app notifications feed.
	r.With(auth.Require(PermView)).Get("/api/v1/alert-rules", listAlertRules)
	r.With(auth.Require(PermEdit)).Post("/api/v1/alert-rules", createAlertRule)
	r.With(auth.Require(PermEdit)).Put("/api/v1/alert-rules/{id}", updateAlertRule)
	r.With(auth.Require(PermView)).Get("/api/v1/alert-events", listAlertEvents)
	r.With(auth.Require(PermView)).Get("/api/v1/live/notifications", notificationsSSE)
}

func init() { httpapi.Add(Module{}) }

// internalErr logs and writes the C2 500 envelope.
func internalErr(w http.ResponseWriter, what string, err error) {
	if pkgLog != nil {
		pkgLog.Error("monitorsvc: "+what+" failed", "error", err)
	}
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
