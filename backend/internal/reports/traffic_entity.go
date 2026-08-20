package reports

// Traffic by entity (FR-173): paginated, searchable, two tabs that never
// share a query. Users reads usage_daily (subscriber_id) exactly like
// usage.go's existing top_consumers view; Cards reads the new
// usage_card_daily rollup (migration 0633) joined to card_groups for a
// human-readable name and a group-level total — a card itself has no name
// (codes are hashed-only, cards.go), so "entity" for this tab is the card
// GROUP, matching where the phase brief's "rows linking to... card group"
// points. Cursor pagination follows the C2 convention (httpapi.ParsePage/
// EncodeCursor) with a (total_bytes DESC, id ASC) keyset, since these rows
// are ranked, not naturally ordered by a single column.
//
// "Today in MB, month in GB" (the phase brief's UX note) is a display
// concern only — this endpoint always returns raw bytes, same as every
// other traffic figure in this package, so the frontend formats it exactly
// like it formats every other byte count.

import (
	"net/http"
	"strconv"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type trafficEntityRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TodayDownB int64  `json:"today_down_bytes"`
	TodayUpB   int64  `json:"today_up_bytes"`
	MonthDownB int64  `json:"month_down_bytes"`
	MonthUpB   int64  `json:"month_up_bytes"`
}

func (m *Module) trafficByEntityHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := auth.ScopeFilter(ctx)
	view := r.URL.Query().Get("view")
	q := r.URL.Query().Get("q")
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}

	// UTC-anchored, matching usage_daily/usage_card_daily's own bucket anchor
	// — see top10Handler's doc comment in trends.go for why this is
	// deliberate, not a missed Asia/Baghdad conversion.
	today := "date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'"
	monthStart := "date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'"

	var cursorTotal *int64
	var cursorID string
	if len(page.Cursor) == 2 {
		if n, err := strconv.ParseInt(page.Cursor[0], 10, 64); err == nil {
			cursorTotal = &n
			cursorID = page.Cursor[1]
		}
	}

	var sql string
	args := []any{}
	switch view {
	case "users":
		sql = `WITH agg AS (
		    SELECT s.id::text AS id, s.username AS name,
		           COALESCE(SUM(u.down_bytes) FILTER (WHERE u.day = ` + today + `), 0)::bigint AS today_down,
		           COALESCE(SUM(u.up_bytes) FILTER (WHERE u.day = ` + today + `), 0)::bigint AS today_up,
		           COALESCE(SUM(u.down_bytes), 0)::bigint AS month_down,
		           COALESCE(SUM(u.up_bytes), 0)::bigint AS month_up
		      FROM subscribers s
		      JOIN usage_daily u ON u.subscriber_id = s.id AND u.day >= ` + monthStart + ` AND u.day <= ` + today + `
		     WHERE ($1 = '' OR s.username ILIKE '%' || $1 || '%')`
		args = append(args, q)
		if scope != nil {
			sql += ` AND s.owner_manager_id = $` + strconv.Itoa(len(args)+1) + `::uuid`
			args = append(args, scope.ManagerID)
		}
		sql += ` GROUP BY s.id, s.username)`
	case "cards":
		sql = `WITH agg AS (
		    SELECT g.id::text AS id, g.name AS name,
		           COALESCE(SUM(uc.down_bytes) FILTER (WHERE uc.day = ` + today + `), 0)::bigint AS today_down,
		           COALESCE(SUM(uc.up_bytes) FILTER (WHERE uc.day = ` + today + `), 0)::bigint AS today_up,
		           COALESCE(SUM(uc.down_bytes), 0)::bigint AS month_down,
		           COALESCE(SUM(uc.up_bytes), 0)::bigint AS month_up
		      FROM card_groups g
		      JOIN cards c ON c.group_id = g.id
		      JOIN usage_card_daily uc ON uc.card_id = c.id AND uc.day >= ` + monthStart + ` AND uc.day <= ` + today + `
		     WHERE ($1 = '' OR g.name ILIKE '%' || $1 || '%')`
		args = append(args, q)
		if scope != nil {
			sql += ` AND g.creator_manager_id = $` + strconv.Itoa(len(args)+1) + `::uuid`
			args = append(args, scope.ManagerID)
		}
		sql += ` GROUP BY g.id, g.name)`
	default:
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "view", Message: "must be one of: users cards"})
		return
	}

	sql += ` SELECT id, name, today_down, today_up, month_down, month_up, (month_down + month_up) AS total
	           FROM agg WHERE `
	if cursorTotal == nil {
		sql += `true`
	} else {
		sql += `(month_down + month_up) < $` + strconv.Itoa(len(args)+1) +
			` OR ((month_down + month_up) = $` + strconv.Itoa(len(args)+1) + ` AND id > $` + strconv.Itoa(len(args)+2) + `)`
		args = append(args, *cursorTotal, cursorID)
	}
	sql += ` ORDER BY total DESC, id ASC LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		m.internalError(w, "traffic by entity", err)
		return
	}
	defer rows.Close()
	var items []trafficEntityRow
	var totals []int64
	for rows.Next() {
		var t trafficEntityRow
		var total int64
		if err := rows.Scan(&t.ID, &t.Name, &t.TodayDownB, &t.TodayUpB, &t.MonthDownB, &t.MonthUpB, &total); err != nil {
			m.internalError(w, "traffic by entity scan", err)
			return
		}
		items = append(items, t)
		totals = append(totals, total)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "traffic by entity rows", err)
		return
	}

	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		totals = totals[:page.Limit]
		next = httpapi.EncodeCursor(strconv.FormatInt(totals[len(totals)-1], 10), items[len(items)-1].ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}
