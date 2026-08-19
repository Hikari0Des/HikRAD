package billing

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type postReceiptRequest struct {
	Amount int64  `json:"amount"`
	Method string `json:"method"`
	Memo   string `json:"memo"`
}

// createARReceiptHandler serves POST /invoices/{id}/receipts.
func (m *Module) createARReceiptHandler(w http.ResponseWriter, r *http.Request) {
	invoiceID := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if !m.invoiceVisible(r.Context(), invoiceID, auth.ScopeFilter(r.Context())) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	var in postReceiptRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Amount <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "amount", Message: "must be positive"})
		return
	}
	if in.Method == "" {
		in.Method = "cash"
	}
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "receipt begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "receipt now", err)
		return
	}
	receiptID, err := postARReceipt(r.Context(), tx, at, invoiceID, in.Amount, in.Method, in.Memo, managerID(mgr), "billing.ar_receipt")
	if err != nil {
		writeARAPError(w, m, "receipt", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "receipt commit", err)
		return
	}
	_ = auth.Audit(r.Context(), "invoice.receipt", "invoice", invoiceID, nil, map[string]any{"receipt_id": receiptID, "amount": in.Amount})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"id": receiptID, "invoice_id": invoiceID, "amount": in.Amount})
}

type postPaymentRequest struct {
	Amount int64  `json:"amount"`
	Method string `json:"method"`
	Memo   string `json:"memo"`
}

// createAPPaymentHandler serves POST /expenses/{id}/payments.
func (m *Module) createAPPaymentHandler(w http.ResponseWriter, r *http.Request) {
	expenseID := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	var in postPaymentRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Amount <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "amount", Message: "must be positive"})
		return
	}
	if in.Method == "" {
		in.Method = "cash"
	}
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "payment begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "payment now", err)
		return
	}
	paymentID, err := postAPPayment(r.Context(), tx, at, expenseID, in.Amount, in.Method, in.Memo, managerID(mgr), "billing.ap_payment")
	if err != nil {
		writeARAPError(w, m, "payment", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "payment commit", err)
		return
	}
	_ = auth.Audit(r.Context(), "expense.payment", "expense", expenseID, nil, map[string]any{"payment_id": paymentID, "amount": in.Amount})
	httpapi.JSON(w, http.StatusCreated, map[string]any{"id": paymentID, "expense_id": expenseID, "amount": in.Amount})
}

func writeARAPError(w http.ResponseWriter, m *Module, what string, err error) {
	switch err {
	case errInvoiceNotFound, errExpenseNotFound:
		httpapi.Error(w, http.StatusNotFound, "not_found", what+" target not found")
	case errInvoiceVoidOrPaid, errExpenseVoidOrPaid:
		httpapi.Error(w, http.StatusConflict, "not_settleable", "this document is void or already fully paid")
	case errOverpayment:
		httpapi.Error(w, http.StatusUnprocessableEntity, "overpayment", "amount exceeds the remaining balance")
	default:
		m.internalError(w, what, err)
	}
}
