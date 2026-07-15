package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter builds the hikrad-api handler: the frozen middleware chain
// (request ID, structured logging, recovery, dev CORS, content-type
// enforcement) plus every registered module. Modules register absolute
// paths: `/api/v1/...` (public, proxied by Caddy) and `/internal/...`
// (unproxied, reachable only inside the Compose network — Agent 1's Caddy
// config forwards only /api*, which is what keeps /internal unreachable
// from outside; verified at the Phase-1 integration gate).
//
// dev comes from platform.Config.IsDev() (the C3 Deps.Settings is the typed
// settings service, not process config, so dev-mode toggles are passed
// explicitly).
func NewRouter(d Deps, dev bool) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(echoRequestID)
	r.Use(requestLogger(d.Log))
	r.Use(recoverer(d.Log))
	if dev {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			AllowCredentials: false,
			MaxAge:           300,
		}))
	}
	r.Use(enforceJSON)
	r.Use(licenseGate)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "resource not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	for _, m := range Modules() {
		m.Register(r, d)
	}
	return r
}

// echoRequestID copies the request ID into the response headers so clients
// can correlate errors with server logs.
func echoRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// recoverer converts panics into a C2-shaped 500 carrying the request ID,
// so no handler bug can ever leak a non-envelope error body.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					reqID := middleware.GetReqID(r.Context())
					log.Error("panic recovered",
						"panic", fmt.Sprint(rec),
						"request_id", reqID,
						"method", r.Method,
						"path", r.URL.Path,
					)
					Error(w, http.StatusInternalServerError, "internal",
						fmt.Sprintf("internal server error (request_id: %s)", reqID))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// enforceJSON rejects mutating requests whose body is not declared as JSON.
func enforceJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if r.ContentLength != 0 {
				ct := r.Header.Get("Content-Type")
				if !strings.HasPrefix(ct, "application/json") {
					Error(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
						"Content-Type must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
