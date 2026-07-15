// Package push is the Web Push backend (contract C4, FR-54.4): VAPID identity
// bootstrap, the subscribe/unsubscribe REST surface, RFC 8291 payload
// encryption, and delivery with 410-Gone pruning. It is consumed two ways:
//
//   - monitorsvc wires it in as the alert engine's 4th channel (panel surface,
//     DeliverPanel) — see monitorsvc/push_channel.go. Critically, the alert
//     engine's background loops run inside the SEPARATE hikrad-monitor
//     process (cmd/hikrad-monitor/main.go), which never mounts httpapi
//     modules — so Module.Register (below) never runs there. hikrad-monitor's
//     main.go therefore calls Init directly at boot, exactly like it already
//     does for crypto.Configure. Forgetting this wiring is a nil-pkgDB panic
//     the first time an alert fires, since Register alone only covers the
//     hikrad-api process.
//   - D's portalapi calls Subscribe/Unsubscribe/DeliverToSubscriber directly
//     for the portal surface once its subscriber-token middleware lands
//     (this package has no subscriber auth of its own — the portal HTTP route
//     is D's to add, backed by these exported functions; documented in
//     status-agent-2.md as the concrete seam since D had not started this
//     package at the time this was written).
//
// The HTTP route mounted here (POST/DELETE /api/v1/push/subscribe) is the
// panel path: manager-token-authenticated, surface="panel" only.
package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	surfacePanel  = "panel"
	surfacePortal = "portal"
)

var (
	pkgDB       *pgxpool.Pool
	pkgRDB      *redis.Client
	pkgSettings platform.Settings
	pkgLog      *slog.Logger
)

// Init wires the package's DB/settings/log handles and bootstraps the VAPID
// identity (idempotent — see EnsureKeys). Every process that calls into this
// package (hikrad-api via Module.Register, and hikrad-monitor directly at
// boot — see the package doc comment) must call this once; Go package state
// does not cross process boundaries, so each binary needs its own call.
func Init(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) {
	pkgDB, pkgRDB, pkgSettings, pkgLog = db, rdb, settings, log
	if _, _, err := EnsureKeys(context.Background(), settings); err != nil && log != nil {
		log.Warn("push: VAPID bootstrap deferred (will retry on first use)", "error", err)
	}
}

// Module is the httpapi registration hook (contract C3).
type Module struct{}

func (Module) Name() string { return "push" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	Init(d.DB, d.Redis, d.Settings, d.Log)

	r.With(auth.Require("")).Post("/api/v1/push/subscribe", handleSubscribe)
	r.With(auth.Require("")).Delete("/api/v1/push/subscribe", handleUnsubscribe)
	r.Get("/api/v1/push/vapid-public-key", handleVapidPublicKey)
}

func init() { httpapi.Add(Module{}) }

type subscriptionBody struct {
	Endpoint string `json:"endpoint"`
	Keys     Keys   `json:"keys"`
}

type subscribeRequest struct {
	Surface      string           `json:"surface"`
	Subscription subscriptionBody `json:"subscription"`
}

func handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_body", "malformed JSON")
		return
	}
	if req.Surface != surfacePanel {
		httpapi.Error(w, http.StatusBadRequest, "invalid_surface", "this route accepts panel subscriptions only")
		return
	}
	m, ok := auth.ManagerFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := Subscribe(r.Context(), surfacePanel, m.ID, req.Subscription.Endpoint, req.Subscription.Keys); err != nil {
		internalErr(w, "subscribe", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

func handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_body", "malformed JSON")
		return
	}
	m, ok := auth.ManagerFrom(r.Context())
	if !ok {
		httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := Unsubscribe(r.Context(), surfacePanel, m.ID, req.Endpoint); err != nil {
		internalErr(w, "unsubscribe", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleVapidPublicKey serves the VAPID public key needed before a browser
// can call PushManager.subscribe. Unauthenticated by design: the key is
// public per RFC 8292 (it's the `k=` param sent to the push service on every
// send), and both the panel and portal surfaces need it before either has
// finished its own auth handshake.
func handleVapidPublicKey(w http.ResponseWriter, r *http.Request) {
	_, pubB64, err := EnsureKeys(r.Context(), pkgSettings)
	if err != nil {
		internalErr(w, "vapid public key", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]string{"key": pubB64})
}

func internalErr(w http.ResponseWriter, what string, err error) {
	if pkgLog != nil {
		pkgLog.Error("push: "+what+" failed", "error", err)
	}
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
