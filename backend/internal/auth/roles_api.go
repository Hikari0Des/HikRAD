package auth

// Roles API (C7 / FR-27.1): CRUD over roles + their permission matrix, plus
// the permission catalog the matrix UI renders. Every mutation is audited. The
// privilege-escalation guard ensures an editor cannot grant a permission they
// do not themselves hold.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func listPermissionCatalogHandler(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, http.StatusOK, map[string]any{"modules": permissionCatalog})
}

func listRolesHandler(w http.ResponseWriter, r *http.Request) {
	roles, err := listRoles(r.Context(), svc.db)
	if err != nil {
		svc.log.Error("list roles failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": roles})
}

func getRoleHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
		return
	}
	role, err := getRole(r.Context(), svc.db, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
			return
		}
		svc.log.Error("get role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, role)
}

type createRoleRequest struct {
	Name        string   `json:"name" validate:"required,min=2,max=64"`
	Description string   `json:"description" validate:"max=256"`
	Require2FA  bool     `json:"require_2fa"`
	Permissions []string `json:"permissions"`
}

func createRoleHandler(w http.ResponseWriter, r *http.Request) {
	var req createRoleRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	if bad := invalidPermissions(req.Permissions); bad != "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "unknown permission: "+bad)
		return
	}
	// Escalation guard (FR-27.1): the editor cannot grant what they lack.
	if denied := escalationCheck(ctx, req.Permissions); denied != "" {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you cannot grant a permission you do not hold: "+denied)
		return
	}
	id, err := createRole(ctx, svc.db, req.Name, req.Description, req.Require2FA, req.Permissions)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpapi.Error(w, http.StatusConflict, "conflict", "a role with that name already exists")
			return
		}
		svc.log.Error("create role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	role, _ := getRole(ctx, svc.db, id)
	_ = Audit(ctx, "roles.create", "role", id, nil, role)
	httpapi.JSON(w, http.StatusCreated, role)
}

type updateRoleRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Require2FA  *bool     `json:"require_2fa"`
	Permissions *[]string `json:"permissions"`
}

func updateRoleHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
		return
	}
	var req updateRoleRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()

	before, err := getRole(ctx, svc.db, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
			return
		}
		svc.log.Error("get role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	name, desc, require2FA := before.Name, before.Description, before.Require2FA
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		desc = *req.Description
	}
	if req.Require2FA != nil {
		require2FA = *req.Require2FA
	}

	var perms []string
	if req.Permissions != nil {
		perms = *req.Permissions
		if bad := invalidPermissions(perms); bad != "" {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "unknown permission: "+bad)
			return
		}
		// Only newly-added permissions are escalation-checked (removing is fine).
		added := diffAdded(before.Permissions, perms)
		if denied := escalationCheck(ctx, added); denied != "" {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "you cannot grant a permission you do not hold: "+denied)
			return
		}
	}

	if err := updateRole(ctx, svc.db, id, name, desc, require2FA, perms); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpapi.Error(w, http.StatusConflict, "conflict", "a role with that name already exists")
			return
		}
		svc.log.Error("update role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	after, _ := getRole(ctx, svc.db, id)
	_ = Audit(ctx, "roles.update", "role", id, before, after)
	httpapi.JSON(w, http.StatusOK, after)
}

func deleteRoleHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
		return
	}
	ctx := r.Context()
	role, err := getRole(ctx, svc.db, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "role not found")
			return
		}
		svc.log.Error("get role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if role.IsBuiltin {
		httpapi.Error(w, http.StatusConflict, "conflict", "builtin roles cannot be deleted")
		return
	}
	n, err := roleMemberCount(ctx, svc.db, id)
	if err != nil {
		svc.log.Error("role member count failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if n > 0 {
		// Deleting an in-use role is blocked; reassign its managers first.
		httpapi.Error(w, http.StatusConflict, "role_in_use", "reassign the managers on this role before deleting it")
		return
	}
	if err := deleteRole(ctx, svc.db, id); err != nil {
		svc.log.Error("delete role failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(ctx, "roles.delete", "role", id, role, nil)
	w.WriteHeader(http.StatusNoContent)
}

// invalidPermissions returns the first permission not in the catalog, or "".
func invalidPermissions(perms []string) string {
	for _, p := range perms {
		if !catalogSet[p] {
			return p
		}
	}
	return ""
}

// escalationCheck returns the first permission the current actor does not hold,
// or "" if they may grant them all. An actor with '*' may grant anything.
func escalationCheck(ctx context.Context, perms []string) string {
	m, ok := ManagerFrom(ctx)
	if !ok {
		if len(perms) > 0 {
			return perms[0]
		}
		return ""
	}
	for _, p := range perms {
		if !m.Can(p) {
			return p
		}
	}
	return ""
}

// diffAdded returns permissions in next that are not in prev.
func diffAdded(prev, next []string) []string {
	have := map[string]bool{}
	for _, p := range prev {
		have[p] = true
	}
	var added []string
	for _, p := range next {
		if !have[p] {
			added = append(added, p)
		}
	}
	return added
}
