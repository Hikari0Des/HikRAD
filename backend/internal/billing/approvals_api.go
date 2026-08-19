package billing

// Approval-queue HTTP surface (FR-159). Listing/approving/rejecting the
// full queue requires approvals.manage; an agent may always list and
// withdraw their OWN pending requests with no extra permission — that
// narrower self-service ability is what "agents see and may withdraw their
// own pending requests" means, not a grant to see anyone else's queue.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/approvals"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

const permApprovalsManage = "approvals.manage"

// listApprovalRequestsHandler serves GET /approval-requests?state=&mine=.
// mine=true is always honored (self-service, no permission needed); the
// full unfiltered queue requires approvals.manage.
func (m *Module) listApprovalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	q := r.URL.Query()
	mineOnly := q.Get("mine") == "true"
	if !mineOnly && (mgr == nil || !mgr.Can(permApprovalsManage)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only view your own approval requests")
		return
	}
	mine := ""
	if mineOnly && mgr != nil {
		mine = mgr.ID
	}
	limit := 50
	items, err := approvals.List(r.Context(), m.db, q.Get("state"), mine, limit)
	if err != nil {
		m.internalError(w, "list approval requests", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (m *Module) getApprovalRequestHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	v, err := approvals.Get(r.Context(), m.db, id)
	if errors.Is(err, approvals.ErrNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "approval request not found")
		return
	}
	if err != nil {
		m.internalError(w, "get approval request", err)
		return
	}
	if mgr == nil || (v.RequestedBy != mgr.ID && !mgr.Can(permApprovalsManage)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this approval request")
		return
	}
	httpapi.JSON(w, http.StatusOK, v)
}

// approveApprovalRequestHandler serves POST /approval-requests/{id}/approve.
// Replays the ORIGINAL registered executor (approvals.Approve) — no second
// implementation exists for what "approved" means (FR-159 gate item 10).
func (m *Module) approveApprovalRequestHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	result, err := approvals.Approve(r.Context(), m.db, id, managerID(mgr))
	if m.writeApprovalError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "approval_request.approve", "approval_request", id, nil, map[string]any{"result": result})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "approved", "result": result})
}

type rejectApprovalRequest struct {
	Reason string `json:"reason"`
}

func (m *Module) rejectApprovalRequestHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	var in rejectApprovalRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &in) {
		return
	}
	err := approvals.Reject(r.Context(), m.db, id, managerID(mgr), in.Reason)
	if m.writeApprovalError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "approval_request.reject", "approval_request", id, nil, map[string]any{"reason": in.Reason})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "rejected"})
}

func (m *Module) withdrawApprovalRequestHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	err := approvals.Withdraw(r.Context(), m.db, id, managerID(mgr))
	if m.writeApprovalError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "approval_request.withdraw", "approval_request", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "withdrawn"})
}

func (m *Module) writeApprovalError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, approvals.ErrNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "approval request not found")
	case errors.Is(err, approvals.ErrNotPending):
		httpapi.Error(w, http.StatusConflict, "not_pending", "this request is no longer pending")
	case errors.Is(err, approvals.ErrUnknownKind):
		m.internalError(w, "approval kind not registered", err)
	default:
		m.internalError(w, "approval decision", err)
	}
	return true
}
