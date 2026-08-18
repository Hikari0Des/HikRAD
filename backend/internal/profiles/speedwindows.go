package profiles

// Speed windows (FR-131, migration 0599) — D owns the schema and CRUD; B's
// policy engine and continuous worker consume this subscriber's resolved
// profile's window set through the AuthView field D's subscriber loader
// populates (subscribers/limits.go), NOT through a provider seam here — a
// subscriber's own profile's windows are already part of the same read
// D resolves at auth time, so there is nothing this package needs to inject
// into B the way tod.go's TODProvider is (that seam exists because the TOD
// sweep needs to enumerate windows PROFILE-first, across every profile, to
// find boundary crossings; the v4-4 limits sweep instead walks online
// SESSIONS first and reuses the same per-subscriber AuthView load — see
// radius/limitsworker.go's package doc). This file is CRUD only.

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

type speedWindow struct {
	ID           string `json:"id"`
	ProfileID    string `json:"profile_id"`
	Label        string `json:"label"`
	DaysMask     int    `json:"days_mask"`
	StartMin     int    `json:"start_min"`
	EndMin       int    `json:"end_min"`
	RateDownKbps int    `json:"rate_down_kbps"`
	RateUpKbps   int    `json:"rate_up_kbps"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
}

type speedWindowInput struct {
	Label        string `json:"label"`
	DaysMask     int    `json:"days_mask"`
	StartMin     int    `json:"start_min"`
	EndMin       int    `json:"end_min"`
	RateDownKbps int    `json:"rate_down_kbps"`
	RateUpKbps   int    `json:"rate_up_kbps"`
	Enabled      *bool  `json:"enabled"`
}

func (in *speedWindowInput) validate() []httpapi.FieldError {
	var fe []httpapi.FieldError
	add := func(f, m string) { fe = append(fe, httpapi.FieldError{Field: f, Message: m}) }
	if in.StartMin < 0 || in.StartMin > 1439 {
		add("start_min", "must be 0..1439 (minutes since local midnight)")
	}
	if in.EndMin < 0 || in.EndMin > 1439 {
		add("end_min", "must be 0..1439 (minutes since local midnight)")
	}
	if in.DaysMask < 0 || in.DaysMask > 127 {
		add("days_mask", "must be 0..127 (a bitmask over Sunday..Saturday; 0 = every day)")
	}
	if in.RateDownKbps <= 0 {
		add("rate_down_kbps", "must be a positive rate")
	}
	if in.RateUpKbps <= 0 {
		add("rate_up_kbps", "must be a positive rate")
	}
	return fe
}

func (m *Module) listSpeedWindowsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, profile_id::text, label, days_mask, start_min, end_min,
		        rate_down_kbps, rate_up_kbps, enabled,
		        to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		   FROM profile_speed_windows WHERE profile_id = $1::uuid ORDER BY start_min`,
		chi.URLParam(r, "id"))
	if err != nil {
		m.internalError(w, "list speed windows", err)
		return
	}
	defer rows.Close()
	items := []speedWindow{}
	for rows.Next() {
		var sw speedWindow
		if err := rows.Scan(&sw.ID, &sw.ProfileID, &sw.Label, &sw.DaysMask, &sw.StartMin, &sw.EndMin,
			&sw.RateDownKbps, &sw.RateUpKbps, &sw.Enabled, &sw.CreatedAt); err != nil {
			m.internalError(w, "scan speed window", err)
			return
		}
		items = append(items, sw)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) createSpeedWindowHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	var in speedWindowInput
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if fe := in.validate(); fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var id string
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO profile_speed_windows
		   (profile_id, label, days_mask, start_min, end_min, rate_down_kbps, rate_up_kbps, enabled)
		 VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
		profileID, in.Label, in.DaysMask, in.StartMin, in.EndMin, in.RateDownKbps, in.RateUpKbps, enabled).
		Scan(&id)
	if err != nil {
		m.internalError(w, "create speed window", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.speed_window.create", "profile", profileID, nil, map[string]any{
		"window_id": id, "start_min": in.StartMin, "end_min": in.EndMin, "days_mask": in.DaysMask,
		"rate_down_kbps": in.RateDownKbps, "rate_up_kbps": in.RateUpKbps,
	})
	// A subscriber's AuthView embeds their profile's windows (subscribers/
	// limits.go's jsonb_agg), so a window change must invalidate the same
	// way any other profile-policy edit does (contract C11).
	m.invalidateProfileViews(r.Context(), profileID)
	httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (m *Module) deleteSpeedWindowHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	wid := chi.URLParam(r, "wid")
	ct, err := m.db.Exec(r.Context(),
		`DELETE FROM profile_speed_windows WHERE id = $1::uuid AND profile_id = $2::uuid`, wid, profileID)
	if err != nil {
		m.internalError(w, "delete speed window", err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpapi.Error(w, http.StatusNotFound, "not_found", "window not found")
		return
	}
	_ = auth.Audit(r.Context(), "profile.speed_window.delete", "profile", profileID, map[string]any{"window_id": wid}, nil)
	m.invalidateProfileViews(r.Context(), profileID)
	w.WriteHeader(http.StatusNoContent)
}

// duplicateSpeedWindows copies every enabled+disabled window from one
// profile to another as part of FR-132's one-click duplication — a
// duplicate should behave identically to the original until edited.
func duplicateSpeedWindows(ctx context.Context, tx pgx.Tx, fromProfileID, toProfileID string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO profile_speed_windows (profile_id, label, days_mask, start_min, end_min, rate_down_kbps, rate_up_kbps, enabled)
		 SELECT $2::uuid, label, days_mask, start_min, end_min, rate_down_kbps, rate_up_kbps, enabled
		   FROM profile_speed_windows WHERE profile_id = $1::uuid`,
		fromProfileID, toProfileID)
	return err
}
