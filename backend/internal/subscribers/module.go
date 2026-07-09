// Package subscribers is the subscriber domain module. Phase 1 ships only a
// placeholder list endpoint that proves the httpapi framework end to end;
// full CRUD (FR-1..FR-12) arrives in Phase 2 from this same agent role.
package subscribers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type Module struct{}

func (Module) Name() string { return "subscribers" }

func (Module) Register(r chi.Router, d httpapi.Deps) {
	r.With(httpapi.RequireAuth).Get("/api/v1/subscribers", listHandler(d))
}

func init() { httpapi.Add(Module{}) }

// Subscriber is the Phase-1 read shape (schema per contract C6; password_enc
// is never serialized).
type Subscriber struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Name      *string    `json:"name"`
	Phone     *string    `json:"phone"`
	Status    string     `json:"status"`
	ProfileID *string    `json:"profile_id"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func listHandler(d httpapi.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpapi.ParsePage(r)
		if err != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
			return
		}
		// Keyset over the primary key: stable under concurrent inserts.
		var after *string
		if len(page.Cursor) == 1 {
			after = &page.Cursor[0]
		} else if page.Cursor != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		rows, err := d.DB.Query(r.Context(),
			`SELECT id::text, username::text, name, phone, status, profile_id::text, expires_at, created_at
			   FROM subscribers
			  WHERE $1::uuid IS NULL OR id > $1::uuid
			  ORDER BY id
			  LIMIT $2`,
			after, page.Limit+1)
		if err != nil {
			d.Log.Error("subscribers list query failed", "error", err)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		defer rows.Close()

		items := make([]Subscriber, 0, page.Limit)
		for rows.Next() {
			var s Subscriber
			if err := rows.Scan(&s.ID, &s.Username, &s.Name, &s.Phone, &s.Status, &s.ProfileID, &s.ExpiresAt, &s.CreatedAt); err != nil {
				d.Log.Error("subscribers list scan failed", "error", err)
				httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			s.CreatedAt = s.CreatedAt.UTC()
			if s.ExpiresAt != nil {
				utc := s.ExpiresAt.UTC()
				s.ExpiresAt = &utc
			}
			items = append(items, s)
		}
		if rows.Err() != nil {
			d.Log.Error("subscribers list rows failed", "error", rows.Err())
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
