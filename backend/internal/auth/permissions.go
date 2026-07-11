package auth

// Permission model — Phase 3 (contract C7, FR-27). Phase 2 shipped hardcoded
// role→permission sets; this phase resolves the effective set from DB-backed
// editable roles (`roles`/`role_permissions`) plus per-manager overrides
// (`manager_permission_overrides`). The frozen contract is unchanged: code
// checks *permission strings* (`<module>.<verb>` and bare action perms), never
// role names, and deny-by-default holds.
//
// Resolution happens once, at login/refresh: the effective set is embedded in
// the access token (see tokens.go) and read by Manager.Can with no per-request
// DB hit. A role or override edit therefore takes effect within one
// access-token lifetime (≤ accessTTL) — the same propagation SLA as revocation.
//
// The in-memory sets below are retained as (a) the seed source of truth the
// 0210 migration mirrors and (b) a fallback for any Manager built without a
// resolved set (e.g. unit tests, or a legacy manager row with no role_id).

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Built-in role names (FR-27.3). These are seeded as editable DB rows by
// migration 0210; the constants remain for the fallback path and seeding.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleAgent    = "agent"
)

// permWildcard is the allow-all permission held by the Admin role.
const permWildcard = "*"

// Action permissions (contract C2/C7): granted independently of module verbs.
const (
	PermRenew      = "renew"
	PermDisconnect = "disconnect"
	PermTopup      = "topup"
	PermExport     = "export"
)

// rolePermissions is the deny-by-default fallback map for non-admin builtin
// roles, mirroring the seeded role_permissions rows. Admin is allow-all and is
// intentionally absent (see roleCan). Keys are permission strings.
var rolePermissions = map[string]map[string]bool{
	RoleOperator: setOf(
		"subscribers.view", "subscribers.create", "subscribers.edit",
		"profiles.view",
		"nas.view",
		"pools.view",
		"live.view",
		"sessions.view",
		"reports.view",
		"audit.view",
		PermRenew, PermDisconnect, PermTopup, PermExport,
	),
	RoleAgent: setOf(
		"subscribers.view",
		"reports.view",
		PermRenew,
	),
}

func setOf(perms ...string) map[string]bool {
	m := make(map[string]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// roleCan reports whether a builtin role name grants a permission string. Used
// only as the fallback when a Manager carries no resolved permission set (see
// Manager.Can). Admin is unconditionally allowed; every other role is
// deny-by-default.
func roleCan(role, perm string) bool {
	if role == RoleAdmin {
		return true
	}
	return rolePermissions[role][perm]
}

// resolvePermissions computes a manager's effective permission set: the
// permissions of their assigned role, then per-manager overrides applied on top
// (granted adds, !granted removes). Returns a deduplicated, unordered slice.
//
// A manager with no role_id (legacy row) resolves from the in-memory fallback
// keyed on the legacy role text so authorization never silently opens up.
func resolvePermissions(ctx context.Context, db *pgxpool.Pool, managerID string) ([]string, error) {
	set := map[string]bool{}

	rows, err := db.Query(ctx,
		`SELECT rp.permission
		   FROM managers m
		   JOIN role_permissions rp ON rp.role_id = m.role_id
		  WHERE m.id = $1::uuid`, managerID)
	if err != nil {
		return nil, err
	}
	hadRole := false
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		set[p] = true
		hadRole = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Legacy manager with no role_id: fall back to the builtin set by role text.
	if !hadRole {
		var role string
		if err := db.QueryRow(ctx, `SELECT role FROM managers WHERE id = $1::uuid`, managerID).Scan(&role); err == nil {
			if role == RoleAdmin {
				set[permWildcard] = true
			}
			for p := range rolePermissions[role] {
				set[p] = true
			}
		}
	}

	// Per-manager overrides layered last.
	orows, err := db.Query(ctx,
		`SELECT permission, granted FROM manager_permission_overrides WHERE manager_id = $1::uuid`, managerID)
	if err != nil {
		return nil, err
	}
	for orows.Next() {
		var p string
		var granted bool
		if err := orows.Scan(&p, &granted); err != nil {
			orows.Close()
			return nil, err
		}
		if granted {
			set[p] = true
		} else {
			delete(set, p)
		}
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	return out, nil
}
