package auth

// Minimal audit-log reader (task 5): lets Agent E's user page show a change
// trail. The full audit viewer UI is Phase 3. Permission-gated (audit.view),
// keyset-paginated newest-first, filterable by entity and actor.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/httpapi"
)

type auditEntry struct {
	ID         int64           `json:"id"`
	ActorID    *string         `json:"actor_id"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	IP         string          `json:"ip"`
	UA         string          `json:"ua"`
	At         string          `json:"at"`
}

func listAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	// Keyset over the descending identity id: stable, no offsets.
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

	q := r.URL.Query()
	entityType := q.Get("entity_type")
	entityID := q.Get("entity_id")
	actorID := q.Get("actor_id")

	rows, err := svc.db.Query(r.Context(),
		`SELECT id, actor_id::text, action, entity_type, entity_id,
		        before, after, ip, ua, at
		   FROM audit_log
		  WHERE ($1 = '' OR entity_type = $1)
		    AND ($2 = '' OR entity_id = $2)
		    AND ($3 = '' OR actor_id = $3::uuid)
		    AND ($4::bigint IS NULL OR id < $4::bigint)
		  ORDER BY id DESC
		  LIMIT $5`,
		entityType, entityID, actorID, afterID, page.Limit+1)
	if err != nil {
		svc.log.Error("audit-log query failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	defer rows.Close()

	items := make([]auditEntry, 0, page.Limit)
	for rows.Next() {
		var e auditEntry
		var at time.Time
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.EntityType, &e.EntityID,
			&e.Before, &e.After, &e.IP, &e.UA, &at); err != nil {
			svc.log.Error("audit-log scan failed", "error", err)
			httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		e.At = at.UTC().Format(time.RFC3339Nano)
		items = append(items, e)
	}
	if rows.Err() != nil {
		svc.log.Error("audit-log rows failed", "error", rows.Err())
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
