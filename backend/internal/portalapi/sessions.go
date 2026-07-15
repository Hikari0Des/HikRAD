package portalapi

// portal_sessions store (migration 0300). Mirrors internal/auth's
// panel_sessions rotation discipline (opaque refresh token, rotate-on-use,
// theft-detected-as-reuse) but is its own table so a portal session can never
// be mistaken for or revoke a manager one.

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createSession(ctx context.Context, db *pgxpool.Pool, subscriberID string, hash []byte, ip, ua string) (string, error) {
	var id string
	err := db.QueryRow(ctx,
		`INSERT INTO portal_sessions (subscriber_id, refresh_hash, ip, ua) VALUES ($1::uuid, $2, $3, $4) RETURNING id::text`,
		subscriberID, hash, ip, ua).Scan(&id)
	return id, err
}

// getSessionForRefresh returns the session's owner + stored hash, or
// pgx.ErrNoRows if the session id does not exist.
func getSessionForRefresh(ctx context.Context, db *pgxpool.Pool, sessionID string) (subscriberID string, hash []byte, revoked bool, err error) {
	err = db.QueryRow(ctx,
		`SELECT subscriber_id::text, refresh_hash, (revoked_at IS NOT NULL) FROM portal_sessions WHERE id = $1::uuid`,
		sessionID).Scan(&subscriberID, &hash, &revoked)
	if err == pgx.ErrNoRows {
		return "", nil, false, err
	}
	return subscriberID, hash, revoked, err
}

// rotateSession installs a new refresh hash, only when the session is still
// live — a concurrent revoke wins the race cleanly (rotated=false).
func rotateSession(ctx context.Context, db *pgxpool.Pool, sessionID string, newHash []byte, ip, ua string) (bool, error) {
	ct, err := db.Exec(ctx,
		`UPDATE portal_sessions SET refresh_hash = $2, ip = $3, ua = $4, rotated_at = now()
		  WHERE id = $1::uuid AND revoked_at IS NULL`,
		sessionID, newHash, ip, ua)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func revokeSession(ctx context.Context, db *pgxpool.Pool, sessionID string) error {
	_, err := db.Exec(ctx, `UPDATE portal_sessions SET revoked_at = now() WHERE id = $1::uuid AND revoked_at IS NULL`, sessionID)
	return err
}

// revokeAllSessions is the password-change token-theft mitigation (task
// edge case: "rotate on password change") — every other device/browser is
// signed out immediately; the caller re-issues a fresh session for itself.
func revokeAllSessions(ctx context.Context, db *pgxpool.Pool, subscriberID string) error {
	_, err := db.Exec(ctx,
		`UPDATE portal_sessions SET revoked_at = now() WHERE subscriber_id = $1::uuid AND revoked_at IS NULL`, subscriberID)
	return err
}
