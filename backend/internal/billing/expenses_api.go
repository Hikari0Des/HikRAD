package billing

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

const (
	permExpensesView   = "expenses.view"
	permExpensesCreate = "expenses.create"
	permExpensesManage = "expenses.manage" // void
)

type expenseView struct {
	ID           string `json:"id"`
	ExpenseNo    string `json:"expense_no"`
	Category     string `json:"category"`
	Vendor       string `json:"vendor"`
	Amount       int64  `json:"amount"`
	AmountPaid   int64  `json:"amount_paid"`
	IncurredDate string `json:"incurred_date"`
	DueDate      string `json:"due_date"`
	Status       string `json:"status"`
	Memo         string `json:"memo"`
}

const expenseColumns = `id::text, expense_no, category, vendor, amount, amount_paid, incurred_date::text, due_date::text, status, memo`

func scanExpense(row interface{ Scan(...any) error }) (expenseView, error) {
	var v expenseView
	err := row.Scan(&v.ID, &v.ExpenseNo, &v.Category, &v.Vendor, &v.Amount, &v.AmountPaid, &v.IncurredDate, &v.DueDate, &v.Status, &v.Memo)
	return v, err
}

type createExpenseRequest struct {
	Category string `json:"category"`
	Vendor   string `json:"vendor"`
	Amount   int64  `json:"amount"`
	DueDate  string `json:"due_date"`
	Memo     string `json:"memo"`
}

func (m *Module) createExpenseHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createExpenseRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.Category == "" {
		fe = append(fe, httpapi.FieldError{Field: "category", Message: "this field is required"})
	}
	if in.Amount <= 0 {
		fe = append(fe, httpapi.FieldError{Field: "amount", Message: "must be positive"})
	}
	dueDate, err := time.Parse("2006-01-02", in.DueDate)
	if err != nil {
		fe = append(fe, httpapi.FieldError{Field: "due_date", Message: "must be YYYY-MM-DD"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "expense begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "expense now", err)
		return
	}
	expenseID, err := createExpense(r.Context(), tx, at, createExpenseParams{
		Category: in.Category, Vendor: in.Vendor, Amount: in.Amount, IncurredDate: at, DueDate: dueDate,
		Memo: in.Memo, CreatedBy: managerID(mgr), Source: "billing.expense",
	})
	if err != nil {
		m.internalError(w, "create expense", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "expense commit", err)
		return
	}
	v, err := scanExpense(m.db.QueryRow(r.Context(), `SELECT `+expenseColumns+` FROM expenses WHERE id = $1::uuid`, expenseID))
	if err != nil {
		m.internalError(w, "reload expense", err)
		return
	}
	_ = auth.Audit(r.Context(), "expense.create", "expense", expenseID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

func (m *Module) listExpensesHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	rows, err := m.db.Query(r.Context(),
		`SELECT `+expenseColumns+` FROM expenses WHERE ($1 = '' OR status = $1) ORDER BY incurred_date DESC, id DESC LIMIT $2`,
		status, page.Limit)
	if err != nil {
		m.internalError(w, "list expenses", err)
		return
	}
	defer rows.Close()
	items := []expenseView{}
	for rows.Next() {
		v, err := scanExpense(rows)
		if err != nil {
			m.internalError(w, "scan expense", err)
			return
		}
		items = append(items, v)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, ""))
}

type voidExpenseRequest struct {
	Reason string `json:"reason"`
}

func (m *Module) voidExpenseHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	var in voidExpenseRequest
	if r.ContentLength != 0 && !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Reason == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "reason", Message: "a reason is required"})
		return
	}
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "void expense begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "void expense now", err)
		return
	}
	if err := voidExpense(r.Context(), tx, at, id, in.Reason, managerID(mgr), "billing.expense_void"); err != nil {
		switch err {
		case errExpenseNotFound:
			httpapi.Error(w, http.StatusNotFound, "not_found", "expense not found")
		case errExpenseAlreadyVoid:
			httpapi.Error(w, http.StatusConflict, "already_void", "this expense is already void")
		case errExpenseHasPayments:
			httpapi.Error(w, http.StatusConflict, "has_payments", "this expense has payments posted against it; settle or reverse those first")
		default:
			m.internalError(w, "void expense", err)
		}
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "void expense commit", err)
		return
	}
	_ = auth.Audit(r.Context(), "expense.void", "expense", id, nil, map[string]any{"reason": in.Reason})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "status": "void"})
}
