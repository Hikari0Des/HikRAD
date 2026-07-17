package billing

// Ledger read + export (FR-24). The ledger is filterable by manager/subscriber/
// type/date and exportable as CSV under the `export` permission. Scoped agents
// see only their own entries (FR-27.2). Reads never mutate — the ledger is
// append-only at the DB level.

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// ledgerFilters builds the shared WHERE clause + args from query params, applying
// the caller's scope. startArg is the first positional placeholder to use.
func ledgerFilters(r *http.Request, scope *auth.ManagerScope, startArg int) (string, []any) {
	var clauses []string
	var args []any
	arg := startArg
	add := func(cond string, val any) {
		clauses = append(clauses, fmt.Sprintf(cond, arg))
		args = append(args, val)
		arg++
	}
	q := r.URL.Query()
	// A scoped agent is pinned to their own entries regardless of query.
	if scope != nil {
		add("actor_manager_id = $%d::uuid", scope.ManagerID)
	} else if v := q.Get("manager_id"); v != "" {
		add("actor_manager_id = $%d::uuid", v)
	}
	if v := q.Get("subscriber_id"); v != "" {
		add("subscriber_id = $%d::uuid", v)
	}
	if v := q.Get("type"); v != "" {
		add("type = $%d", v)
	}
	// v2 phase 4 (FR-70.2): reports/ledger views run per currency, never
	// summed across one — this filter is how a caller picks which.
	if v := q.Get("currency"); v != "" {
		add("currency = $%d", v)
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("at >= $%d", t)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("at <= $%d", t)
		}
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

type ledgerItem struct {
	ID             string    `json:"id"`
	At             time.Time `json:"at"`
	Type           string    `json:"type"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	ActorManagerID *string   `json:"actor_manager_id"`
	SubscriberID   *string   `json:"subscriber_id"`
	Source         string    `json:"source"`
	Reference      string    `json:"reference"`
	ReversesID     *string   `json:"reverses_id"`
	Note           string    `json:"note"`
}

func (m *Module) ledgerListHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	scope := auth.ScopeFilter(r.Context())
	where, args := ledgerFilters(r, scope, 1)

	// Keyset pagination on (at, id) DESC for stable pages under inserts.
	if len(page.Cursor) == 2 {
		conj := " WHERE "
		if where != "" {
			conj = " AND "
		}
		where += fmt.Sprintf("%s(at, id) < ($%d::timestamptz, $%d::uuid)", conj, len(args)+1, len(args)+2)
		args = append(args, page.Cursor[0], page.Cursor[1])
	}
	q := `SELECT id::text, at, type, amount, currency, actor_manager_id::text, subscriber_id::text,
	             source, reference, reverses_id::text, note
	        FROM ledger_transactions` + where +
		fmt.Sprintf(" ORDER BY at DESC, id DESC LIMIT %d", page.Limit+1)

	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "ledger list", err)
		return
	}
	defer rows.Close()
	items := []ledgerItem{}
	for rows.Next() {
		var it ledgerItem
		if err := rows.Scan(&it.ID, &it.At, &it.Type, &it.Amount, &it.Currency, &it.ActorManagerID,
			&it.SubscriberID, &it.Source, &it.Reference, &it.ReversesID, &it.Note); err != nil {
			m.internalError(w, "ledger scan", err)
			return
		}
		it.At = it.At.UTC()
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "ledger rows", err)
		return
	}

	next := ""
	if len(items) > page.Limit {
		last := items[page.Limit-1]
		items = items[:page.Limit]
		next = httpapi.EncodeCursor(last.At.Format(time.RFC3339Nano), last.ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

// ledgerExportHandler streams the full filtered ledger as CSV (FR-24, export
// permission). It uses a single query and writes rows as they arrive so a
// 10k+-row export never buffers in memory.
func (m *Module) ledgerExportHandler(w http.ResponseWriter, r *http.Request) {
	scope := auth.ScopeFilter(r.Context())
	where, args := ledgerFilters(r, scope, 1)
	q := `SELECT id::text, to_char(at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'), type,
	             amount, currency, COALESCE(actor_manager_id::text,''), COALESCE(subscriber_id::text,''),
	             source, reference, COALESCE(reverses_id::text,''), note
	        FROM ledger_transactions` + where + ` ORDER BY at DESC, id DESC`

	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "ledger export", err)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ledger.csv"`)
	_, _ = w.Write([]byte("id,at,type,amount,currency,actor_manager_id,subscriber_id,source,reference,reverses_id,note\n"))
	for rows.Next() {
		var (
			id, at, typ, currency, actor, sub, source, ref, rev, note string
			amount                                                    int64
		)
		if err := rows.Scan(&id, &at, &typ, &amount, &currency, &actor, &sub, &source, &ref, &rev, &note); err != nil {
			return // partial CSV already sent; log-and-stop
		}
		fmt.Fprintf(w, "%s,%s,%s,%d,%s,%s,%s,%s,%s,%s,%s\n",
			id, at, typ, amount, currency, actor, sub, source, csvField(ref), rev, csvField(note))
	}
}

// csvField quotes a field that contains a comma, quote or newline (RFC 4180).
func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
