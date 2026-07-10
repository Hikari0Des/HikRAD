package auth

// Minimal manager CRUD (task 6): enough to create/edit operator & agent
// accounts and reset passwords for Phase-2/3 testing. The full roles editor is
// Phase 3. All routes are permission-gated (managers.*) — only admins hold
// those permissions this phase — and every mutation is audited.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func listManagersHandler(w http.ResponseWriter, r *http.Request) {
	views, err := listManagerViews(r.Context(), svc.db)
	if err != nil {
		svc.log.Error("list managers failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": views})
}

type createManagerRequest struct {
	Username string `json:"username" validate:"required,min=3,max=64"`
	Password string `json:"password" validate:"required,min=8,max=256"`
	Role     string `json:"role" validate:"required,oneof=admin operator agent"`
	Scoped   bool   `json:"scoped"`
}

func createManagerHandler(w http.ResponseWriter, r *http.Request) {
	var req createManagerRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	if !validRole(req.Role) {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid role")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		svc.log.Error("hash password failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	view, err := insertManager(r.Context(), svc.db, req.Username, hash, req.Role, req.Scoped)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			httpapi.Error(w, http.StatusConflict, "conflict", "username already exists")
			return
		}
		svc.log.Error("insert manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(r.Context(), "managers.create", "manager", view.ID, nil, view)
	httpapi.JSON(w, http.StatusCreated, view)
}

type updateManagerRequest struct {
	Role     *string `json:"role"`
	Scoped   *bool   `json:"scoped"`
	Password *string `json:"password"`
}

func updateManagerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	var req updateManagerRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	actor, _ := ManagerFrom(ctx)

	before, err := lookupManagerByID(ctx, svc.db, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	role := before.Role
	if req.Role != nil {
		if !validRole(*req.Role) {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid role")
			return
		}
		role = *req.Role
	}
	scoped := before.Scoped
	if req.Scoped != nil {
		scoped = *req.Scoped
	}

	view, err := updateManagerRoleScope(ctx, svc.db, id, role, scoped)
	if err != nil {
		svc.log.Error("update manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	// Password reset (task 7): revoke sessions. If the admin is changing their
	// own password, keep the current session; otherwise revoke all of the
	// target's sessions so a reset actually logs them out.
	if req.Password != nil {
		if len(*req.Password) < 8 {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "password must be at least 8 characters")
			return
		}
		hash, herr := hashPassword(*req.Password)
		if herr != nil {
			svc.log.Error("hash password failed", "error", herr)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		if uerr := updatePasswordHash(ctx, svc.db, id, hash); uerr != nil {
			svc.log.Error("reset password failed", "error", uerr)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		keep := ""
		if actor != nil && actor.ID == id {
			keep = actor.SessionID
		}
		if rerr := revokeOtherSessions(ctx, svc.db, id, keep); rerr != nil {
			svc.log.Error("revoke sessions on password reset failed", "error", rerr)
		}
	}

	_ = Audit(ctx, "managers.update", "manager", id, before.redactable(), view)
	httpapi.JSON(w, http.StatusOK, view)
}

// unlockManagerHandler clears an account lockout (FR-28.2 admin unlock).
func unlockManagerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	m, err := lookupManagerByID(r.Context(), svc.db, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if uerr := svc.limiter.unlockAccount(r.Context(), m.Username); uerr != nil {
		svc.log.Error("unlock failed", "error", uerr)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(r.Context(), "managers.unlock", "manager", id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// redactable exposes only non-secret manager fields for an audit before-image.
func (m *managerAuthRow) redactable() managerBefore {
	return managerBefore{ID: m.ID, Username: m.Username, Role: m.Role, Scoped: m.Scoped}
}

type managerBefore struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Scoped   bool   `json:"scoped"`
	// PasswordHash is deliberately omitted from the audit image; if it were
	// ever added it must carry `audit:"secret"`.
}
