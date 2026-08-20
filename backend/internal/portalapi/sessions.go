package portalapi

// portal_sessions store (migration 0300). Mirrors internal/auth's
// panel_sessions rotation discipline (opaque refresh token, rotate-on-use,
// theft-detected-as-reuse) but is its own table so a portal session can never
// be mistaken for or revoke a manager one.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createSession inserts a new portal session. scope/hardCap are v4-11
// additions (migration 0631: portal_sessions.scope/expires_at): a normal
// password login passes (scopeFull, nil) — unbounded lifetime, exactly the
// pre-v4-11 behavior; auto-login (autologin.go) passes (scopeReduced,
// &deadline) — the session cannot be refreshed past hardCap no matter how
// many times it rotates (FR-167 gate item 11).
func createSession(ctx context.Context, db *pgxpool.Pool, subscriberID string, hash []byte, ip, ua, scope string, hardCap *time.Time) (string, error) {
	var id string
	err := db.QueryRow(ctx,
		`INSERT INTO portal_sessions (subscriber_id, refresh_hash, ip, ua, scope, expires_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6) RETURNING id::text`,
		subscriberID, hash, ip, ua, scope, hardCap).Scan(&id)
	return id, err
}

// getSessionForRefresh returns the session's owner + stored hash + its
// current scope + hard cap (nil = unbounded), or pgx.ErrNoRows if the
// session id does not exist.
func getSessionForRefresh(ctx context.Context, db *pgxpool.Pool, sessionID string) (subscriberID string, hash []byte, revoked bool, scope string, hardCap *time.Time, err error) {
	err = db.QueryRow(ctx,
		`SELECT subscriber_id::text, refresh_hash, (revoked_at IS NOT NULL), scope, expires_at
		   FROM portal_sessions WHERE id = $1::uuid`,
		sessionID).Scan(&subscriberID, &hash, &revoked, &scope, &hardCap)
	if err == pgx.ErrNoRows {
		return "", nil, false, "", nil, err
	}
	return subscriberID, hash, revoked, scope, hardCap, err
}

// sessionScope returns a live session's current scope (used by step-up to
// confirm which row it is upgrading) and hard cap.
func sessionScope(ctx context.Context, db *pgxpool.Pool, sessionID string) (scope string, hardCap *time.Time, revoked bool, err error) {
	err = db.QueryRow(ctx,
		`SELECT scope, expires_at, (revoked_at IS NOT NULL) FROM portal_sessions WHERE id = $1::uuid`,
		sessionID).Scan(&scope, &hardCap, &revoked)
	return scope, hardCap, revoked, err
}

// upgradeSessionScope promotes a session to full scope in place (FR-167's
// password step-up) — same row, same hard cap, so "upgrades to full for its
// remaining lifetime" is exact: nothing here touches expires_at.
func upgradeSessionScope(ctx context.Context, db *pgxpool.Pool, sessionID string) error {
	_, err := db.Exec(ctx,
		`UPDATE portal_sessions SET scope = 'full' WHERE id = $1::uuid AND revoked_at IS NULL`, sessionID)
	return err
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
