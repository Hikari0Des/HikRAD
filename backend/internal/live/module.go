// Package live is the panel-facing side of the monitoring pipeline (Agent 3):
// the SSE Live Sessions feed, the live.Count/List interface B's session-limit
// check consumes, the session history + usage-graph REST, and the Disconnect
// action. It reads the live-session state hikrad-acct's consumer maintains in
// Redis (internal/live/livestate) and wires the radius authorize seams at boot.
// It never ingests packets — that is hikrad-acct (internal/accounting).
package live

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	log *slog.Logger
}

func (Module) Name() string { return "live" }

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.log = d.Log
	setHandles(d.Redis, d.DB)

	// Wire the radius authorize seams (contract C4/C6): B's session-limit check
	// (FR-58.2), NAS-delete confirmation, and pool utilisation all read C's live
	// state through these. Pool utilisation is deferred (0% shown) — the live
	// state does not carry pool membership; documented in the package README.
	radius.SetLiveCounter(Count)
	radius.SetNASLiveCounter(NASCount)
	radius.SetPoolUsageCounter(func(string) int { return 0 })

	// Live feed (SSE) + Disconnect (CoA via B).
	r.With(auth.Require("live.view")).Get("/api/v1/live/sessions", m.liveSessionsSSE)
	r.With(auth.Require(auth.PermDisconnect)).Post("/api/v1/live/disconnect", m.disconnect)

	// History + usage graphs (C7-C).
	r.With(auth.Require("sessions.view")).Get("/api/v1/sessions", m.listSessions)
	r.With(auth.Require("sessions.view")).Get("/api/v1/usage/subscriber/{id}", m.usageBySubscriber)
}

func init() { httpapi.Add(&Module{}) }

// internal logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internal(w http.ResponseWriter, what string, err error) {
	if m.log != nil {
		m.log.Error("live: "+what+" failed", "error", err)
	}
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
