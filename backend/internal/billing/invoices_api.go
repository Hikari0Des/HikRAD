package billing

// Invoice HTTP surface (FR-154).

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

// invoiceVisible reports whether invoiceID exists and, for a scoped caller,
// belongs (via its subscriber) to one of their own subscribers — the same
// ownership discipline subscriberVisible already applies to renew/refund
// (FR-27.2). A no-subscriber (owner-issued) invoice is visible only to an
// unscoped caller. Security review finding (v4-9): without this check, a
// scoped agent holding invoices.manage/invoices.create could void or post a
// receipt against ANY invoice in the system, not just ones tied to their own
// subscribers — the same class of cross-tenant leak the reseller-isolation
// contract (gate item 12) exists to prevent.
func (m *Module) invoiceVisible(ctx context.Context, invoiceID string, scope *auth.ManagerScope) bool {
	if scope == nil {
		var exists bool
		return m.db.QueryRow(ctx, `SELECT true FROM invoices WHERE id = $1::uuid`, invoiceID).Scan(&exists) == nil
	}
	var exists bool
	err := m.db.QueryRow(ctx,
		`SELECT true FROM invoices i JOIN subscribers s ON s.id = i.subscriber_id
		  WHERE i.id = $1::uuid AND s.owner_manager_id = $2::uuid`, invoiceID, scope.ManagerID).Scan(&exists)
	return err == nil
}

const (
	permInvoicesView     = "invoices.view"
	permInvoicesCreate   = "invoices.create"
	permInvoicesManage   = "invoices.manage" // void
	permInvoicesNoCharge = "invoices.no_charge"
)

type invoiceLineView struct {
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   int64   `json:"unit_price"`
	Amount      int64   `json:"amount"`
}

type invoiceView struct {
	ID           string            `json:"id"`
	InvoiceNo    string            `json:"invoice_no"`
	SubscriberID *string           `json:"subscriber_id"`
	Mode         string            `json:"mode"`
	IssueDate    string            `json:"issue_date"`
	DueDate      string            `json:"due_date"`
	TaxRate      float64           `json:"tax_rate"`
	Subtotal     int64             `json:"subtotal"`
	TaxAmount    int64             `json:"tax_amount"`
	Total        int64             `json:"total"`
	AmountPaid   int64             `json:"amount_paid"`
	Status       string            `json:"status"`
	Memo         string            `json:"memo"`
	Lines        []invoiceLineView `json:"lines,omitempty"`
}

// Qualified with i. throughout — listInvoicesHandler joins subscribers for
// the scope check (gate item 12), and both tables have their own id/status
// columns, so an unqualified column list would be ambiguous the moment that
// join is present.
const invoiceColumns = `i.id::text, i.invoice_no, i.subscriber_id::text, i.mode, i.issue_date::text, i.due_date::text,
	i.tax_rate, i.subtotal, i.tax_amount, i.total, i.amount_paid, i.status, i.memo`

func scanInvoice(row interface{ Scan(...any) error }) (invoiceView, error) {
	var v invoiceView
	err := row.Scan(&v.ID, &v.InvoiceNo, &v.SubscriberID, &v.Mode, &v.IssueDate, &v.DueDate,
		&v.TaxRate, &v.Subtotal, &v.TaxAmount, &v.Total, &v.AmountPaid, &v.Status, &v.Memo)
	return v, err
}

type createInvoiceRequest struct {
	SubscriberID string  `json:"subscriber_id"`
	Mode         string  `json:"mode"`
	DueDate      string  `json:"due_date"`
	TaxRate      float64 `json:"tax_rate"`
	Memo         string  `json:"memo"`
	Lines        []struct {
		Description string  `json:"description"`
		Category    string  `json:"category"`
		Quantity    float64 `json:"quantity"`
		UnitPrice   int64   `json:"unit_price"`
	} `json:"lines"`
}

func (m *Module) createInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createInvoiceRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.Mode != "full" && in.Mode != "prorated" && in.Mode != "no_charge" {
		fe = append(fe, httpapi.FieldError{Field: "mode", Message: "must be one of full, prorated, no_charge"})
	}
	if len(in.Lines) == 0 {
		fe = append(fe, httpapi.FieldError{Field: "lines", Message: "at least one line is required"})
	}
	dueDate, err := time.Parse("2006-01-02", in.DueDate)
	if err != nil {
		fe = append(fe, httpapi.FieldError{Field: "due_date", Message: "must be YYYY-MM-DD"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	if in.Mode == "no_charge" && (mgr == nil || !mgr.Can(permInvoicesNoCharge)) {
		httpapi.Error(w, http.StatusForbidden, "no_charge_not_permitted",
			"you do not have permission to issue a no-charge invoice")
		return
	}

	lines := make([]invoiceLineInput, len(in.Lines))
	for i, l := range in.Lines {
		lines[i] = invoiceLineInput{Description: l.Description, Category: l.Category, Quantity: l.Quantity, UnitPrice: l.UnitPrice}
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "invoice begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	var dbNow time.Time
	_ = tx.QueryRow(r.Context(), `SELECT now()`).Scan(&dbNow)
	dbNow = dbNow.UTC()

	invoiceID, err := createInvoice(r.Context(), tx, dbNow, createInvoiceParams{
		SubscriberID: in.SubscriberID, Mode: in.Mode, IssueDate: dbNow, DueDate: dueDate,
		TaxRatePct: in.TaxRate, Lines: lines, Memo: in.Memo, CreatedBy: managerID(mgr), Source: "billing.invoice",
	})
	if err != nil {
		m.internalError(w, "create invoice", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "invoice commit", err)
		return
	}
	v, err := scanInvoice(m.db.QueryRow(r.Context(), `SELECT `+invoiceColumns+` FROM invoices i WHERE i.id = $1::uuid`, invoiceID))
	if err != nil {
		m.internalError(w, "reload invoice", err)
		return
	}
	_ = auth.Audit(r.Context(), "invoice.create", "invoice", invoiceID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

func (m *Module) listInvoicesHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	subID := r.URL.Query().Get("subscriber_id")
	// Scope (FR-27.2, gate item 12): a scoped agent sees only invoices for
	// their own subscribers — the same reseller-isolation posture as every
	// other list endpoint in this package (ledgerFilters, ticketFilters).
	scope := auth.ScopeFilter(r.Context())
	scopeOwner := ""
	if scope != nil {
		scopeOwner = scope.ManagerID
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT `+invoiceColumns+` FROM invoices i
		   LEFT JOIN subscribers s ON s.id = i.subscriber_id
		  WHERE ($1 = '' OR i.status = $1) AND ($2 = '' OR i.subscriber_id::text = $2)
		    AND ($3 = '' OR s.owner_manager_id::text = $3)
		  ORDER BY i.issue_date DESC, i.id DESC LIMIT $4`, status, subID, scopeOwner, page.Limit)
	if err != nil {
		m.internalError(w, "list invoices", err)
		return
	}
	defer rows.Close()
	items := []invoiceView{}
	for rows.Next() {
		v, err := scanInvoice(rows)
		if err != nil {
			m.internalError(w, "scan invoice", err)
			return
		}
		items = append(items, v)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, ""))
}

func (m *Module) getInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !m.invoiceVisible(r.Context(), id, auth.ScopeFilter(r.Context())) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	v, err := scanInvoice(m.db.QueryRow(r.Context(), `SELECT `+invoiceColumns+` FROM invoices i WHERE i.id = $1::uuid`, id))
	if isNoRows(err) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if err != nil {
		m.internalError(w, "get invoice", err)
		return
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT description, category, quantity, unit_price, amount FROM invoice_lines WHERE invoice_id = $1::uuid`, id)
	if err != nil {
		m.internalError(w, "invoice lines", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var l invoiceLineView
		if err := rows.Scan(&l.Description, &l.Category, &l.Quantity, &l.UnitPrice, &l.Amount); err != nil {
			m.internalError(w, "scan invoice line", err)
			return
		}
		v.Lines = append(v.Lines, l)
	}
	httpapi.JSON(w, http.StatusOK, v)
}

type voidInvoiceRequest struct {
	Reason string `json:"reason"`
}

// voidInvoiceHandler serves POST /invoices/{id}/void. FR-159: a scoped
// agent's void request parks for approval instead of executing — see
// approvals_gate.go's voidInvoicePayload/executeVoidInvoice, the ONE
// implementation both this handler and approvals.Approve call.
func (m *Module) voidInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if !m.invoiceVisible(r.Context(), id, auth.ScopeFilter(r.Context())) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	var in voidInvoiceRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Reason == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "reason", Message: "a reason is required"})
		return
	}
	payload := voidInvoicePayload{InvoiceID: id, Reason: in.Reason}
	pending, requestID, _, err := m.requestOrExecute(r.Context(), approvalKindInvoiceVoid, payload, mgr, "void invoice "+id)
	if pending {
		httpapi.JSON(w, http.StatusAccepted, map[string]any{"pending_approval": true, "approval_request_id": requestID})
		return
	}
	if m.writeInvoiceError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "invoice.void", "invoice", id, nil, map[string]any{"reason": in.Reason})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "status": "void"})
}

func (m *Module) writeInvoiceError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case err == errInvoiceNotFound:
		httpapi.Error(w, http.StatusNotFound, "not_found", "invoice not found")
	case err == errInvoiceAlreadyVoid:
		httpapi.Error(w, http.StatusConflict, "already_void", "this invoice is already void")
	case err == errInvoiceHasReceipts:
		httpapi.Error(w, http.StatusConflict, "has_receipts", "this invoice has receipts posted against it; settle or reverse those first")
	default:
		m.internalError(w, "void invoice", err)
	}
	return true
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
