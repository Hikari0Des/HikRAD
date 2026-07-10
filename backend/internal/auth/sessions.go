package auth

// panel_sessions data access (FR-29). One row per login; the refresh secret
// is stored only as a sha256 hash and rotated on every refresh.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sessionRow struct {
	ID         string    `json:"id"`
	ManagerID  string    `json:"manager_id"`
	UA         string    `json:"ua"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Revoked    bool      `json:"revoked"`
}

// createSession inserts a new session and returns its id.
func createSession(ctx context.Context, db *pgxpool.Pool, managerID string, refreshHash []byte, ua, ip string) (string, error) {
	var id string
	err := db.QueryRow(ctx,
		`INSERT INTO panel_sessions (manager_id, refresh_hash, ua, ip)
		 VALUES ($1::uuid, $2, $3, $4)
		 RETURNING id::text`,
		managerID, refreshHash, ua, ip).Scan(&id)
	return id, err
}

// getSessionForRefresh loads the fields refresh needs, or pgx.ErrNoRows.
func getSessionForRefresh(ctx context.Context, db *pgxpool.Pool, sessionID string) (managerID string, refreshHash []byte, revoked bool, err error) {
	err = db.QueryRow(ctx,
		`SELECT manager_id::text, refresh_hash, revoked
		   FROM panel_sessions WHERE id = $1::uuid`,
		sessionID).Scan(&managerID, &refreshHash, &revoked)
	return managerID, refreshHash, revoked, err
}

// rotateSession swaps in a new refresh hash and bumps last_seen, but only for
// a non-revoked session. Returns whether a row was updated.
func rotateSession(ctx context.Context, db *pgxpool.Pool, sessionID string, newHash []byte, ip, ua string) (bool, error) {
	tag, err := db.Exec(ctx,
		`UPDATE panel_sessions
		    SET refresh_hash = $2, last_seen_at = now(), ip = $3, ua = $4
		  WHERE id = $1::uuid AND revoked = false`,
		sessionID, newHash, ip, ua)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// revokeSession marks one session revoked. ownerID, when non-empty, scopes the
// revoke to that manager (a non-admin may only revoke their own). Returns
// whether a matching row was affected.
func revokeSession(ctx context.Context, db *pgxpool.Pool, sessionID, ownerID string) (bool, error) {
	var tag pgconn.CommandTag
	var err error
	if ownerID == "" {
		tag, err = db.Exec(ctx,
			`UPDATE panel_sessions SET revoked = true WHERE id = $1::uuid`, sessionID)
	} else {
		tag, err = db.Exec(ctx,
			`UPDATE panel_sessions SET revoked = true
			  WHERE id = $1::uuid AND manager_id = $2::uuid`, sessionID, ownerID)
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// revokeOtherSessions revokes every live session of a manager except keepID
// (used on password change / self-service). Pass keepID="" to revoke all.
func revokeOtherSessions(ctx context.Context, db *pgxpool.Pool, managerID, keepID string) error {
	if keepID == "" {
		_, err := db.Exec(ctx,
			`UPDATE panel_sessions SET revoked = true
			  WHERE manager_id = $1::uuid AND revoked = false`, managerID)
		return err
	}
	_, err := db.Exec(ctx,
		`UPDATE panel_sessions SET revoked = true
		  WHERE manager_id = $1::uuid AND revoked = false AND id <> $2::uuid`,
		managerID, keepID)
	return err
}

// listSessions returns a manager's sessions newest first.
func listSessions(ctx context.Context, db *pgxpool.Pool, managerID string) ([]sessionRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, manager_id::text, ua, ip, created_at, last_seen_at, revoked
		   FROM panel_sessions
		  WHERE manager_id = $1::uuid
		  ORDER BY created_at DESC`,
		managerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sessionRow{}
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.ID, &s.ManagerID, &s.UA, &s.IP, &s.CreatedAt, &s.LastSeenAt, &s.Revoked); err != nil {
			return nil, err
		}
		s.CreatedAt = s.CreatedAt.UTC()
		s.LastSeenAt = s.LastSeenAt.UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// sessionOwner returns the manager_id owning a session, or pgx.ErrNoRows.
func sessionOwner(ctx context.Context, db *pgxpool.Pool, sessionID string) (string, error) {
	var owner string
	err := db.QueryRow(ctx,
		`SELECT manager_id::text FROM panel_sessions WHERE id = $1::uuid`, sessionID).Scan(&owner)
	return owner, err
}
