package profiles

// Time-of-day windows (FR-11) — D owns the schema (migration 0203) and read-model;
// B's boundary sweeps consume it through the injected radius.TODProvider seam
// (radius/tod.go). D also exposes window CRUD on the profile so operators can
// configure e.g. free night speed 00:00–06:00. A window edit is additive to the
// auth-time policy (already correct via AuthView burst/rate); the sweep only
// re-rates already-online sessions at the boundary.

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5/pgxpool"
)

// todProvider implements radius.TODProvider.
type todProvider struct{ db *pgxpool.Pool }

// TODWindows returns every enabled window across all profiles (contract C4).
func (t *todProvider) TODWindows(ctx context.Context) ([]radius.TODWindow, error) {
	rows, err := t.db.Query(ctx,
		`SELECT profile_id::text, label, start_min, end_min, boost_rate, normal_rate, exempt
		   FROM profile_tod_windows WHERE enabled ORDER BY profile_id, start_min`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []radius.TODWindow
	for rows.Next() {
		var w radius.TODWindow
		if err := rows.Scan(&w.ProfileID, &w.Label, &w.StartMin, &w.EndMin,
			&w.BoostRate, &w.NormalRate, &w.Exempt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ProfilesForSubscribers maps subscriber id → profile id (contract C4).
func (t *todProvider) ProfilesForSubscribers(ctx context.Context, subIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(subIDs))
	if len(subIDs) == 0 {
		return out, nil
	}
	rows, err := t.db.Query(ctx,
		`SELECT id::text, profile_id::text FROM subscribers
		  WHERE id = ANY($1::uuid[]) AND profile_id IS NOT NULL`, subIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid, pid string
		if err := rows.Scan(&sid, &pid); err != nil {
			return nil, err
		}
		out[sid] = pid
	}
	return out, rows.Err()
}

// --- Window CRUD ------------------------------------------------------------

type todWindow struct {
	ID         string `json:"id"`
	ProfileID  string `json:"profile_id"`
	Label      string `json:"label"`
	StartMin   int    `json:"start_min"`
	EndMin     int    `json:"end_min"`
	BoostRate  string `json:"boost_rate"`
	NormalRate string `json:"normal_rate"`
	Exempt     bool   `json:"exempt"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
}

type todWindowInput struct {
	Label      string `json:"label"`
	StartMin   int    `json:"start_min"`
	EndMin     int    `json:"end_min"`
	BoostRate  string `json:"boost_rate"`
	NormalRate string `json:"normal_rate"`
	Exempt     bool   `json:"exempt"`
	Enabled    *bool  `json:"enabled"`
}

func (in *todWindowInput) validate() []httpapi.FieldError {
	var fe []httpapi.FieldError
	if in.StartMin < 0 || in.StartMin > 1439 {
		fe = append(fe, httpapi.FieldError{Field: "start_min", Message: "must be 0..1439 (minutes since local midnight)"})
	}
	if in.EndMin < 0 || in.EndMin > 1439 {
		fe = append(fe, httpapi.FieldError{Field: "end_min", Message: "must be 0..1439 (minutes since local midnight)"})
	}
	return fe
}

func (m *Module) listTODHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, profile_id::text, label, start_min, end_min, boost_rate, normal_rate,
		        exempt, enabled,
		        to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		   FROM profile_tod_windows WHERE profile_id = $1::uuid ORDER BY start_min`,
		chi.URLParam(r, "id"))
	if err != nil {
		m.internalError(w, "list tod", err)
		return
	}
	defer rows.Close()
	items := []todWindow{}
	for rows.Next() {
		var win todWindow
		if err := rows.Scan(&win.ID, &win.ProfileID, &win.Label, &win.StartMin, &win.EndMin,
			&win.BoostRate, &win.NormalRate, &win.Exempt, &win.Enabled, &win.CreatedAt); err != nil {
			m.internalError(w, "scan tod", err)
			return
		}
		items = append(items, win)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) createTODHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	var in todWindowInput
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
		`INSERT INTO profile_tod_windows
		   (profile_id, label, start_min, end_min, boost_rate, normal_rate, exempt, enabled)
		 VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
		profileID, in.Label, in.StartMin, in.EndMin, in.BoostRate, in.NormalRate, in.Exempt, enabled).
		Scan(&id)
	if err != nil {
		m.internalError(w, "create tod", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.tod.create", "profile", profileID, nil, map[string]any{
		"window_id": id, "start_min": in.StartMin, "end_min": in.EndMin, "exempt": in.Exempt,
	})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (m *Module) deleteTODHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	wid := chi.URLParam(r, "wid")
	ct, err := m.db.Exec(r.Context(),
		`DELETE FROM profile_tod_windows WHERE id = $1::uuid AND profile_id = $2::uuid`, wid, profileID)
	if err != nil {
		m.internalError(w, "delete tod", err)
		return
	}
	if ct.RowsAffected() == 0 {
		httpapi.Error(w, http.StatusNotFound, "not_found", "window not found")
		return
	}
	_ = auth.Audit(r.Context(), "profile.tod.delete", "profile", profileID, map[string]any{"window_id": wid}, nil)
	w.WriteHeader(http.StatusNoContent)
}
