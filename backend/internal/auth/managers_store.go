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
}

// managerView is the API read shape (no secret material).
type managerView struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Scoped    bool      `json:"scoped"`
	CreatedAt time.Time `json:"created_at"`
}

func lookupManagerByUsername(ctx context.Context, db *pgxpool.Pool, username string) (*managerAuthRow, error) {
	var m managerAuthRow
	err := db.QueryRow(ctx,
		`SELECT id::text, username::text, password_hash, role, scoped
		   FROM managers WHERE username = $1`, username).
		Scan(&m.ID, &m.Username, &m.PasswordHash, &m.Role, &m.Scoped)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func lookupManagerByID(ctx context.Context, db *pgxpool.Pool, id string) (*managerAuthRow, error) {
	var m managerAuthRow
	err := db.QueryRow(ctx,
		`SELECT id::text, username::text, password_hash, role, scoped
		   FROM managers WHERE id = $1::uuid`, id).
		Scan(&m.ID, &m.Username, &m.PasswordHash, &m.Role, &m.Scoped)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// updatePasswordHash re-points a manager's stored hash (used by the argon2id
// upgrade path on successful login, and by password reset).
func updatePasswordHash(ctx context.Context, db *pgxpool.Pool, id, hash string) error {
	_, err := db.Exec(ctx, `UPDATE managers SET password_hash = $2 WHERE id = $1::uuid`, id, hash)
	return err
}

func insertManager(ctx context.Context, db *pgxpool.Pool, username, hash, role string, scoped bool) (managerView, error) {
	var v managerView
	err := db.QueryRow(ctx,
		`INSERT INTO managers (username, password_hash, role, scoped)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id::text, username::text, role, scoped, created_at`,
		username, hash, role, scoped).
		Scan(&v.ID, &v.Username, &v.Role, &v.Scoped, &v.CreatedAt)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, err
}

func getManagerView(ctx context.Context, db *pgxpool.Pool, id string) (managerView, error) {
	var v managerView
	err := db.QueryRow(ctx,
		`SELECT id::text, username::text, role, scoped, created_at
		   FROM managers WHERE id = $1::uuid`, id).
		Scan(&v.ID, &v.Username, &v.Role, &v.Scoped, &v.CreatedAt)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, err
}

func listManagerViews(ctx context.Context, db *pgxpool.Pool) ([]managerView, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, username::text, role, scoped, created_at
		   FROM managers ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []managerView{}
	for rows.Next() {
		var v managerView
		if err := rows.Scan(&v.ID, &v.Username, &v.Role, &v.Scoped, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.CreatedAt = v.CreatedAt.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// updateManagerRoleScope updates role and/or scoped, returning the new view.
func updateManagerRoleScope(ctx context.Context, db *pgxpool.Pool, id, role string, scoped bool) (managerView, error) {
	var v managerView
	err := db.QueryRow(ctx,
		`UPDATE managers SET role = $2, scoped = $3 WHERE id = $1::uuid
		 RETURNING id::text, username::text, role, scoped, created_at`,
		id, role, scoped).
		Scan(&v.ID, &v.Username, &v.Role, &v.Scoped, &v.CreatedAt)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, err
}
