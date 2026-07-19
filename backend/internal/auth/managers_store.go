package auth

// managers table data access (this agent owns the managers security columns;
// the base table is Phase-1 contract C6). Password hashes are argon2id for new
// writes; legacy bcrypt (from the seed) is upgraded on login.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type managerAuthRow struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Scoped       bool
	Disabled     bool
	// TOTP + 2FA-enforcement fields (FR-28.1), loaded for the login flow.
	TOTPEnabled    bool
	TOTPSecretEnc  []byte
	RoleRequire2FA bool
}

// managerView is the API read shape (no secret material).
type managerView struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Role        string    `json:"role"`
	RoleID      *string   `json:"role_id"`
	Scoped      bool      `json:"scoped"`
	Disabled    bool      `json:"disabled"`
	TOTPEnabled bool      `json:"totp_enabled"`
	CreatedAt   time.Time `json:"created_at"`
	FullName    *string   `json:"full_name"`
	Phone       *string   `json:"phone"`
	Email       *string   `json:"email"`
	Address     *string   `json:"address"`
	Notes       *string   `json:"notes"`
}

// managerProfile carries the optional contact fields (migration 0591). Nil =
// leave unchanged on update / absent on create.
type managerProfile struct {
	FullName *string
	Phone    *string
	Email    *string
	Address  *string
	Notes    *string
}

const managerAuthCols = `m.id::text, m.username::text, m.password_hash, m.role, m.scoped, m.disabled,
	 m.totp_enabled, m.totp_secret_enc, COALESCE(r.require_2fa, false)`

func scanManagerAuth(row interface {
	Scan(dest ...any) error
}) (*managerAuthRow, error) {
	var m managerAuthRow
	if err := row.Scan(&m.ID, &m.Username, &m.PasswordHash, &m.Role, &m.Scoped, &m.Disabled,
		&m.TOTPEnabled, &m.TOTPSecretEnc, &m.RoleRequire2FA); err != nil {
		return nil, err
	}
	return &m, nil
}

func lookupManagerByUsername(ctx context.Context, db *pgxpool.Pool, username string) (*managerAuthRow, error) {
	return scanManagerAuth(db.QueryRow(ctx,
		`SELECT `+managerAuthCols+`
		   FROM managers m LEFT JOIN roles r ON r.id = m.role_id
		  WHERE m.username = $1`, username))
}

func lookupManagerByID(ctx context.Context, db *pgxpool.Pool, id string) (*managerAuthRow, error) {
	return scanManagerAuth(db.QueryRow(ctx,
		`SELECT `+managerAuthCols+`
		   FROM managers m LEFT JOIN roles r ON r.id = m.role_id
		  WHERE m.id = $1::uuid`, id))
}

// updatePasswordHash re-points a manager's stored hash (used by the argon2id
// upgrade path on successful login, and by password reset).
func updatePasswordHash(ctx context.Context, db *pgxpool.Pool, id, hash string) error {
	_, err := db.Exec(ctx, `UPDATE managers SET password_hash = $2 WHERE id = $1::uuid`, id, hash)
	return err
}

// managerViewCols is aliased for SELECTs over `managers m`; managerViewColsRet
// is the same list unqualified, for INSERT/UPDATE ... RETURNING (no alias in
// scope there). Both scan in the order scanManagerView expects.
const managerViewCols = `m.id::text, m.username::text, m.role, m.role_id::text, m.scoped, m.disabled, m.totp_enabled, m.created_at,
	 m.full_name, m.phone, m.email, m.address, m.notes`
const managerViewColsRet = `id::text, username::text, role, role_id::text, scoped, disabled, totp_enabled, created_at,
	 full_name, phone, email, address, notes`

func scanManagerView(row interface {
	Scan(dest ...any) error
}) (managerView, error) {
	var v managerView
	err := row.Scan(&v.ID, &v.Username, &v.Role, &v.RoleID, &v.Scoped, &v.Disabled, &v.TOTPEnabled, &v.CreatedAt,
		&v.FullName, &v.Phone, &v.Email, &v.Address, &v.Notes)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, err
}

// insertManager creates a manager assigned to the role named `role` (role text
// kept for display/back-compat; role_id is the authoritative link).
func insertManager(ctx context.Context, db *pgxpool.Pool, username, hash, role string, scoped bool, p managerProfile) (managerView, error) {
	return scanManagerView(db.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role, role_id, scoped, full_name, phone, email, address, notes)
		 VALUES ($1, $2, $3, (SELECT id FROM roles WHERE name = $3), $4, $5, $6, $7, $8, $9)
		 RETURNING `+managerViewColsRet,
		username, hash, role, scoped, p.FullName, p.Phone, p.Email, p.Address, p.Notes))
}

// updateManagerProfile writes only the profile fields present (non-nil) in p,
// returning the new view. An explicit empty string clears a field.
func updateManagerProfile(ctx context.Context, db *pgxpool.Pool, id string, p managerProfile) (managerView, error) {
	return scanManagerView(db.QueryRow(ctx,
		`UPDATE managers SET
		    full_name = CASE WHEN $2::bool THEN NULLIF($3, '') ELSE full_name END,
		    phone     = CASE WHEN $4::bool THEN NULLIF($5, '') ELSE phone END,
		    email     = CASE WHEN $6::bool THEN NULLIF($7, '') ELSE email END,
		    address   = CASE WHEN $8::bool THEN NULLIF($9, '') ELSE address END,
		    notes     = CASE WHEN $10::bool THEN NULLIF($11, '') ELSE notes END
		  WHERE id = $1::uuid
		 RETURNING `+managerViewColsRet,
		id,
		p.FullName != nil, strOrEmpty(p.FullName),
		p.Phone != nil, strOrEmpty(p.Phone),
		p.Email != nil, strOrEmpty(p.Email),
		p.Address != nil, strOrEmpty(p.Address),
		p.Notes != nil, strOrEmpty(p.Notes)))
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getManagerView(ctx context.Context, db *pgxpool.Pool, id string) (managerView, error) {
	return scanManagerView(db.QueryRow(ctx,
		`SELECT `+managerViewCols+` FROM managers m WHERE m.id = $1::uuid`, id))
}

func listManagerViews(ctx context.Context, db *pgxpool.Pool) ([]managerView, error) {
	rows, err := db.Query(ctx,
		`SELECT `+managerViewCols+` FROM managers m ORDER BY m.created_at, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []managerView{}
	for rows.Next() {
		v, err := scanManagerView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// updateManagerRoleScope updates role (by name) and/or scoped, returning the
// new view. role_id is kept in sync with the role text.
func updateManagerRoleScope(ctx context.Context, db *pgxpool.Pool, id, role string, scoped bool) (managerView, error) {
	return scanManagerView(db.QueryRow(ctx,
		`UPDATE managers
		    SET role = $2, role_id = (SELECT id FROM roles WHERE name = $2), scoped = $3
		  WHERE id = $1::uuid
		 RETURNING `+managerViewColsRet,
		id, role, scoped))
}

// setManagerDisabled flips the disabled flag, returning the new view. Session
// revocation on disable is the caller's job.
func setManagerDisabled(ctx context.Context, db *pgxpool.Pool, id string, disabled bool) (managerView, error) {
	return scanManagerView(db.QueryRow(ctx,
		`UPDATE managers SET disabled = $2 WHERE id = $1::uuid
		 RETURNING `+managerViewColsRet, id, disabled))
}

// deleteManager hard-deletes a manager row. Sessions/overrides/allowlist rows
// cascade; subscriber ownership and voucher attribution SET NULL. A manager
// with ledger history cannot be deleted at all: the ledger's append-only
// trigger rejects the FK's SET NULL update (see migration 0413's comment) —
// callers map that error to a "disable instead" response.
func deleteManager(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `DELETE FROM managers WHERE id = $1::uuid`, id)
	return err
}

// otherActiveManagerAdminExists reports whether at least one manager other
// than `excludeID` is enabled and can administer managers (via role wildcard /
// managers.edit, a granted override, or the legacy admin role text). It backs
// the "never remove or disable the last admin" guard: the check is deliberately
// permissive in what counts as an admin — the guard must never leave zero.
func otherActiveManagerAdminExists(ctx context.Context, db *pgxpool.Pool, excludeID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(
		    SELECT 1 FROM managers m
		     WHERE m.id <> $1::uuid AND NOT m.disabled
		       AND ( m.role = 'admin'
		          OR EXISTS (SELECT 1 FROM role_permissions rp
		                      WHERE rp.role_id = m.role_id AND rp.permission IN ('*','managers.edit'))
		          OR EXISTS (SELECT 1 FROM manager_permission_overrides o
		                      WHERE o.manager_id = m.id AND o.granted AND o.permission IN ('*','managers.edit'))))`,
		excludeID).Scan(&exists)
	return exists, err
}

// roleExists reports whether a role with the given name exists.
func roleExists(ctx context.Context, db *pgxpool.Pool, name string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, name).Scan(&exists)
	return exists, err
}
