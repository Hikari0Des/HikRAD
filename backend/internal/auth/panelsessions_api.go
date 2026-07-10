package auth

// Panel session management endpoints (FR-29): a manager lists and revokes
// their own sessions; an admin (managers.edit) may act on any manager's.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

type panelSessionView struct {
	sessionRow
	Current bool `json:"current"`
}

func listPanelSessionsHandler(w http.ResponseWriter, r *http.Request) {
	m, _ := ManagerFrom(r.Context())

	target := m.ID
	// Admins may inspect another manager's sessions via ?manager_id.
	if q := r.URL.Query().Get("manager_id"); q != "" && q != m.ID {
		if !m.Can("managers.view") {
			httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have permission to view other managers' sessions")
			return
		}
		target = q
	}

	rows, err := listSessions(r.Context(), svc.db, target)
	if err != nil {
		svc.log.Error("list sessions failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	items := make([]panelSessionView, 0, len(rows))
	for _, row := range rows {
		items = append(items, panelSessionView{sessionRow: row, Current: row.ID == m.SessionID})
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func deletePanelSessionHandler(w http.ResponseWriter, r *http.Request) {
	m, _ := ManagerFrom(r.Context())
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "session not found")
		return
	}

	// An admin may revoke any session; everyone else only their own.
	owner := m.ID
	if m.Can("managers.edit") {
		owner = ""
	}

	revoked, err := revokeSession(r.Context(), svc.db, id, owner)
	if err != nil {
		// Malformed UUID surfaces here as a scan/parse error → 404, not 500.
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "session not found")
			return
		}
		svc.log.Error("revoke session failed", "error", err)
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if !revoked {
		httpapi.Error(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	_ = Audit(r.Context(), "panel_sessions.revoke", "panel_session", id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
