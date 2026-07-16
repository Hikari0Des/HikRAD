package radius

// FR-64 scope persistence (contract C4, amended 2026-07-16 to a set).
//
// A subscriber or a profile may be scoped to MANY NAS/service pairs. The two
// owners are structurally identical — same columns, same rules, same failure
// modes — so the read/write/validate logic lives here once rather than being
// copy-pasted into the subscribers and profiles packages, which both already
// depend on this one (for InvalidatePolicy) and neither of which owns the nas /
// nas_services tables the scopes point at.
//
// The scope tables are written as a WHOLE SET: a write deletes the owner's rows
// and re-inserts. There is no partial-scope edit, because "add one NAS" and
// "replace the set" are indistinguishable at the API and the set is small.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScopeOwner names one of the two scope tables and its owning key. Callers use
// the two package vars below rather than constructing this — the strings are
// interpolated into SQL, so they must never come from a request.
type ScopeOwner struct {
	table string
	col   string
}

var (
	// SubscriberScopes scopes one account (FR-64, subscriber-over-profile).
	SubscriberScopes = ScopeOwner{table: "subscriber_nas_scopes", col: "subscriber_id"}
	// ProfileScopes scopes every subscriber on a profile that has no scope of
	// its own.
	ProfileScopes = ScopeOwner{table: "profile_nas_scopes", col: "profile_id"}
)

// LoadScopes reads one owner's scope set. An empty result means any NAS.
func LoadScopes(ctx context.Context, db *pgxpool.Pool, owner ScopeOwner, id string) ([]NASScope, error) {
	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT nas_id::text, COALESCE(nas_service_id::text, '')
		   FROM %s WHERE %s = $1::uuid
		  ORDER BY nas_id, nas_service_id NULLS FIRST`, owner.table, owner.col), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectScopes(rows)
}

// LoadScopesFor reads the scope sets of many owners at once, keyed by owner id.
// The list endpoints need every row's scopes; doing that one query per row is
// the N+1 that would show up as a slow subscriber list on a real ISP's data.
// Owners with no scopes are simply absent from the map (nil slice = any NAS).
func LoadScopesFor(ctx context.Context, db *pgxpool.Pool, owner ScopeOwner, ids []string) (map[string][]NASScope, error) {
	out := map[string][]NASScope{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT %s::text, nas_id::text, COALESCE(nas_service_id::text, '')
		   FROM %s WHERE %s = ANY($1::uuid[])
		  ORDER BY nas_id, nas_service_id NULLS FIRST`, owner.col, owner.table, owner.col), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID string
		var s NASScope
		if err := rows.Scan(&ownerID, &s.NASID, &s.ServiceID); err != nil {
			return nil, err
		}
		out[ownerID] = append(out[ownerID], s)
	}
	return out, rows.Err()
}

func collectScopes(rows pgx.Rows) ([]NASScope, error) {
	var out []NASScope
	for rows.Next() {
		var s NASScope
		if err := rows.Scan(&s.NASID, &s.ServiceID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReplaceScopes sets an owner's scope set to exactly scopes, inside the caller's
// transaction so the scope change commits or rolls back with the row it belongs
// to. Passing an empty set clears the scope back to "any NAS".
//
// tx must be a transaction: a scope set is deleted then re-inserted, and an
// account left with the delete applied but not the insert would authenticate
// ANYWHERE — a silent widening, the exact failure this whole feature exists to
// prevent.
func ReplaceScopes(ctx context.Context, tx pgx.Tx, owner ScopeOwner, id string, scopes []NASScope) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = $1::uuid`, owner.table, owner.col), id); err != nil {
		return err
	}
	for _, s := range DedupeScopes(scopes) {
		var svc *string
		if s.ServiceID != "" {
			svc = &s.ServiceID
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (%s, nas_id, nas_service_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`,
			owner.table, owner.col), id, s.NASID, svc); err != nil {
			return err
		}
	}
	return nil
}

// ValidateScopes checks a requested scope set against the NAS registry and
// returns a per-entry message for each problem, keyed by the entry's index (the
// caller renders "nas_scopes.<i>" field errors).
//
// A wrong scope is invisible at write time and total at auth time: an
// unsatisfiable entry rejects every login the operator meant to permit, and they
// would debug it as "RADIUS is broken", not "my form was wrong". Both failures
// below are therefore caught here rather than at 2am.
func ValidateScopes(ctx context.Context, db *pgxpool.Pool, scopes []NASScope) (map[int]string, error) {
	bad := map[int]string{}
	for i, s := range scopes {
		if s.NASID == "" {
			// A service with no NAS: the pair is meaningless, since a service
			// instance only exists on a NAS.
			bad[i] = "a NAS is required"
			continue
		}
		if s.ServiceID == "" {
			var exists bool
			if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nas WHERE id = $1::uuid)`, s.NASID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				bad[i] = "unknown NAS"
			}
			continue
		}
		var parent string
		err := db.QueryRow(ctx, `SELECT nas_id::text FROM nas_services WHERE id = $1::uuid`, s.ServiceID).Scan(&parent)
		if err == pgx.ErrNoRows {
			bad[i] = "unknown NAS service"
			continue
		}
		if err != nil {
			return nil, err
		}
		// A service belonging to a different NAS can never match: no request
		// satisfies both halves, so the entry silently denies instead of allows.
		if parent != s.NASID {
			bad[i] = "service does not belong to the selected NAS"
		}
	}
	return bad, nil
}

// DedupeScopes removes duplicates and collapses redundancy: a whole-NAS scope
// absorbs every per-service scope on that NAS, because the NAS-wide row already
// allows them and keeping both would show the operator a contradictory list
// ("this NAS" AND "only the Lobby zone on this NAS").
func DedupeScopes(in []NASScope) []NASScope {
	wholeNAS := map[string]bool{}
	for _, s := range in {
		if s.ServiceID == "" {
			wholeNAS[s.NASID] = true
		}
	}
	seen := map[NASScope]bool{}
	out := make([]NASScope, 0, len(in))
	for _, s := range in {
		if s.ServiceID != "" && wholeNAS[s.NASID] {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
