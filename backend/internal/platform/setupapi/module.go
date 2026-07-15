// Package setupapi is the HTTP surface for Phase 5's platform work (Agent 1;
// contracts C4/C5/C7 — FR-50 license, FR-49.3 first-run wizard, FR-53
// settings, FR-51 backup status, FR-52.4 version). It is a sibling of
// internal/platform rather than living inside that package because these
// handlers need internal/auth (Require/Audit/CreateFirstAdmin) and platform
// already imports... nothing that would cycle, but auth imports platform, so
// platform itself must stay auth-free; a separate package that imports both
// is the same shape every other domain module (billing, radius, subscribers)
// already uses.
package setupapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

type service struct {
	db       *pgxpool.Pool
	settings platform.Settings
	log      *slog.Logger
}

var svc *service

type Module struct{}

func (Module) Name() string { return "setupapi" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	svc = &service{db: d.DB, settings: d.Settings, log: d.Log}

	// Prime the license cache at boot and keep it fresh on a ticker: the
	// grace clock must advance even on an idle server with no /api/v1/license
	// traffic, and expired_grace must take effect within one tick of the
	// 14-day boundary, not only when someone happens to poll the banner.
	go func() {
		ctx := context.Background()
		platform.RefreshLicenseCache(ctx, svc.db, svc.log)
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			platform.RefreshLicenseCache(ctx, svc.db, svc.log)
		}
	}()

	// License (C4). GET is any authenticated manager (the banner is shown to
	// everyone); mutating endpoints need license.manage (admin-only by
	// default — see auth/roles.go's "system" catalog group).
	r.With(auth.Require("")).Get("/api/v1/license", getLicenseHandler)
	r.With(auth.Require("license.manage")).Post("/api/v1/license", uploadLicenseHandler)
	r.With(auth.Require("license.manage")).Post("/api/v1/license/request-blob", requestBlobHandler)

	// First-run wizard (FR-49.3): unauthenticated by design (no admin exists
	// yet to hold a token), gated per-handler on "no manager exists yet".
	r.Get("/api/v1/setup/status", setupStatusHandler)
	r.Get("/api/v1/setup/license", setupGetLicenseHandler)
	r.Post("/api/v1/setup/license", setupUploadLicenseHandler)
	r.Post("/api/v1/setup/admin", setupCreateAdminHandler)
	r.Get("/api/v1/setup/branding", setupGetBrandingHandler)
	r.Post("/api/v1/setup/branding", setupPutBrandingHandler)

	// Settings (FR-53).
	r.With(auth.Require("settings.view")).Get("/api/v1/settings/{group}", getSettingsGroupHandler)
	r.With(auth.Require("settings.edit")).Put("/api/v1/settings/{group}", putSettingsGroupHandler)
	r.With(auth.Require("settings.edit")).Post("/api/v1/settings/notifications/test", testNotificationHandler)

	// Backups (FR-51): read-only status the panel's Settings > System screen
	// polls. `hikrad backup`/`hikrad restore` (scripts/hikrad) do the actual
	// work outside the API process (they need host filesystem access for
	// .env/Caddy config/branding assets) and write their result here via
	// `hikrad-api record-backup` so the panel has something to show.
	r.With(auth.Require("backups.view")).Get("/api/v1/backups", listBackupsHandler)

	// Version/system info (FR-52.4).
	r.Get("/api/v1/system/version", systemVersionHandler)
}

func init() { httpapi.Add(Module{}) }
