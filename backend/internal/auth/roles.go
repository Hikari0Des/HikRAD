package auth

// Roles + permission-matrix data access (FR-27.1). A role is a named, reusable
// permission set; managers reference exactly one (flat v1, FR-27.4). Builtin
// roles are editable copies (FR-27.3), not hardcoded behaviour.

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// roleView is the API read shape for a role (with its permission matrix).
type roleView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsBuiltin   bool     `json:"is_builtin"`
	Require2FA  bool     `json:"require_2fa"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"member_count"`
}

// permissionCatalog is the full set of assignable permission strings, grouped
// by module, that the matrix UI renders (FR-27.1). The wildcard '*' is
// deliberately excluded — it is Admin-only and set via the builtin seed, not
// hand-assigned. Keep in sync with the Require(...) strings across modules.
var permissionCatalog = []struct {
	Module string   `json:"module"`
	Perms  []string `json:"permissions"`
}{
	{"subscribers", []string{"subscribers.view", "subscribers.create", "subscribers.edit", "subscribers.delete"}},
	{"profiles", []string{"profiles.view", "profiles.create", "profiles.edit", "profiles.delete"}},
	// nas.reboot (v4-6/FR-142) is deliberately separate from nas.edit — a
	// router reboot is a destructive action, not a config edit, so it must be
	// grantable (or withheld) independently. nas.update (v4-7/FR-147) is the
	// same posture applied to installing a RouterOS update: also reboots the
	// router, but is neither a config edit nor the reboot action itself, so
	// it gets its own independently grantable/withholdable permission too.
	{"nas", []string{"nas.view", "nas.create", "nas.edit", "nas.delete", "nas.reboot", "nas.update"}},
	{"pools", []string{"pools.view", "pools.create", "pools.edit", "pools.delete"}},
	{"billing", []string{"billing.view", "billing.create", "billing.edit",
		"currency_rates.manage", "overheads.manage", "reseller_prices.manage",
		"payment_providers.manage", "payment_tickets.verify"}},
	// v4-9 (FR-153-159): double-entry accounting, invoices/expenses, and the
	// agent approval gate. accounting.* covers the chart of accounts and
	// every statement (P&L/balance sheet/cash flow/aging/advanced); a
	// reseller/agent holding accounting.view still sees their own
	// statements with cost/margin fields omitted (gate item 12), never a
	// separate reseller permission. approvals.manage is the full queue;
	// an agent may always list/withdraw their OWN pending requests and
	// undo their OWN actions with no permission beyond authentication
	// (checked by ownership, not by grant — see billing/undo_api.go).
	{"accounting", []string{"accounting.view", "accounting.manage"}},
	{"invoices", []string{"invoices.view", "invoices.create", "invoices.manage", "invoices.no_charge"}},
	{"expenses", []string{"expenses.view", "expenses.create", "expenses.manage"}},
	{"approvals", []string{"approvals.manage"}},
	{"vouchers", []string{"vouchers.view", "vouchers.create", "vouchers.edit"}},
	{"cards", []string{"cards.view", "cards.create", "cards.edit", "cards.print"}},
	{"monitoring", []string{"monitoring.view", "monitoring.create", "monitoring.edit", "monitoring.delete"}},
	{"reports", []string{"reports.view"}},
	{"settings", []string{"settings.view", "settings.edit"}},
	{"system", []string{"license.manage", "backups.view", "backups.manage", "tunnel.manage", "system.update"}},
	{"managers", []string{"managers.view", "managers.create", "managers.edit", "managers.delete"}},
	{"live", []string{"live.view", "sessions.view"}},
	// v4-8 (FR-150-152): the CoA Outbox operations screen. Separate from
	// nas.* — it is a queue over every NAS's CoAs, not a per-router config
	// surface, so it gets its own grantable/withholdable pair.
	{"coa_outbox", []string{"coa_outbox.view", "coa_outbox.manage"}},
	// v4-10 (FR-160-165): the Notification Center (channels/templates/rules/
	// broadcast/outbox) and support tickets each get their own
	// grantable/withholdable pair — neither is a subset of any existing
	// module (billing/monitoring included), same posture as coa_outbox above.
	{"notifications", []string{"notifications.view", "notifications.manage"}},
	{"support", []string{"support.view", "support.manage"}},
	{"audit", []string{"audit.view"}},
	{"actions", []string{PermRenew, PermDisconnect, PermTopup, PermExport, "refund"}},
}

// catalogSet is every catalog permission as a lookup set, for validation.
var catalogSet = func() map[string]bool {
	s := map[string]bool{}
	for _, g := range permissionCatalog {
		for _, p := range g.Perms {
			s[p] = true
		}
	}
	return s
}()

// listRoles returns every role with its permissions and member count.
func listRoles(ctx context.Context, db *pgxpool.Pool) ([]roleView, error) {
	rows, err := db.Query(ctx,
		`SELECT r.id::text, r.name, r.description, r.is_builtin, r.require_2fa,
		        COALESCE(array_remove(array_agg(rp.permission), NULL), '{}') AS perms,
		        (SELECT count(*) FROM managers m WHERE m.role_id = r.id) AS members
		   FROM roles r
		   LEFT JOIN role_permissions rp ON rp.role_id = r.id
		  GROUP BY r.id
		  ORDER BY r.is_builtin DESC, r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []roleView{}
	for rows.Next() {
		var v roleView
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.IsBuiltin, &v.Require2FA, &v.Permissions, &v.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// getRole returns one role or pgx.ErrNoRows.
func getRole(ctx context.Context, db *pgxpool.Pool, id string) (roleView, error) {
	var v roleView
	err := db.QueryRow(ctx,
		`SELECT r.id::text, r.name, r.description, r.is_builtin, r.require_2fa,
		        COALESCE(array_remove(array_agg(rp.permission), NULL), '{}') AS perms,
		        (SELECT count(*) FROM managers m WHERE m.role_id = r.id) AS members
		   FROM roles r
		   LEFT JOIN role_permissions rp ON rp.role_id = r.id
		  WHERE r.id = $1::uuid
		  GROUP BY r.id`, id).
		Scan(&v.ID, &v.Name, &v.Description, &v.IsBuiltin, &v.Require2FA, &v.Permissions, &v.MemberCount)
	return v, err
}

// createRole inserts a role and its permissions.
func createRole(ctx context.Context, db *pgxpool.Pool, name, desc string, require2FA bool, perms []string) (string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO roles (name, description, is_builtin, require_2fa) VALUES ($1, $2, false, $3) RETURNING id::text`,
		name, desc, require2FA).Scan(&id); err != nil {
		return "", err
	}
	for _, p := range dedupe(perms) {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1::uuid, $2)`, id, p); err != nil {
			return "", err
		}
	}
	return id, tx.Commit(ctx)
}

// updateRole swaps a role's name/description/require_2fa and (if perms != nil)
// replaces its permission set.
func updateRole(ctx context.Context, db *pgxpool.Pool, id, name, desc string, require2FA bool, perms []string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx,
		`UPDATE roles SET name = $2, description = $3, require_2fa = $4 WHERE id = $1::uuid`,
		id, name, desc, require2FA); err != nil {
		return err
	}
	if perms != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1::uuid`, id); err != nil {
			return err
		}
		for _, p := range dedupe(perms) {
			if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1::uuid, $2)`, id, p); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// roleMemberCount returns how many managers are assigned to a role.
func roleMemberCount(ctx context.Context, db *pgxpool.Pool, id string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM managers WHERE role_id = $1::uuid`, id).Scan(&n)
	return n, err
}

// deleteRole removes a role (caller must have confirmed it is unused/non-builtin).
func deleteRole(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `DELETE FROM roles WHERE id = $1::uuid`, id)
	return err
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
