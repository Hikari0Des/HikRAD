package cards

// Group creation + code generation, and the group actions FR-136 names:
// enable/disable all, extend valid_until, add-more cards, duplicate group
// (fresh codes), and per-card counter reset.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// groupInput is the create/update request body.
type groupInput struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	UsernamePrefix     string  `json:"username_prefix"`
	CodeFormat         string  `json:"code_format"`
	CodeLength         int     `json:"code_length"`
	Count              int     `json:"count"` // create-only: how many cards to generate
	Price              int64   `json:"price"`
	RateDownKbps       int     `json:"rate_down_kbps"`
	RateUpKbps         int     `json:"rate_up_kbps"`
	TransferMode       string  `json:"transfer_mode"`
	TransferTotalBytes *int64  `json:"transfer_total_bytes"`
	TransferDownBytes  *int64  `json:"transfer_down_bytes"`
	TransferUpBytes    *int64  `json:"transfer_up_bytes"`
	UptimeLimitSeconds *int64  `json:"uptime_limit_seconds"`
	ExpireAfterSeconds *int64  `json:"expire_after_seconds"`
	ValidUntil         *string `json:"valid_until"` // RFC3339
}

type fieldError struct{ Field, Message string }

func (in *groupInput) validate() []fieldError {
	var fe []fieldError
	add := func(f, m string) { fe = append(fe, fieldError{f, m}) }
	if in.Name == "" {
		add("name", "this field is required")
	}
	switch in.CodeFormat {
	case "":
		in.CodeFormat = "alphanumeric"
	case "numeric", "alpha", "alphanumeric":
	default:
		add("code_format", "must be one of: numeric alpha alphanumeric")
	}
	if in.CodeLength == 0 {
		in.CodeLength = 10
	}
	if in.CodeLength < 6 || in.CodeLength > 24 {
		add("code_length", "must be between 6 and 24")
	}
	if in.Price < 0 {
		add("price", "must not be negative")
	}
	switch in.TransferMode {
	case "":
		in.TransferMode = "unlimited"
	case "unlimited":
		in.TransferTotalBytes, in.TransferDownBytes, in.TransferUpBytes = nil, nil, nil
	case "combined":
		if in.TransferTotalBytes == nil || *in.TransferTotalBytes <= 0 {
			add("transfer_total_bytes", "required and positive when transfer_mode is 'combined'")
		}
		in.TransferDownBytes, in.TransferUpBytes = nil, nil
	case "separate":
		if (in.TransferDownBytes == nil || *in.TransferDownBytes <= 0) &&
			(in.TransferUpBytes == nil || *in.TransferUpBytes <= 0) {
			add("transfer_down_bytes", "at least one of down/up must be positive when transfer_mode is 'separate'")
		}
		in.TransferTotalBytes = nil
	default:
		add("transfer_mode", "must be one of: unlimited combined separate")
	}
	return fe
}

// createGroup creates a group and generates its initial `want` cards in one
// transaction (a group with zero cards is a config mistake, never a
// deliberate state).
func createGroup(ctx context.Context, db *pgxpool.Pool, in groupInput, creatorID string, validUntil *time.Time) (Group, []string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return Group{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO card_groups
		    (name, description, username_prefix, code_format, code_length, price,
		     rate_down_kbps, rate_up_kbps, transfer_mode, transfer_total_bytes, transfer_down_bytes,
		     transfer_up_bytes, uptime_limit_seconds, expire_after_seconds, valid_until,
		     enabled, source, creator_manager_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,true,'native',NULLIF($16,'')::uuid)
		RETURNING id::text`,
		in.Name, in.Description, in.UsernamePrefix, in.CodeFormat, in.CodeLength, in.Price,
		in.RateDownKbps, in.RateUpKbps, in.TransferMode, in.TransferTotalBytes, in.TransferDownBytes,
		in.TransferUpBytes, in.UptimeLimitSeconds, in.ExpireAfterSeconds, validUntil,
		creatorID).Scan(&id)
	if err != nil {
		return Group{}, nil, err
	}

	want := in.Count
	if want <= 0 {
		want = 1
	}
	plain, err := insertCards(ctx, tx, id, in.UsernamePrefix, in.CodeFormat, want, in.CodeLength)
	if err != nil {
		return Group{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, nil, err
	}
	g, err := getGroup(ctx, db, id)
	return g, plain, err
}

// insertCards generates `want` unique codes for group and inserts them,
// returning the plaintext codes — the only time they ever exist, exactly
// mirroring billing/voucher.go's insertCodes.
func insertCards(ctx context.Context, tx pgx.Tx, groupID, prefix, codeFormat string, want, codeLength int) ([]string, error) {
	plain := make([]string, 0, want)
	for attempt := 0; len(plain) < want && attempt < 5; attempt++ {
		need := want - len(plain)
		codes := make([]string, 0, need)
		hashes := make([]string, 0, need)
		seen := make(map[string]bool, need)
		for len(codes) < need {
			c := genCode(prefix, codeFormat, codeLength)
			h := hashCode(c)
			if seen[h] {
				continue
			}
			seen[h] = true
			codes = append(codes, c)
			hashes = append(hashes, h)
		}
		rows, err := tx.Query(ctx,
			`INSERT INTO cards (group_id, code_hash)
			 SELECT $1::uuid, h FROM unnest($2::text[]) AS h
			 ON CONFLICT (code_hash) DO NOTHING
			 RETURNING code_hash`, groupID, hashes)
		if err != nil {
			return nil, err
		}
		inserted := map[string]bool{}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return nil, err
			}
			inserted[h] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i, h := range hashes {
			if inserted[h] {
				plain = append(plain, codes[i])
			}
		}
	}
	if len(plain) < want {
		return nil, errors.New("cards: could not generate enough unique codes")
	}
	return plain, nil
}

// addMoreCards generates `want` additional codes for an existing group,
// reusing its own prefix/format/length settings.
func addMoreCards(ctx context.Context, db *pgxpool.Pool, groupID string, want int) ([]string, error) {
	g, err := getGroup(ctx, db, groupID)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plain, err := insertCards(ctx, tx, groupID, g.UsernamePrefix, g.CodeFormat, want, g.CodeLength)
	if err != nil {
		return nil, err
	}
	return plain, tx.Commit(ctx)
}

// duplicateGroup copies a group's settings into a new "<name> (copy)" group
// with `count` FRESH codes — "the one-click reorder operators actually use."
// Group settings (limits, rate, price) are copied; existing cards are not.
func duplicateGroup(ctx context.Context, db *pgxpool.Pool, sourceID string, count int) (Group, []string, error) {
	src, err := getGroup(ctx, db, sourceID)
	if err != nil {
		return Group{}, nil, err
	}
	in := groupInput{
		Name: src.Name + " (copy)", Description: src.Description, UsernamePrefix: src.UsernamePrefix,
		CodeFormat: src.CodeFormat, CodeLength: src.CodeLength, Count: count, Price: src.Price,
		RateDownKbps: src.RateDownKbps, RateUpKbps: src.RateUpKbps, TransferMode: src.TransferMode,
		TransferTotalBytes: src.TransferTotalBytes, TransferDownBytes: src.TransferDownBytes,
		TransferUpBytes: src.TransferUpBytes, UptimeLimitSeconds: src.UptimeLimitSeconds,
		ExpireAfterSeconds: src.ExpireAfterSeconds,
	}
	var creator string
	if src.CreatorManagerID != nil {
		creator = *src.CreatorManagerID
	}
	return createGroup(ctx, db, in, creator, src.ValidUntil)
}

// setGroupEnabled implements "enable/disable all" — a group-level switch a
// disabled card can never authenticate through, checked by the auth resolver
// regardless of the individual card's own state.
func setGroupEnabled(ctx context.Context, db *pgxpool.Pool, id string, enabled bool) error {
	ct, err := db.Exec(ctx, `UPDATE card_groups SET enabled = $2 WHERE id = $1::uuid`, id, enabled)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errGroupNotFound
	}
	return nil
}

// extendValidUntil sets a group's shelf date to the given absolute instant
// (the caller — the panel's "extend" dialog — always sends the new date, so
// this is a plain set, not a delta).
func extendValidUntil(ctx context.Context, db *pgxpool.Pool, id string, validUntil time.Time) error {
	ct, err := db.Exec(ctx, `UPDATE card_groups SET valid_until = $2 WHERE id = $1::uuid`, id, validUntil)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errGroupNotFound
	}
	return nil
}

// resetCard moves one card's usage_cycle_anchor forward to now() — "reset an
// individual card's counters" — and, if it had exhausted, restores it to
// active (a fresh cycle, not a fresh card: first_login_at and the group's
// shelf date are untouched, so expire_after_seconds keeps counting from the
// ORIGINAL first login).
func resetCard(ctx context.Context, db *pgxpool.Pool, id string) error {
	ct, err := db.Exec(ctx, `
		UPDATE cards SET usage_cycle_anchor = now(),
		       state = CASE WHEN state = 'exhausted' THEN 'active' ELSE state END
		 WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errCardNotFound
	}
	return nil
}

// setCardState is the per-card enable/disable action (distinct from the
// group-wide enable/disable-all).
func setCardState(ctx context.Context, db *pgxpool.Pool, id, state string) error {
	ct, err := db.Exec(ctx, `UPDATE cards SET state = $2 WHERE id = $1::uuid`, id, state)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errCardNotFound
	}
	return nil
}
