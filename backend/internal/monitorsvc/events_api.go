package monitorsvc

// Alert-event history (contract C5): GET /api/v1/alert-events, keyset-paginated
// newest-first, optionally filtered by rule_id. This is the durable record
// behind the in-app feed and the "what fired and did it deliver" audit.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type alertEventView struct {
	ID         string          `json:"id"`
	RuleID     *string         `json:"rule_id"`
	At         time.Time       `json:"at"`
	State      string          `json:"state"`
	Type       string          `json:"type"`
	Summary    string          `json:"summary"`
	Payload    json.RawMessage `json:"payload"`
	Deliveries json.RawMessage `json:"deliveries"`
}

func listAlertEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	var cursorTS *time.Time
	var cursorID *string
	if len(page.Cursor) == 2 {
		t, terr := time.Parse(time.RFC3339Nano, page.Cursor[0])
		if terr != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		cursorTS, cursorID = &t, &page.Cursor[1]
	} else if page.Cursor != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
		return
	}

	var ruleFilter *string
	if v := r.URL.Query().Get("rule_id"); v != "" {
		ruleFilter = &v
	}

	rows, err := pkgDB.Query(ctx,
		`SELECT id::text, rule_id::text, at, state, type, summary, payload, deliveries
		   FROM alert_events
		  WHERE ($1::uuid IS NULL OR rule_id = $1::uuid)
		    AND ($2::timestamptz IS NULL OR (at, id) < ($2::timestamptz, $3::uuid))
		  ORDER BY at DESC, id DESC
		  LIMIT $4`,
		ruleFilter, cursorTS, cursorID, page.Limit+1)
	if err != nil {
		internalErr(w, "list events", err)
		return
	}
	defer rows.Close()

	items := make([]alertEventView, 0, page.Limit)
	for rows.Next() {
		var e alertEventView
		var ruleID *string
		if err := rows.Scan(&e.ID, &ruleID, &e.At, &e.State, &e.Type, &e.Summary, &e.Payload, &e.Deliveries); err != nil {
			internalErr(w, "scan event", err)
			return
		}
		e.RuleID = ruleID
		e.At = e.At.UTC()
		items = append(items, e)
	}
	if rows.Err() != nil {
		internalErr(w, "rows events", rows.Err())
		return
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = httpapi.EncodeCursor(last.At.Format(time.RFC3339Nano), last.ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}
