package billing

// Admin verification-queue HTTP surface (contract C8, FR-59.2). The portal
// submit endpoint (POST /portal/card-payments) lives in portalapi and calls
// through the portal_seam.go wrapper — everything here is panel/manager-only,
// gated by permCardPaymentsVerify.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

func (m *Module) listCardPaymentsHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	items, err := m.listCardPayments(r.Context(), state)
	if err != nil {
		m.internalError(w, "list card payments", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// revealCardPaymentHandler returns the card code once, per-call, and writes
// an audit entry naming the revealing manager (FR-59.4 / AC-59c).
func (m *Module) revealCardPaymentHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	code, err := m.revealCard(r.Context(), id)
	if err == errCardNotFound {
		httpapi.Error(w, http.StatusNotFound, "not_found", "card payment not found")
		return
	}
	if err != nil {
		m.internalError(w, "reveal card payment", err)
		return
	}
	_ = auth.Audit(r.Context(), "card_payment.reveal", "card_payment", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"code": code})
}

func (m *Module) approveCardPaymentHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	res, err := m.approveCard(r.Context(), id, managerID(mgr))
	if bad := m.writeCardError(w, err); bad {
		return
	}
	_ = auth.Audit(r.Context(), "card_payment.approve", "card_payment", id, nil, map[string]any{
		"ledger_tx_id": res.LedgerTxID, "receipt_no": res.ReceiptNo, "new_expires_at": res.NewExpiresAt,
	})
	httpapi.JSON(w, http.StatusOK, res)
}

type rejectCardRequest struct {
	Reason string `json:"reason" validate:"required"`
}

func (m *Module) rejectCardPaymentHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in rejectCardRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	err := m.rejectCard(r.Context(), id, managerID(mgr), in.Reason)
	if bad := m.writeCardError(w, err); bad {
		return
	}
	_ = auth.Audit(r.Context(), "card_payment.reject", "card_payment", id, nil, map[string]any{"reason": in.Reason})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "rejected"})
}

// writeCardError maps a card-payment error to the C2 envelope; returns true
// when it wrote a response (caller should return immediately).
func (m *Module) writeCardError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch err {
	case errCardNotFound:
		httpapi.Error(w, http.StatusNotFound, "not_found", "card payment not found")
	case errCardNotPending:
		httpapi.Error(w, http.StatusConflict, "not_pending", "card payment is no longer pending")
	default:
		m.internalError(w, "card payment decision", err)
	}
	return true
}
