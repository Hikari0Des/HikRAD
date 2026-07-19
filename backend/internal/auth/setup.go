package auth

// Exported seam for the first-run wizard (FR-49.3, contract C4): setupapi
// (internal/platform/setupapi) needs to create the very first manager
// account before any token exists to authorize a normal POST
// /api/v1/managers call. It lives here rather than in setupapi because
// insertManager/hashPassword/roleExists are unexported package internals —
// this is the one function that crosses that boundary, and it re-checks
// "no manager exists yet" itself so it is safe even if a caller's own check
// raced.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSetupAlreadyComplete is returned when a manager already exists.
var ErrSetupAlreadyComplete = errors.New("auth: setup already complete")

// ManagerCount returns how many manager accounts exist — the wizard-gating
// signal ("setup endpoints active only while no admin exists").
func ManagerCount(ctx context.Context, db *pgxpool.Pool) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM managers`).Scan(&n)
	return n, err
}

// CreateFirstAdmin creates the sole initial manager with the built-in admin
// role. It refuses (ErrSetupAlreadyComplete) if any manager already exists,
// so it can never be used to add a second admin outside the wizard.
func CreateFirstAdmin(ctx context.Context, db *pgxpool.Pool, username, password string) (managerView, error) {
	n, err := ManagerCount(ctx, db)
	if err != nil {
		return managerView{}, err
	}
	if n > 0 {
		return managerView{}, ErrSetupAlreadyComplete
	}
	hash, err := hashPassword(password)
	if err != nil {
		return managerView{}, err
	}
	return insertManager(ctx, db, username, hash, RoleAdmin, false, managerProfile{})
}
