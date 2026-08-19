package support

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	pkgDB  *pgxpool.Pool
	pkgLog *slog.Logger
)

type Module struct{}

func (Module) Name() string { return "support" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	pkgDB = d.DB
	pkgLog = d.Log

	r.With(auth.Require(PermView)).Get("/api/v1/support-tickets", listTicketsHandler)
	r.With(auth.Require(PermView)).Get("/api/v1/support-tickets/counts", countsHandler)
	r.With(auth.Require(PermView)).Get("/api/v1/support-tickets/{id}", getTicketHandler)
	r.With(auth.Require(PermManage)).Post("/api/v1/support-tickets", createTicketHandler)
	r.With(auth.Require(PermManage)).Post("/api/v1/support-tickets/{id}/messages", addMessageHandler)
	r.With(auth.Require(PermManage)).Post("/api/v1/support-tickets/{id}/status", setStatusHandler)
	r.With(auth.Require(PermManage)).Post("/api/v1/support-tickets/{id}/withdraw", withdrawHandler)
}

func init() { httpapi.Add(Module{}) }
