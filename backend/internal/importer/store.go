package importer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// batch is the persisted state of one import run (migration 0400).
type batch struct {
	ID         string
	Filename   string
	Encoding   string
	Delimiter  string
	RawCSV     []byte // decoded UTF-8 text, stored as bytes
	Header     []string
	ColumnMap  map[string]string
	Preset     string
	Status     string
	RowCount   int
	DryRunAt   *time.Time
	ExecutedAt *time.Time
	Summary    map[string]any
	CreatedBy  string
	CreatedAt  time.Time
}

const batchColumns = `id::text, filename, encoding, delimiter, raw_csv, header, column_map,
	COALESCE(preset,''), status, row_count, dry_run_at, executed_at, summary,
	COALESCE(created_by::text,''), created_at`

func scanBatch(row pgx.Row) (batch, error) {
	var b batch
	var header, colmap, summary []byte
	err := row.Scan(&b.ID, &b.Filename, &b.Encoding, &b.Delimiter, &b.RawCSV, &header, &colmap,
		&b.Preset, &b.Status, &b.RowCount, &b.DryRunAt, &b.ExecutedAt, &summary,
		&b.CreatedBy, &b.CreatedAt)
	if err != nil {
		return batch{}, err
	}
	_ = json.Unmarshal(header, &b.Header)
	_ = json.Unmarshal(colmap, &b.ColumnMap)
	_ = json.Unmarshal(summary, &b.Summary)
	b.CreatedAt = b.CreatedAt.UTC()
	return b, nil
}

func insertBatch(ctx context.Context, db *pgxpool.Pool, filename, encoding string, delimiter rune,
	rawUTF8 []byte, header []string, createdBy string) (string, error) {
	headerJSON, _ := json.Marshal(header)
	var id string
	err := db.QueryRow(ctx,
		`INSERT INTO import_batches (filename, encoding, delimiter, raw_csv, header, created_by)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid) RETURNING id::text`,
		filename, encoding, string(delimiter), rawUTF8, headerJSON, createdBy).Scan(&id)
	return id, err
}

func getBatch(ctx context.Context, db *pgxpool.Pool, id string) (batch, error) {
	return scanBatch(db.QueryRow(ctx, `SELECT `+batchColumns+` FROM import_batches WHERE id = $1::uuid`, id))
}

func updateColumnMap(ctx context.Context, db *pgxpool.Pool, id string, colMap map[string]string, preset string) error {
	b, _ := json.Marshal(colMap)
	_, err := db.Exec(ctx,
		`UPDATE import_batches SET column_map = $2, preset = NULLIF($3,''), status = 'mapped' WHERE id = $1::uuid`,
		id, b, preset)
	return err
}

func setBatchStatus(ctx context.Context, db *pgxpool.Pool, id, status string) error {
	_, err := db.Exec(ctx, `UPDATE import_batches SET status = $2 WHERE id = $1::uuid`, id, status)
	return err
}

func markDryRun(ctx context.Context, db *pgxpool.Pool, id string, rowCount int) error {
	_, err := db.Exec(ctx,
		`UPDATE import_batches SET status = 'dry_run', row_count = $2, dry_run_at = now() WHERE id = $1::uuid`,
		id, rowCount)
	return err
}

func markExecuted(ctx context.Context, db *pgxpool.Pool, id string, summary map[string]any) error {
	b, _ := json.Marshal(summary)
	_, err := db.Exec(ctx,
		`UPDATE import_batches SET status = 'completed', executed_at = now(), summary = $2 WHERE id = $1::uuid`,
		id, b)
	return err
}

// --- rows --------------------------------------------------------------

type importRow struct {
	ID           string
	RowNumber    int
	Fields       map[string]string
	Errors       []string
	Warnings     []string
	Action       string // create | skip
	Status       string // pending | imported | skipped | failed
	SubscriberID string
	Error        string
}

// replaceRows deletes any prior dry-run rows for the batch and inserts the
// fresh set (dry-run is always re-derived from the current column_map, never
// merged with a stale run).
func replaceRows(ctx context.Context, db *pgxpool.Pool, batchID string, rows []importRow) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM import_rows WHERE batch_id = $1::uuid`, batchID); err != nil {
		return err
	}
	for _, r := range rows {
		fields, _ := json.Marshal(r.Fields)
		errs, _ := json.Marshal(r.Errors)
		warns, _ := json.Marshal(r.Warnings)
		if _, err := tx.Exec(ctx,
			`INSERT INTO import_rows (batch_id, row_number, fields, errors, warnings, action, status)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, 'pending')`,
			batchID, r.RowNumber, fields, errs, warns, r.Action); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func listRows(ctx context.Context, db *pgxpool.Pool, batchID string) ([]importRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, row_number, fields, errors, warnings, action, status,
		        COALESCE(subscriber_id::text,''), COALESCE(error,'')
		   FROM import_rows WHERE batch_id = $1::uuid ORDER BY row_number`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []importRow
	for rows.Next() {
		var r importRow
		var fields, errs, warns []byte
		if err := rows.Scan(&r.ID, &r.RowNumber, &fields, &errs, &warns, &r.Action, &r.Status,
			&r.SubscriberID, &r.Error); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fields, &r.Fields)
		_ = json.Unmarshal(errs, &r.Errors)
		_ = json.Unmarshal(warns, &r.Warnings)
		out = append(out, r)
	}
	return out, rows.Err()
}

func markRowResult(ctx context.Context, db *pgxpool.Pool, rowID, status, subscriberID, errMsg string) error {
	_, err := db.Exec(ctx,
		`UPDATE import_rows SET status = $2, subscriber_id = NULLIF($3,'')::uuid, error = NULLIF($4,'')
		  WHERE id = $1::uuid`, rowID, status, subscriberID, errMsg)
	return err
}
