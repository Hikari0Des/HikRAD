package live

// Subscriber-attribute resolution for filtering/scoping. The live hash carries
// subscriber_id but not profile_id / owner_manager_id, so those are resolved
// from the subscribers table in one batched query per feed read. owner_manager_id
// is D's column (migrations 0100–0109); until it lands the query degrades to
// profile_id only and every owner reads as "" (so scoped managers see nothing —
// the safe default — until D ships).

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// resolveSubjects batch-loads subject attributes for a set of subscriber ids.
// Unknown ids (empty or unmatched) simply have no map entry → subjectAttrs zero.
func resolveSubjects(ctx context.Context, db *pgxpool.Pool, ids []string) map[string]subjectAttrs {
	out := make(map[string]subjectAttrs, len(ids))
	if db == nil || len(ids) == 0 {
		return out
	}
	// Try the full shape first; fall back if D's owner_manager_id is not there.
	rows, err := db.Query(ctx,
		`SELECT id::text, COALESCE(profile_id::text,''), COALESCE(owner_manager_id::text,'')
		   FROM subscribers WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return resolveSubjectsNoOwner(ctx, db, ids)
	}
	defer rows.Close()
	for rows.Next() {
		var id, profileID, owner string
		if err := rows.Scan(&id, &profileID, &owner); err != nil {
			return out
		}
		out[id] = subjectAttrs{ProfileID: profileID, OwnerManagerID: owner}
	}
	return out
}

func resolveSubjectsNoOwner(ctx context.Context, db *pgxpool.Pool, ids []string) map[string]subjectAttrs {
	out := make(map[string]subjectAttrs, len(ids))
	rows, err := db.Query(ctx,
		`SELECT id::text, COALESCE(profile_id::text,'') FROM subscribers WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, profileID string
		if err := rows.Scan(&id, &profileID); err != nil {
			return out
		}
		out[id] = subjectAttrs{ProfileID: profileID}
	}
	return out
}
