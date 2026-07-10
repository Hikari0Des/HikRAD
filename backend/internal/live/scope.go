package live

// Manager-scope resolution for the DB-backed history/usage endpoints (FR-27.2,
// C2). ScopeFilter gives a manager id; a scoped caller may only see sessions of
// subscribers they own. owner_manager_id is D's column — until it lands the
// resolution returns an empty allow-set for a scoped caller (safe: they see
// nothing) while unscoped callers are unaffected.

import (
	"context"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// allowedSubscribers returns (nil, true) for an unscoped caller (no restriction)
// and (ids, false) for a scoped caller — the subscriber ids they own. An empty
// non-nil slice means "owns nothing / owner column absent" → queries return no
// rows.
func allowedSubscribers(ctx context.Context, db *pgxpool.Pool, scope *auth.ManagerScope) (ids []string, unscoped bool) {
	if scope == nil {
		return nil, true
	}
	out := []string{}
	if db == nil {
		return out, false
	}
	rows, err := db.Query(ctx, `SELECT id::text FROM subscribers WHERE owner_manager_id = $1::uuid`, scope.ManagerID)
	if err != nil {
		// owner_manager_id not present yet (pre-D): deny-by-default.
		return out, false
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, false
		}
		out = append(out, id)
	}
	return out, false
}
