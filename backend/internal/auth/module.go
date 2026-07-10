// Package auth is the platform security module (Phase 2, Agent 1). It owns
// real manager authentication (argon2id, JWT access + rotating refresh, panel
// session records), the permission/scoping middleware every API module adopts
// (auth.Require / auth.ScopeFilter), the append-only audit-log write API
// (auth.Audit), and installs the process-wide crypto default (platform/crypto)
// other modules use for secrets at rest. It replaces the Phase-1 dev auth stub
// while keeping the C7 login response shape.
//
// The 2FA, roles editor, IP allowlist and audit viewer land in Phase 3 on
// these same tables and middleware.
package auth

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// service holds the wired dependencies shared by every handler and by the
// package-level helpers (Require, ScopeFilter, Audit). It is a package global
// in the same style as httpapi's authenticator seam, so other modules call
// auth.Require/Audit with no wiring.
type service struct {
	db      *pgxpool.Pool
	rdb     *redis.Client
	log     *slog.Logger
	tokens  *tokenService
	limiter *loginLimiter
	crypto  *crypto.Service
}

var svc *service

// configure wires the package globals and installs the real authenticator into
// the httpapi seam (replacing the Phase-1 signature-only stub). Idempotent.
func configure(d httpapi.Deps, jwtSecret, encKey []byte) error {
	// Make platform/crypto's package-level Encrypt/Decrypt usable process-wide
	// (Agents B/D consume it for NAS secrets and subscriber passwords).
	if err := crypto.Configure(encKey); err != nil {
		return err
	}
	cs, err := crypto.New(encKey)
	if err != nil {
		return err
	}
	svc = &service{
		db:      d.DB,
		rdb:     d.Redis,
		log:     d.Log,
		tokens:  newTokenService(jwtSecret),
		limiter: newLoginLimiter(d.Redis),
		crypto:  cs,
	}
	httpapi.SetAuthenticator(httpAuthenticator{tokens: svc.tokens})
	return nil
}

type Module struct{}

func (Module) Name() string { return "auth" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	// Deps (C3) carries neither the JWT secret nor the encryption key, so —
	// exactly like internal/radius — reload the already-validated environment.
	// main() loaded it successfully before building the router; a failure here
	// means the environment changed under a running process and deserves a
	// loud crash, not a per-request 500.
	cfg, err := platform.LoadConfig()
	if err != nil {
		panic("auth: reload config: " + err.Error())
	}
	if err := configure(d, []byte(cfg.JWTSecret), cfg.EncryptionKey); err != nil {
		panic("auth: configure: " + err.Error())
	}

	// Public auth endpoints (C7 shapes preserved). login/refresh are unguarded
	// (they establish identity); logout requires a valid access token.
	r.Post("/api/v1/auth/login", loginHandler)
	r.Post("/api/v1/auth/refresh", refreshHandler)
	r.With(Require("")).Post("/api/v1/auth/logout", logoutHandler)

	// Manager CRUD (admins only, via managers.* permissions).
	r.With(Require("managers.view")).Get("/api/v1/managers", listManagersHandler)
	r.With(Require("managers.create")).Post("/api/v1/managers", createManagerHandler)
	r.With(Require("managers.edit")).Put("/api/v1/managers/{id}", updateManagerHandler)
	r.With(Require("managers.edit")).Post("/api/v1/managers/{id}/unlock", unlockManagerHandler)

	// Panel session management (own; admins any).
	r.With(Require("")).Get("/api/v1/panel-sessions", listPanelSessionsHandler)
	r.With(Require("")).Delete("/api/v1/panel-sessions/{id}", deletePanelSessionHandler)

	// Audit log reader (permission-gated).
	r.With(Require("audit.view")).Get("/api/v1/audit-log", listAuditLogHandler)
}

func init() { httpapi.Add(Module{}) }
