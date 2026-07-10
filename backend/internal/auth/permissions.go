package auth

// Permission model — Phase 2 (contract C2). This phase ships hardcoded
// role→permission sets; the full matrix editor is Phase 3. The frozen
// contract is that code checks *permission strings* (`<module>.<verb>` and
// bare action perms), never role names — so Phase 3 can swap the source of
// the sets without touching a single call site.

// Built-in role names (FR-27.3). Editable copies become DB-backed roles in
// Phase 3; here they map to fixed permission sets.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleAgent    = "agent"
)

// Action permissions (contract C2): granted independently of module verbs.
const (
	PermRenew      = "renew"
	PermDisconnect = "disconnect"
	PermTopup      = "topup"
	PermExport     = "export"
)

// rolePermissions is the deny-by-default map for non-admin roles. Admin is
// allow-all and is intentionally absent (see roleCan). Keys are permission
// strings; presence == granted.
var rolePermissions = map[string]map[string]bool{
	// Operator = Sara (FR-27.3): subscriber view/create/edit, renew,
	// disconnect; read-only elsewhere; no settings, no managers, no deletes.
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
	// Agent = Hassan (FR-27.3): scoped subscriber view, renew, own
	// collection report. The `scoped` flag (per-manager) is what limits the
	// rows he sees; the role just grants the verbs.
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

// roleCan reports whether a role grants a permission string. Admin is
// unconditionally allowed; every other role is deny-by-default.
func roleCan(role, perm string) bool {
	if role == RoleAdmin {
		return true
	}
	return rolePermissions[role][perm]
}

// validRole reports whether role is one this phase understands. Manager CRUD
// rejects anything else so a typo can't silently create a permission-less
// account.
func validRole(role string) bool {
	switch role {
	case RoleAdmin, RoleOperator, RoleAgent:
		return true
	default:
		return false
	}
}
