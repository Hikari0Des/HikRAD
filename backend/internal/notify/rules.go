package notify

// Per-event rules (FR-162, migration 0626): enable/disable, channels,
// timing (days_before), the ignore_opt_out flag, and the throttling knobs
// (batch_size/tick_interval_seconds — surfaced on the Rules & timers tab,
// gate item 7). One row per event_key, seeded by the migration.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rule is one notification_rules row.
type Rule struct {
	ID                  string
	EventKey            string
	Name                string
	Enabled             bool
	Channels            []string
	DaysBefore          *int
	IgnoreOptOut        bool
	BatchSize           int
	TickIntervalSeconds int
}

func scanRule(row pgx.Row) (Rule, error) {
	var r Rule
	var channelsRaw []byte
	err := row.Scan(&r.ID, &r.EventKey, &r.Name, &r.Enabled, &channelsRaw, &r.DaysBefore, &r.IgnoreOptOut, &r.BatchSize, &r.TickIntervalSeconds)
	if err != nil {
		return Rule{}, err
	}
	_ = json.Unmarshal(channelsRaw, &r.Channels)
	return r, nil
}

const ruleColumns = `id::text, event_key, name, enabled, channels, days_before, ignore_opt_out, batch_size, tick_interval_seconds`

func getRule(ctx context.Context, db *pgxpool.Pool, eventKey string) (Rule, bool, error) {
	r, err := scanRule(db.QueryRow(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE event_key = $1`, eventKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, false, nil
	}
	return r, err == nil, err
}

func listRules(ctx context.Context, db *pgxpool.Pool) ([]Rule, error) {
	rows, err := db.Query(ctx, `SELECT `+ruleColumns+` FROM notification_rules ORDER BY event_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RuleUpdate is the Rules & timers tab's PUT body.
type RuleUpdate struct {
	Enabled             *bool    `json:"enabled"`
	Channels            []string `json:"channels"`
	DaysBefore          *int     `json:"days_before"`
	IgnoreOptOut        *bool    `json:"ignore_opt_out"`
	BatchSize           *int     `json:"batch_size"`
	TickIntervalSeconds *int     `json:"tick_interval_seconds"`
}

func updateRule(ctx context.Context, db *pgxpool.Pool, eventKey string, u RuleUpdate) (Rule, bool, error) {
	current, ok, err := getRule(ctx, db, eventKey)
	if err != nil || !ok {
		return Rule{}, ok, err
	}
	if u.Enabled != nil {
		current.Enabled = *u.Enabled
	}
	if u.Channels != nil {
		current.Channels = u.Channels
	}
	if u.DaysBefore != nil {
		current.DaysBefore = u.DaysBefore
	}
	if u.IgnoreOptOut != nil {
		current.IgnoreOptOut = *u.IgnoreOptOut
	}
	if u.BatchSize != nil && *u.BatchSize > 0 {
		current.BatchSize = *u.BatchSize
	}
	if u.TickIntervalSeconds != nil && *u.TickIntervalSeconds > 0 {
		current.TickIntervalSeconds = *u.TickIntervalSeconds
	}
	channelsJSON, _ := json.Marshal(current.Channels)
	_, err = db.Exec(ctx, `
		UPDATE notification_rules
		   SET enabled = $2, channels = $3, days_before = $4, ignore_opt_out = $5,
		       batch_size = $6, tick_interval_seconds = $7
		 WHERE event_key = $1`,
		eventKey, current.Enabled, channelsJSON, current.DaysBefore, current.IgnoreOptOut,
		current.BatchSize, current.TickIntervalSeconds)
	return current, true, err
}
