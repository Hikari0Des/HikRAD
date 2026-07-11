package auth

// Per-manager security surface (C7): effective-permission view, per-manager
// permission overrides (FR-27.1), and the IP allowlist (FR-30). All mutations
// are audited and escalation-guarded; changes propagate within one access-token
// lifetime (they are re-embedded on refresh).

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type overrideEntry struct {
	Permission string `json:"permission"`
	Granted    bool   `json:"granted"`
}

func listOverrides(ctx context.Context, db *pgxpool.Pool, managerID string) ([]overrideEntry, error) {
	rows, err := db.Query(ctx,
		`SELECT permission, granted FROM manager_permission_overrides
		  WHERE manager_id = $1::uuid ORDER BY permission`, managerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []overrideEntry{}
	for rows.Next() {
		var e overrideEntry
		if err := rows.Scan(&e.Permission, &e.Granted); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func replaceOverrides(ctx context.Context, db *pgxpool.Pool, managerID string, entries []overrideEntry) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `DELETE FROM manager_permission_overrides WHERE manager_id = $1::uuid`, managerID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO manager_permission_overrides (manager_id, permission, granted) VALUES ($1::uuid, $2, $3)`,
			managerID, e.Permission, e.Granted); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func getManagerPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	ctx := r.Context()
	if _, err := lookupManagerByID(ctx, svc.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	overrides, err := listOverrides(ctx, svc.db, id)
	if err != nil {
		svc.log.Error("list overrides failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	effective, err := resolvePermissions(ctx, svc.db, id)
	if err != nil {
		svc.log.Error("resolve permissions failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"overrides": overrides,
		"effective": effective,
	})
}

type putOverridesRequest struct {
	Overrides []overrideEntry `json:"overrides"`
}

func putManagerOverridesHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	var req putOverridesRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	if _, err := lookupManagerByID(ctx, svc.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	// Validate + escalation-guard the granted permissions only.
	var granted []string
	for _, e := range req.Overrides {
		if !catalogSet[e.Permission] {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "unknown permission: "+e.Permission)
			return
		}
		if e.Granted {
			granted = append(granted, e.Permission)
		}
	}
	if denied := escalationCheck(ctx, granted); denied != "" {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you cannot grant a permission you do not hold: "+denied)
		return
	}
	before, _ := listOverrides(ctx, svc.db, id)
	if err := replaceOverrides(ctx, svc.db, id, req.Overrides); err != nil {
		svc.log.Error("replace overrides failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	after, _ := listOverrides(ctx, svc.db, id)
	_ = Audit(ctx, "managers.permissions_update", "manager", id, before, after)
	httpapi.JSON(w, http.StatusOK, map[string]any{"overrides": after})
}

func getManagerAllowlistHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	ctx := r.Context()
	if _, err := lookupManagerByID(ctx, svc.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	entries, err := listAllowlist(ctx, svc.db, id)
	if err != nil {
		svc.log.Error("list allowlist failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	httpapi.JSON(w, http.StatusOK, allowlistResponse(entries, r))
}

type putAllowlistRequest struct {
	Entries []allowlistEntry `json:"entries"`
}

func putManagerAllowlistHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
		return
	}
	var req putAllowlistRequest
	if !httpapi.Bind(w, r, &req) {
		return
	}
	ctx := r.Context()
	if _, err := lookupManagerByID(ctx, svc.db, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "manager not found")
			return
		}
		svc.log.Error("lookup manager failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	for _, e := range req.Entries {
		if !validCIDR(e.CIDR) {
			httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid CIDR: "+e.CIDR)
			return
		}
	}
	before, _ := listAllowlist(ctx, svc.db, id)
	if err := replaceAllowlist(ctx, svc.db, id, req.Entries); err != nil {
		svc.log.Error("replace allowlist failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	after, _ := listAllowlist(ctx, svc.db, id)
	_ = Audit(ctx, "managers.allowlist_update", "manager", id, before, after)
	httpapi.JSON(w, http.StatusOK, allowlistResponse(after, r))
}

// allowlistResponse packages the allowlist plus the self-edit warning (FR-30):
// whether the request's own current IP would be excluded by this list.
func allowlistResponse(entries []allowlistEntry, r *http.Request) map[string]any {
	cidrs := make([]string, 0, len(entries))
	for _, e := range entries {
		cidrs = append(cidrs, e.CIDR)
	}
	ip := clientIP(r)
	return map[string]any{
		"entries":            entries,
		"current_ip":         ip,
		"current_ip_allowed": ipAllowed(ip, cidrs),
	}
}
