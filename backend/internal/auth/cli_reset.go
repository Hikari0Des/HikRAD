package auth

// ResetPasswordByUsername backs `hikrad-api reset-manager-password` (v4-2,
// FR-119 — the `hikrad passwd` maintenance command SAMM's CLI has and HikRAD
// didn't). It exists because a locked-out operator has, by definition, no
// authenticated session to reach the ordinary admin reset endpoint
// (managers_api.go) through — this is the same direct-DB-access posture as
// `hikrad-api seed`/`print-tunnel-token`, invoked by root on the host, never
// exposed over HTTP.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrManagerNotFound = errors.New("auth: no manager with that username")

func ResetPasswordByUsername(ctx context.Context, db *pgxpool.Pool, username, newPassword string) error {
	m, err := lookupManagerByUsername(ctx, db, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrManagerNotFound
	}
	if err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	return updatePasswordHash(ctx, db, m.ID, hash)
}
