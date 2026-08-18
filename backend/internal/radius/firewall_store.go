package radius

// FR-146 firewall snapshot storage (migration 0612) — the auto-revert safety
// net's evidence. See 0612's migration comment for why revert_sentences is
// stored rather than recomputed at revert time.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type firewallSnapshotRow struct {
	ID                 string
	NASID              string
	ChangeDescription  string
	Snapshot           vendor.FirewallSnapshot
	RevertDeadline     time.Time
	Confirmed          bool
	Reverted           bool
	CreatedAt          time.Time
}

// insertFirewallSnapshot records the pre-change state BEFORE apply writes
// anything (the caller's ordering, not enforced here) and returns the new
// row's id, which the caller returns to the panel so it can show the revert
// countdown and let the operator confirm it.
func insertFirewallSnapshot(ctx context.Context, db *pgxpool.Pool, nasID, description string, snap vendor.FirewallSnapshot, deadline time.Time, createdBy string) (string, error) {
	snapJSON, err := json.Marshal(snap.Rules)
	if err != nil {
		return "", err
	}
	revertJSON, err := json.Marshal(snap.RevertSentences)
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRow(ctx,
		`INSERT INTO nas_firewall_snapshots (nas_id, change_description, snapshot, revert_sentences, revert_deadline, created_by)
		 VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6, '')::uuid)
		 RETURNING id::text`,
		nasID, description, snapJSON, revertJSON, deadline, createdBy).Scan(&id)
	return id, err
}

func getFirewallSnapshot(ctx context.Context, db *pgxpool.Pool, nasID, id string) (firewallSnapshotRow, error) {
	var row firewallSnapshotRow
	var snapJSON, revertJSON []byte
	err := db.QueryRow(ctx,
		`SELECT id::text, nas_id::text, change_description, snapshot, revert_sentences, revert_deadline, confirmed, reverted, created_at
		   FROM nas_firewall_snapshots WHERE id = $1::uuid AND nas_id = $2::uuid`, id, nasID).
		Scan(&row.ID, &row.NASID, &row.ChangeDescription, &snapJSON, &revertJSON, &row.RevertDeadline, &row.Confirmed, &row.Reverted, &row.CreatedAt)
	if err != nil {
		return row, err
	}
	_ = json.Unmarshal(snapJSON, &row.Snapshot.Rules)
	_ = json.Unmarshal(revertJSON, &row.Snapshot.RevertSentences)
	return row, nil
}

// confirmFirewallSnapshot marks a pending change as accepted by the operator
// — the auto-revert worker skips any row with confirmed=true, exactly the
// "operator confirmed within the window" outcome (gate item 11's alternate
// path to the same test).
func confirmFirewallSnapshot(ctx context.Context, db *pgxpool.Pool, nasID, id string) error {
	ct, err := db.Exec(ctx,
		`UPDATE nas_firewall_snapshots SET confirmed = true, confirmed_at = now()
		  WHERE id = $1::uuid AND nas_id = $2::uuid AND confirmed = false AND reverted = false`, id, nasID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// duePendingSnapshots is the auto-revert worker's whole query: every
// unconfirmed, not-yet-reverted snapshot whose deadline has passed. Backed by
// 0612's partial index — a NAS with normal firewall usage (confirmed or
// already-reverted rows) never enters this scan.
func duePendingSnapshots(ctx context.Context, db *pgxpool.Pool) ([]firewallSnapshotRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, nas_id::text, change_description, snapshot, revert_sentences, revert_deadline, confirmed, reverted, created_at
		   FROM nas_firewall_snapshots
		  WHERE confirmed = false AND reverted = false AND revert_deadline <= now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []firewallSnapshotRow
	for rows.Next() {
		var row firewallSnapshotRow
		var snapJSON, revertJSON []byte
		if err := rows.Scan(&row.ID, &row.NASID, &row.ChangeDescription, &snapJSON, &revertJSON,
			&row.RevertDeadline, &row.Confirmed, &row.Reverted, &row.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(snapJSON, &row.Snapshot.Rules)
		_ = json.Unmarshal(revertJSON, &row.Snapshot.RevertSentences)
		out = append(out, row)
	}
	return out, rows.Err()
}

// markSnapshotReverted records the outcome of an auto-revert attempt.
// errMsg empty means it succeeded.
func markSnapshotReverted(ctx context.Context, db *pgxpool.Pool, id, errMsg string) error {
	_, err := db.Exec(ctx,
		`UPDATE nas_firewall_snapshots SET reverted = true, reverted_at = now(), revert_error = NULLIF($2, '')
		  WHERE id = $1::uuid`, id, errMsg)
	return err
}

// listPendingSnapshotsForNAS is the FR-146 Firewall tab's "pending revert"
// banner source.
func listPendingSnapshotsForNAS(ctx context.Context, db *pgxpool.Pool, nasID string) ([]firewallSnapshotRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, nas_id::text, change_description, snapshot, revert_sentences, revert_deadline, confirmed, reverted, created_at
		   FROM nas_firewall_snapshots
		  WHERE nas_id = $1::uuid AND confirmed = false AND reverted = false
		  ORDER BY created_at DESC`, nasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []firewallSnapshotRow
	for rows.Next() {
		var row firewallSnapshotRow
		var snapJSON, revertJSON []byte
		if err := rows.Scan(&row.ID, &row.NASID, &row.ChangeDescription, &snapJSON, &revertJSON,
			&row.RevertDeadline, &row.Confirmed, &row.Reverted, &row.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(snapJSON, &row.Snapshot.Rules)
		_ = json.Unmarshal(revertJSON, &row.Snapshot.RevertSentences)
		out = append(out, row)
	}
	return out, rows.Err()
}
