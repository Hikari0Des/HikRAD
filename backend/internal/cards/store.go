package cards

// Card group + card storage (FR-136, migrations 0603-0605).

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Group is one card_groups row — a batch of credentials sharing the same
// speed/transfer/uptime/expiry limits and shelf date.
type Group struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	UsernamePrefix     string     `json:"username_prefix"`
	CodeFormat         string     `json:"code_format"` // numeric | alpha | alphanumeric
	CodeLength         int        `json:"code_length"`
	Price              int64      `json:"price"` // IQD
	RateDownKbps       int        `json:"rate_down_kbps"`
	RateUpKbps         int        `json:"rate_up_kbps"`
	TransferMode       string     `json:"transfer_mode"` // unlimited | combined | separate
	TransferTotalBytes *int64     `json:"transfer_total_bytes"`
	TransferDownBytes  *int64     `json:"transfer_down_bytes"`
	TransferUpBytes    *int64     `json:"transfer_up_bytes"`
	UptimeLimitSeconds *int64     `json:"uptime_limit_seconds"`
	ExpireAfterSeconds *int64     `json:"expire_after_seconds"`
	ValidUntil         *time.Time `json:"valid_until"`
	Enabled            bool       `json:"enabled"`
	Source             string     `json:"source"` // native | legacy_voucher
	CreatorManagerID   *string    `json:"creator_manager_id"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	// CardCount/UsedCount/UnusedCount are attached by the list handler in a
	// single grouped query (attachCardCounts), not per-row — the same
	// "two grouped queries, not N+1" discipline profiles/store.go's
	// attachCounts already established.
	CardCount     int `json:"card_count"`
	UnusedCount   int `json:"unused_count"`
	ActiveCount   int `json:"active_count"`
	ExhaustCount  int `json:"exhausted_count"`
	DisabledCount int `json:"disabled_count"`
}

const groupColumns = `id::text, name, description, username_prefix, code_format, code_length, price,
	rate_down_kbps, rate_up_kbps, transfer_mode, transfer_total_bytes, transfer_down_bytes, transfer_up_bytes,
	uptime_limit_seconds, expire_after_seconds, valid_until, enabled, source, creator_manager_id::text,
	created_at, updated_at`

func scanGroup(row pgx.Row) (Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.UsernamePrefix, &g.CodeFormat, &g.CodeLength, &g.Price,
		&g.RateDownKbps, &g.RateUpKbps, &g.TransferMode, &g.TransferTotalBytes, &g.TransferDownBytes, &g.TransferUpBytes,
		&g.UptimeLimitSeconds, &g.ExpireAfterSeconds, &g.ValidUntil, &g.Enabled, &g.Source, &g.CreatorManagerID,
		&g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return Group{}, err
	}
	g.CreatedAt, g.UpdatedAt = g.CreatedAt.UTC(), g.UpdatedAt.UTC()
	if g.ValidUntil != nil {
		u := g.ValidUntil.UTC()
		g.ValidUntil = &u
	}
	return g, nil
}

// Card is one cards row.
type Card struct {
	ID           string     `json:"id"`
	GroupID      string     `json:"group_id"`
	State        string     `json:"state"` // unused | active | exhausted | disabled
	FirstLoginAt *time.Time `json:"first_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	// UsageDownBytes/UsageUpBytes/UptimeSeconds are derived live (never
	// stored), attached by loadCardUsage.
	UsageDownBytes int64 `json:"usage_down_bytes"`
	UsageUpBytes   int64 `json:"usage_up_bytes"`
	UptimeSeconds  int64 `json:"uptime_seconds"`
}

var errGroupNotFound = errors.New("cards: group not found")
var errCardNotFound = errors.New("cards: card not found")

func getGroup(ctx context.Context, db *pgxpool.Pool, id string) (Group, error) {
	g, err := scanGroup(db.QueryRow(ctx, `SELECT `+groupColumns+` FROM card_groups WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, errGroupNotFound
	}
	if err != nil {
		return Group{}, err
	}
	list := []Group{g}
	if err := attachCardCounts(ctx, db, list); err != nil {
		return Group{}, err
	}
	return list[0], nil
}

func listGroups(ctx context.Context, db *pgxpool.Pool) ([]Group, error) {
	rows, err := db.Query(ctx, `SELECT `+groupColumns+` FROM card_groups ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := attachCardCounts(ctx, db, out); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

// attachCardCounts fills each group's card-state tallies in ONE grouped
// query rather than one per group.
func attachCardCounts(ctx context.Context, db *pgxpool.Pool, groups []Group) error {
	if len(groups) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
		SELECT group_id::text,
		       count(*) FILTER (WHERE state = 'unused'),
		       count(*) FILTER (WHERE state = 'active'),
		       count(*) FILTER (WHERE state = 'exhausted'),
		       count(*) FILTER (WHERE state = 'disabled'),
		       count(*)
		  FROM cards
		 WHERE group_id = ANY($1::uuid[])
		 GROUP BY group_id`, groupIDs(groups))
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := map[string][5]int{}
	for rows.Next() {
		var id string
		var c [5]int
		if err := rows.Scan(&id, &c[0], &c[1], &c[2], &c[3], &c[4]); err != nil {
			return err
		}
		counts[id] = c
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range groups {
		c := counts[groups[i].ID]
		groups[i].UnusedCount, groups[i].ActiveCount, groups[i].ExhaustCount, groups[i].DisabledCount, groups[i].CardCount =
			c[0], c[1], c[2], c[3], c[4]
	}
	return nil
}

func groupIDs(groups []Group) []string {
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

func listCards(ctx context.Context, db *pgxpool.Pool, groupID string) ([]Card, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, group_id::text, state, first_login_at, created_at
		   FROM cards WHERE group_id = $1::uuid ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.GroupID, &c.State, &c.FirstLoginAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.CreatedAt = c.CreatedAt.UTC()
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachCardUsage(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachCardUsage batches the live-derived per-card usage (bytes + uptime
// since usage_cycle_anchor) for a whole page of cards via LATERAL joins,
// mirroring subscribers/limitprogress.go's approach for the subscriber list.
func attachCardUsage(ctx context.Context, db *pgxpool.Pool, list []Card) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, len(list))
	for i, c := range list {
		ids[i] = c.ID
	}
	rows, err := db.Query(ctx, `
		SELECT c.id::text,
		       COALESCE(u.down, 0), COALESCE(u.up, 0),
		       COALESCE(t.seconds, 0)
		  FROM cards c
		  LEFT JOIN LATERAL (
		      SELECT SUM(delta_down) AS down, SUM(delta_up) AS up FROM usage_points
		       WHERE card_id = c.id AND time >= c.usage_cycle_anchor
		  ) u ON true
		  LEFT JOIN LATERAL (
		      SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (
		               COALESCE(stopped_at, now()) - GREATEST(started_at, c.usage_cycle_anchor)
		             ))), 0) AS seconds
		        FROM sessions
		       WHERE card_id = c.id AND started_at IS NOT NULL
		         AND COALESCE(stopped_at, now()) > c.usage_cycle_anchor
		  ) t ON true
		 WHERE c.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	usage := map[string][3]int64{}
	for rows.Next() {
		var id string
		var down, up, secs int64
		if err := rows.Scan(&id, &down, &up, &secs); err != nil {
			return err
		}
		usage[id] = [3]int64{down, up, secs}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range list {
		u := usage[list[i].ID]
		list[i].UsageDownBytes, list[i].UsageUpBytes, list[i].UptimeSeconds = u[0], u[1], u[2]
	}
	return nil
}
