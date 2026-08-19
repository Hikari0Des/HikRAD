package billing

// Undo HTTP surface (FR-158 x FR-159). A scoped agent's undo parks for
// approval (approvalKindUndo, approvals_gate.go); an unscoped caller's runs
// immediately through the exact same performUndo.

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func (m *Module) undoHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	// Ownership: a scoped agent may only undo their OWN actions; an unscoped
	// caller (admin/operator) may undo anyone's — the same shape ScopeFilter
	// already applies everywhere else. Checked BEFORE parking as an approval
	// request, not inside performUndo, since performUndo also runs on the
	// approve path where the actor is legitimately the approver, not the
	// original requester.
	var ownerID string
	if err := m.db.QueryRow(r.Context(), `SELECT COALESCE(actor_manager_id::text,'') FROM undo_log WHERE id = $1::uuid`, id).Scan(&ownerID); err != nil {
		httpapi.Error(w, http.StatusNotFound, "not_found", "nothing to undo")
		return
	}
	if mgr == nil || (mgr.Scoped && mgr.ID != ownerID) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only undo your own actions")
		return
	}

	pending, requestID, result, err := m.requestOrExecute(r.Context(), approvalKindUndo, undoPayload{UndoID: id}, mgr, "undo "+id)
	if pending {
		httpapi.JSON(w, http.StatusAccepted, map[string]any{"pending_approval": true, "approval_request_id": requestID})
		return
	}
	if m.writeUndoError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "billing.undo", "undo_log", id, nil, result)
	httpapi.JSON(w, http.StatusOK, result)
}

func (m *Module) writeUndoError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errUndoNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "nothing to undo")
	case errors.Is(err, errUndoAlreadyDone):
		httpapi.Error(w, http.StatusConflict, "already_undone", "this action has already been undone")
	case errors.Is(err, errUndoExpired):
		httpapi.Error(w, http.StatusConflict, "window_closed", "the five-minute undo window has closed")
	case errors.Is(err, errUndoRecordGone):
		httpapi.Error(w, http.StatusConflict, "record_gone", "the referenced record no longer exists")
	case errors.Is(err, errUndoRecreated):
		httpapi.Error(w, http.StatusConflict, "recreated", "the referenced record was recreated since this action")
	case errors.Is(err, errUndoChanged):
		httpapi.Error(w, http.StatusConflict, "changed", "this record changed after the action and can no longer be undone")
	default:
		m.internalError(w, "undo", err)
	}
	return true
}

// listMyUndoableHandler serves GET /undo/mine — the top-bar undo widget's
// data source: this caller's own not-yet-undone, not-yet-expired actions.
func (m *Module) listMyUndoableHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil {
		httpapi.JSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, kind, entity_type, entity_id::text, created_at, expires_at
		   FROM undo_log
		  WHERE actor_manager_id = $1::uuid AND undone_at IS NULL AND expires_at > now()
		  ORDER BY created_at DESC LIMIT 20`, mgr.ID)
	if err != nil {
		m.internalError(w, "list undoable", err)
		return
	}
	defer rows.Close()
	type item struct {
		ID         string    `json:"id"`
		Kind       string    `json:"kind"`
		EntityType string    `json:"entity_type"`
		EntityID   string    `json:"entity_id"`
		CreatedAt  time.Time `json:"created_at"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Kind, &it.EntityType, &it.EntityID, &it.CreatedAt, &it.ExpiresAt); err != nil {
			m.internalError(w, "scan undoable", err)
			return
		}
		out = append(out, it)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}
