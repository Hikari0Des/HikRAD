package subscribers

// Global instant search (FR-2, AC-2a). Substring match across username/name/
// phone using the folded trigram indexes (0102), scoped to the manager's owned
// rows (FR-27.2), capped so a broad query stays under the 300 ms budget.

import (
	"net/http"
	"strings"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

const searchLimit = 20

// searchHit is the frozen C7-D result shape.
type searchHit struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Name     *string `json:"name"`
	Phone    *string `json:"phone"`
	Status   string  `json:"status"`
}

// searchHandler is mounted at GET /api/v1/search?q=.
func (m *Module) searchHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpapi.JSON(w, http.StatusOK, map[string]any{"items": []searchHit{}})
		return
	}
	// A phone fragment may be typed in local form; normalize it so "0770…"
	// matches the stored "+964770…". Fall back to the raw fragment for
	// username/name matching.
	phoneFrag := q
	if p, ok := normalizePhone(q); ok && p != "" {
		phoneFrag = p
	}

	scope := auth.ScopeFilter(r.Context())
	// $1 folded query (name/username), $2 raw phone fragment, $3 limit, opt $4 owner.
	args := []any{q, phoneFrag, searchLimit}
	sql := `SELECT id::text, username::text, name, phone, status
	          FROM subscribers
	         WHERE ( subscriber_fold(username::text) ILIKE '%'||subscriber_fold($1)||'%'
	              OR subscriber_fold(name)          ILIKE '%'||subscriber_fold($1)||'%'
	              OR phone                          ILIKE '%'||$2||'%' )`
	clause, arg := scopeClause(scope, 4)
	sql += clause
	sql += ` ORDER BY username LIMIT $3`
	if arg != nil {
		args = append(args, arg)
	}

	rows, err := m.db.Query(r.Context(), sql, args...)
	if err != nil {
		m.internalError(w, "search", err)
		return
	}
	defer rows.Close()
	hits := make([]searchHit, 0, searchLimit)
	for rows.Next() {
		h := searchHit{Type: "subscriber"}
		if err := rows.Scan(&h.ID, &h.Username, &h.Name, &h.Phone, &h.Status); err != nil {
			m.internalError(w, "search scan", err)
			return
		}
		hits = append(hits, h)
	}
	if rows.Err() != nil {
		m.internalError(w, "search rows", rows.Err())
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": hits})
}
