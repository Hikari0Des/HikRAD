// Package cards is the card-groups domain module (v4-5, FR-136-137, Decision
// 65). It owns card_groups/cards CRUD, group actions (enable/disable-all,
// extend shelf date, add-more, duplicate, per-card reset), branded print
// batches, and the radius.CardAuthenticator seam a card logs in through —
// evaluated by the EXISTING v4-4 limit engine, never a second one.
package cards

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	log      *slog.Logger
	settings platform.Settings
}

func (Module) Name() string { return "cards" }

var wireOnce sync.Once

func (m *Module) Register(r chi.Router, d httpapi.Deps) {
	m.db = d.DB
	m.rdb = d.Redis
	m.log = d.Log
	m.settings = d.Settings

	r.With(auth.Require("cards.view")).Get("/api/v1/cards/groups", m.listGroupsHandler)
	r.With(auth.Require("cards.view")).Get("/api/v1/cards/groups/{id}", m.getGroupHandler)
	r.With(auth.Require("cards.create")).Post("/api/v1/cards/groups", m.createGroupHandler)
	r.With(auth.Require("cards.view")).Get("/api/v1/cards/groups/{id}/cards", m.listCardsHandler)

	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/groups/{id}/enable", m.enableGroupHandler)
	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/groups/{id}/disable", m.disableGroupHandler)
	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/groups/{id}/extend", m.extendGroupHandler)
	r.With(auth.Require("cards.create")).Post("/api/v1/cards/groups/{id}/add-more", m.addMoreHandler)
	r.With(auth.Require("cards.create")).Post("/api/v1/cards/groups/{id}/duplicate", m.duplicateGroupHandler)

	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/{id}/reset", m.resetCardHandler)
	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/{id}/disable", m.disableCardHandler)
	r.With(auth.Require("cards.edit")).Post("/api/v1/cards/{id}/enable", m.enableCardHandler)

	r.With(auth.Require("cards.print")).Post("/api/v1/cards/groups/{id}/print", m.printGroupHandler)
	r.With(auth.Require("cards.view")).Get("/api/v1/cards/groups/{id}/usage.csv", m.usageCSVHandler)

	wireOnce.Do(func() {
		// The RADIUS card-hotspot-login seam (FR-136) — radius never imports
		// cards, same discipline as billing's SetVoucherAuthenticator.
		radius.SetCardAuthenticator(&cardAuthenticator{db: d.DB})
	})
}

func init() { httpapi.Add(&Module{}) }

func (m *Module) internalError(w http.ResponseWriter, what string, err error) {
	m.log.Error("cards: "+what+" failed", "error", err)
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}
