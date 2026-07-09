// Package httpapi is the HTTP framework every HikRAD domain package plugs
// into (FR-52). It owns the module registry (Phase-1 contract C3), the
// router and middleware chain, the C2 error envelope, cursor pagination and
// validation helpers, and the injectable authentication seam.
package httpapi

import (
	"log/slog"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Deps carries the shared dependencies handed to every module.
// Shape frozen by Phase-1 contract C3.
type Deps struct {
	DB       *pgxpool.Pool
	Redis    *redis.Client
	Settings platform.Settings
	Log      *slog.Logger
}

// Module is implemented by each domain package. Register receives the root
// router: modules register absolute paths (`/api/v1/...` for public routes
// proxied by Caddy, `/internal/...` for unproxied service-to-service routes).
// Shape frozen by Phase-1 contract C3.
type Module interface {
	Name() string
	Register(r chi.Router, d Deps)
}

var registry []Module

// Add registers a module. Domain packages call it from their init(), so a
// blank import in cmd/hikrad-api/modules.go is all that is needed to mount a
// package — no shared route file is ever edited.
func Add(m Module) { registry = append(registry, m) }

// Modules returns the registered modules in registration order.
func Modules() []Module { return slices.Clone(registry) }
