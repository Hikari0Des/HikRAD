package auth

// Audit-log viewer API (FR-28.3, C7). Keyset-paginated newest-first, filterable
// by actor, entity, action and date range. Each row carries a localizable
// summary (a key + params, never prose — the panel localizes per NFR-6). CSV
// export is a separate endpoint gated on the `export` permission.
//
// before/after are already secret-redacted at write time (audit.go); the
// summary params intentionally carry only non-secret identifiers.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditEntry struct {
	ID            int64             `json:"id"`
	ActorID       *string           `json:"actor_id"`
	Action        string            `json:"action"`
	EntityType    string            `json:"entity_type"`
	EntityID      string            `json:"entity_id"`
	Before        json.RawMessage   `json:"before"`
	After         json.RawMessage   `json:"after"`
	IP            string            `json:"ip"`
	UA            string            `json:"ua"`
	At            string            `json:"at"`
	SummaryKey    string            `json:"summary_key"`
	SummaryParams map[string]string `json:"summary_params"`
}

// auditFilter is the shared filter set of the viewer + export endpoints.
type auditFilter struct {
	entityType string
	entityID   string
	actorID    string
	action     string
	from       *time.Time
	to         *time.Time
}

func parseAuditFilter(r *http.Request) (auditFilter, error) {
	q := r.URL.Query()
	f := auditFilter{
		entityType: q.Get("entity_type"),
		entityID:   q.Get("entity_id"),
		actorID:    q.Get("actor_id"),
		action:     q.Get("action"),
	}
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, fmt.Errorf("from must be RFC 3339")
		}
		f.from = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, fmt.Errorf("to must be RFC 3339")
		}
		f.to = &t
	}
	return f, nil
}

// queryAudit runs the filtered, id-descending query. afterID (keyset) and limit
// are applied; pass limit<=0 for "no limit" (export).
func queryAudit(r *http.Request, db *pgxpool.Pool, f auditFilter, afterID *int64, limit int) ([]auditEntry, error) {
	sql := `SELECT id, actor_id::text, action, entity_type, entity_id,
	               before, after, ip, ua, at
	          FROM audit_log
	         WHERE ($1 = '' OR entity_type = $1)
	           AND ($2 = '' OR entity_id = $2)
	           AND ($3 = '' OR actor_id = $3::uuid)
	           AND ($4 = '' OR action = $4)
	           AND ($5::timestamptz IS NULL OR at >= $5::timestamptz)
	           AND ($6::timestamptz IS NULL OR at <= $6::timestamptz)
	           AND ($7::bigint IS NULL OR id < $7::bigint)
	         ORDER BY id DESC`
	args := []any{f.entityType, f.entityID, f.actorID, f.action, f.from, f.to, afterID}
	if limit > 0 {
		sql += ` LIMIT $8`
		args = append(args, limit)
	}
	rows, err := db.Query(r.Context(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		var at time.Time
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.EntityType, &e.EntityID,
			&e.Before, &e.After, &e.IP, &e.UA, &at); err != nil {
			return nil, err
		}
		e.At = at.UTC().Format(time.RFC3339Nano)
		e.SummaryKey, e.SummaryParams = auditSummary(e)
		out = append(out, e)
	}
	return out, rows.Err()
}

// auditSummary produces a localizable summary key + non-secret params. The
// panel renders these via NFR-6 i18n (never server-side prose).
func auditSummary(e auditEntry) (string, map[string]string) {
	params := map[string]string{
		"entity_type": e.EntityType,
		"entity_id":   e.EntityID,
	}
	if e.ActorID != nil {
		params["actor_id"] = *e.ActorID
	}
	return "audit.action." + e.Action, params
}

func listAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	var afterID *int64
	if len(page.Cursor) == 1 {
		n, cerr := strconv.ParseInt(page.Cursor[0], 10, 64)
		if cerr != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
			return
		}
		afterID = &n
	} else if page.Cursor != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", "malformed cursor")
		return
	}

	filter, ferr := parseAuditFilter(r)
	if ferr != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_filter", ferr.Error())
		return
	}

	items, err := queryAudit(r, svc.db, filter, afterID, page.Limit+1)
	if err != nil {
		svc.log.Error("audit-log query failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		next = httpapi.EncodeCursor(strconv.FormatInt(items[len(items)-1].ID, 10))
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

// exportAuditLogHandler streams the filtered log as CSV (permission `export`).
func exportAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	filter, ferr := parseAuditFilter(r)
	if ferr != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_filter", ferr.Error())
		return
	}
	items, err := queryAudit(r, svc.db, filter, nil, 0)
	if err != nil {
		svc.log.Error("audit-log export failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	_ = Audit(r.Context(), "audit.export", "audit_log", "", nil, map[string]int{"rows": len(items)})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "at", "actor_id", "action", "entity_type", "entity_id", "ip", "ua", "summary_key"})
	for _, e := range items {
		actor := ""
		if e.ActorID != nil {
			actor = *e.ActorID
		}
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10), e.At, actor, e.Action,
			e.EntityType, e.EntityID, e.IP, e.UA, e.SummaryKey,
		})
	}
	cw.Flush()
}
