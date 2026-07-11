package auth

// Per-manager IP allowlist (FR-30). An empty list means unrestricted. The
// effective list is resolved at login/refresh and embedded in the access token
// (tokens.go), so enforcement at login and on every request (middleware.go)
// needs no per-request DB hit; a change propagates within one access-token
// lifetime.
//
// XFF / proxy handling: the client IP comes from clientIP() (middleware.go),
// which trusts the first X-Forwarded-For hop set by Caddy — the only ingress in
// the deployment (NFR-4.4). Nothing downstream of Caddy is trusted.

import (
	"context"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
)

// allowlistEntry is one CIDR rule for a manager.
type allowlistEntry struct {
	CIDR string `json:"cidr"`
	Note string `json:"note"`
}

// ipAllowed reports whether ip falls inside any of the allowlist CIDRs. An
// empty allowlist is unrestricted (returns true). An unparseable client ip or
// malformed CIDR fails closed for that entry (no match), but an empty list
// still means allow.
func ipAllowed(ip string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, c := range allow {
		if _, network, err := net.ParseCIDR(c); err == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}

// listAllowlist returns a manager's allowlist CIDRs (normalized) newest-first.
func listAllowlist(ctx context.Context, db *pgxpool.Pool, managerID string) ([]allowlistEntry, error) {
	rows, err := db.Query(ctx,
		`SELECT cidr::text, note FROM manager_ip_allowlist
		  WHERE manager_id = $1::uuid ORDER BY created_at DESC, cidr`, managerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []allowlistEntry{}
	for rows.Next() {
		var e allowlistEntry
		if err := rows.Scan(&e.CIDR, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// allowlistCIDRs returns just the CIDR strings for a manager (for the token).
func allowlistCIDRs(ctx context.Context, db *pgxpool.Pool, managerID string) ([]string, error) {
	entries, err := listAllowlist(ctx, db, managerID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.CIDR)
	}
	return out, nil
}

// replaceAllowlist atomically swaps a manager's allowlist for the given entries
// (the API sends the full desired list). Invalid CIDRs are rejected by the
// caller before this runs; the cidr column also validates at the DB level.
func replaceAllowlist(ctx context.Context, db *pgxpool.Pool, managerID string, entries []allowlistEntry) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `DELETE FROM manager_ip_allowlist WHERE manager_id = $1::uuid`, managerID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO manager_ip_allowlist (manager_id, cidr, note) VALUES ($1::uuid, $2::cidr, $3)
			 ON CONFLICT (manager_id, cidr) DO UPDATE SET note = EXCLUDED.note`,
			managerID, e.CIDR, e.Note); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// validCIDR reports whether s parses as a CIDR (host bits may be set; the DB
// cidr type is lenient via the ::cidr cast which masks host bits).
func validCIDR(s string) bool {
	_, _, err := net.ParseCIDR(s)
	return err == nil
}
