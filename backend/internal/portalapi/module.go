// Package portalapi is the subscriber-facing portal API (Phase 4, Agent 3;
// contracts C2/C3/C8). It owns subscriber login/self-service, usage/payment
// history passthrough, voucher redemption, the e-wallet payment
// create/poll routes, and scratch-card submission — all subscriber-scoped
// server-side, identity from the token only (IDOR rule, absolute). The heavy
// money/lifecycle logic (renewal, gateway adapters, intent state machine,
// card verification queue) lives in internal/billing; this package is a thin,
// auth-and-shape layer over billing's exported seam (portal_seam.go).
package portalapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Module is the httpapi registration hook (contract C3).
type Module struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	log       *slog.Logger
	settings  platform.Settings
	jwtSecret []byte
	limiter   *loginLimiter
}

func (Module) Name() string { return "portalapi" }

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.rdb = d.Redis
	m.log = d.Log
	m.settings = d.Settings
	m.limiter = newLoginLimiter(d.Redis)

	// Deps carries neither the JWT secret nor the encryption key — reload the
	// already-validated environment, same pattern as internal/auth and
	// internal/radius (main() validated it successfully before serving).
	cfg, err := platform.LoadConfig()
	if err != nil {
		panic("portalapi: reload config: " + err.Error())
	}
	m.jwtSecret = []byte(cfg.JWTSecret)

	// Auth (C2 FR-41.1). login/refresh are unguarded (they establish
	// identity); logout requires a valid portal access token.
	r.Post("/api/v1/portal/login", m.loginHandler)
	r.Post("/api/v1/portal/refresh", m.refreshHandler)
	r.With(m.requireSubscriber).Post("/api/v1/portal/logout", m.logoutHandler)

	// Self-care (C2 FR-41.2/41.3, FR-44).
	r.With(m.requireSubscriber).Get("/api/v1/portal/me", m.meHandler)
	r.With(m.requireSubscriber).Put("/api/v1/portal/me", m.updateMeHandler)
	r.With(m.requireSubscriber).Put("/api/v1/portal/language", m.languageHandler)
	r.With(m.requireSubscriber).Get("/api/v1/portal/usage", m.usageHandler)
	r.With(m.requireSubscriber).Get("/api/v1/portal/payments", m.paymentsHandler)

	// Renewal (FR-42): voucher + e-wallet gateway.
	r.With(m.requireSubscriber).Post("/api/v1/portal/vouchers/redeem", m.redeemVoucherHandler)
	r.With(m.requireSubscriber).Get("/api/v1/portal/payments/gateways", m.listGatewaysHandler)
	r.With(m.requireSubscriber).Post("/api/v1/portal/payments/{gateway}/create", m.createPaymentHandler)
	r.With(m.requireSubscriber).Get("/api/v1/portal/payments/intents/{id}", m.getPaymentIntentHandler)

	// Scratch-card payments (C8, FR-59).
	r.With(m.requireSubscriber).Get("/api/v1/portal/card-payments/types", m.cardTypesHandler)
	r.With(m.requireSubscriber).Get("/api/v1/portal/card-payments/mine", m.myCardPaymentHandler)
	r.With(m.requireSubscriber).Post("/api/v1/portal/card-payments", m.submitCardHandler)

	// Public branding read (C5: manifests, login page).
	r.Get("/api/v1/branding", m.brandingHandler)
}

func init() { httpapi.Add(&Module{}) }

// internalError logs a server-side failure and writes the C2 500 envelope.
func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("portalapi: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
