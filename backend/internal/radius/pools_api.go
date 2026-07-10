package radius

// IP pool REST handlers (C7-B / FR-16) and the static-IP validation service D
// calls. Mutations are audited (C2).

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pkgDB is the pool used by exposed package-level helpers (StaticIPInStaticPool)
// D calls without an httpapi request. Set in Register.
var pkgDB *pgxpool.Pool

type poolRequest struct {
	Name    string   `json:"name" validate:"required,min=1,max=128"`
	Ranges  []string `json:"ranges" validate:"required,min=1,dive,required"`
	Purpose string   `json:"purpose" validate:"omitempty,oneof=active expired static"`
}

func (m *module) listPoolsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := listPools(r.Context(), m.db)
	if err != nil {
		m.internal(w, "list pools", err)
		return
	}
	usage := currentPoolUsage()
	views := make([]poolView, 0, len(rows))
	for _, p := range rows {
		views = append(views, p.view(usage(p.Name)))
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": views})
}

func (m *module) getPoolHandler(w http.ResponseWriter, r *http.Request) {
	p, err := getPool(r.Context(), m.db, chi.URLParam(r, "id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "pool not found")
		return
	}
	if err != nil {
		m.internal(w, "get pool", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p.view(currentPoolUsage()(p.Name)))
}

func (m *module) createPoolHandler(w http.ResponseWriter, r *http.Request) {
	var req poolRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ranges, err := validateRanges(req.Ranges)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(),
			httpapi.FieldError{Field: "ranges", Message: err.Error()})
		return
	}
	purpose := req.Purpose
	if purpose == "" {
		purpose = "active"
	}
	p, err := insertPool(r.Context(), m.db, req.Name, ranges, purpose)
	if err != nil {
		if isUniqueViolation(err) {
			httpapi.Error(w, http.StatusConflict, "conflict", "a pool with that name already exists")
			return
		}
		m.internal(w, "insert pool", err)
		return
	}
	view := p.view(currentPoolUsage()(p.Name))
	_ = auth.Audit(r.Context(), "pools.create", "pool", p.ID, nil, view)
	httpapi.JSON(w, http.StatusCreated, view)
}

func (m *module) updatePoolHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, err := getPool(r.Context(), m.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "pool not found")
		return
	}
	if err != nil {
		m.internal(w, "lookup pool", err)
		return
	}
	var req poolRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ranges, err := validateRanges(req.Ranges)
	if err != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(),
			httpapi.FieldError{Field: "ranges", Message: err.Error()})
		return
	}
	purpose := req.Purpose
	if purpose == "" {
		purpose = before.Purpose
	}
	p, err := updatePool(r.Context(), m.db, id, req.Name, ranges, purpose)
	if err != nil {
		if isUniqueViolation(err) {
			httpapi.Error(w, http.StatusConflict, "conflict", "a pool with that name already exists")
			return
		}
		m.internal(w, "update pool", err)
		return
	}
	usage := currentPoolUsage()
	_ = auth.Audit(r.Context(), "pools.update", "pool", id, before.view(usage(before.Name)), p.view(usage(p.Name)))
	httpapi.JSON(w, http.StatusOK, p.view(usage(p.Name)))
}

func (m *module) deletePoolHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	before, err := getPool(r.Context(), m.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "pool not found")
		return
	}
	if err != nil {
		m.internal(w, "lookup pool", err)
		return
	}
	if err := deletePool(r.Context(), m.db, id); err != nil {
		m.internal(w, "delete pool", err)
		return
	}
	_ = auth.Audit(r.Context(), "pools.delete", "pool", id, before.view(0), nil)
	w.WriteHeader(http.StatusNoContent)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// StaticIPInStaticPool reports whether ip is inside a static-purpose pool — the
// B-owned half of the FR-16.2 static-IP validation D calls before saving a
// subscriber's static IP (D still enforces per-subscriber uniqueness). A
// static IP outside every static pool is rejected by D.
func StaticIPInStaticPool(ctx context.Context, ip string) (bool, error) {
	if pkgDB == nil {
		return false, errors.New("radius: pool store not configured")
	}
	rows, err := listPools(ctx, pkgDB)
	if err != nil {
		return false, err
	}
	for _, p := range rows {
		if p.Purpose == "static" && staticIPInPool(ip, p.Ranges) {
			return true, nil
		}
	}
	return false, nil
}
