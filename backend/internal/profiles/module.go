// Package profiles is the service-profile domain module. Phase 1 ships only
// a placeholder list endpoint that proves the httpapi framework end to end;
// full CRUD arrives in Phase 2 from this same agent role.
package profiles

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type Module struct{}

func (Module) Name() string { return "profiles" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	r.With(httpapi.RequireAuth).Get("/api/v1/profiles", listHandler(d))
}

func init() { httpapi.Add(Module{}) }

// Profile is the Phase-1 read shape (schema per contract C6).
type Profile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PriceIQD     int64     `json:"price_iqd"`
	DurationDays int       `json:"duration_days"`
	RateDownKbps int       `json:"rate_down_kbps"`
	RateUpKbps   int       `json:"rate_up_kbps"`
	CreatedAt    time.Time `json:"created_at"`
}

func listHandler(d httpapi.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpapi.ParsePage(r)
		if err != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
			return
		}
		var after *string
		if len(page.Cursor) == 1 {
			after = &page.Cursor[0]
		} else if page.Cursor != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		rows, err := d.DB.Query(r.Context(),
			`SELECT id::text, name, price_iqd, duration_days, rate_down_kbps, rate_up_kbps, created_at
			   FROM profiles
			  WHERE $1::uuid IS NULL OR id > $1::uuid
			  ORDER BY id
			  LIMIT $2`,
			after, page.Limit+1)
		if err != nil {
			d.Log.Error("profiles list query failed", "error", err)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		defer rows.Close()

		items := make([]Profile, 0, page.Limit)
		for rows.Next() {
			var p Profile
			if err := rows.Scan(&p.ID, &p.Name, &p.PriceIQD, &p.DurationDays, &p.RateDownKbps, &p.RateUpKbps, &p.CreatedAt); err != nil {
				d.Log.Error("profiles list scan failed", "error", err)
				httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			p.CreatedAt = p.CreatedAt.UTC()
			items = append(items, p)
		}
		if rows.Err() != nil {
			d.Log.Error("profiles list rows failed", "error", rows.Err())
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		next := ""
		if len(items) > page.Limit {
			items = items[:page.Limit]
			next = httpapi.EncodeCursor(items[len(items)-1].ID)
		}
		httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
	}
}
