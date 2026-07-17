package billing

// Payment ticket HTTP surface (v2-2, FR-79, contracts C8/C9). Queue/log
// scoping mirrors ledger_api.go's ledgerFilters pattern exactly — same
// keyset-pagination-on-(created_at,id) shape, same auth.ScopeFilter pinning
// for a scoped agent, same explicit filters for an unscoped admin.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

type ticketItem struct {
	ID                string          `json:"id"`
	SubscriberID      string          `json:"subscriber_id"`
	SubscriberUsername string         `json:"subscriber_username"`
	ProfileID         string          `json:"profile_id"`
	MethodKey         string          `json:"method_key"`
	ProviderID        *string         `json:"provider_id"`
	Amount            int64           `json:"amount"`
	Currency          string          `json:"currency"`
	TransferReference *string         `json:"transfer_reference"`
	TransferDate      *time.Time      `json:"transfer_date"`
	Note              string          `json:"note"`
	MethodDetail      json.RawMessage `json:"method_detail"`
	State             string          `json:"state"`
	DecidedBy         *string         `json:"decided_by"`
	DecidedAt         *time.Time      `json:"decided_at"`
	RejectReason      *string         `json:"reject_reason"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	OwnerManagerID    *string         `json:"owner_manager_id"`
}

const ticketListColumns = `t.id::text, t.subscriber_id::text, s.username, t.profile_id::text, t.method_key,
	t.provider_id::text, t.amount, t.currency, t.transfer_reference, t.transfer_date, t.note, t.method_detail,
	t.state, t.decided_by::text, t.decided_at, t.reject_reason, t.created_at, t.updated_at, s.owner_manager_id::text`

func scanTicketItem(row pgx.Row) (ticketItem, error) {
	var it ticketItem
	err := row.Scan(&it.ID, &it.SubscriberID, &it.SubscriberUsername, &it.ProfileID, &it.MethodKey,
		&it.ProviderID, &it.Amount, &it.Currency, &it.TransferReference, &it.TransferDate, &it.Note, &it.MethodDetail,
		&it.State, &it.DecidedBy, &it.DecidedAt, &it.RejectReason, &it.CreatedAt, &it.UpdatedAt, &it.OwnerManagerID)
	return it, err
}

// ticketFilters mirrors ledgerFilters: scope=mine (default) pins to the
// caller's own owner_manager_id (a scoped agent's own ID, or an unscoped
// admin's own ID if they directly own subscribers); scope=all is honored
// only for an unscoped caller and drops the owner restriction (an `agent`
// query param may still narrow it) — a scoped caller's scope=all is silently
// downgraded to scope=mine, never a 403 (C9).
func ticketFilters(r *http.Request, scope *auth.ManagerScope, callerID string, startArg int) (string, []any) {
	var clauses []string
	var args []any
	arg := startArg
	add := func(cond string, val any) {
		clauses = append(clauses, fmt.Sprintf(cond, arg))
		args = append(args, val)
		arg++
	}
	q := r.URL.Query()
	all := q.Get("scope") == "all" && scope == nil
	if !all {
		owner := callerID
		if scope != nil {
			owner = scope.ManagerID
		}
		add("s.owner_manager_id = $%d::uuid", owner)
	} else if v := q.Get("agent"); v != "" {
		add("s.owner_manager_id = $%d::uuid", v)
	}
	if v := q.Get("state"); v != "" {
		add("t.state = $%d", v)
	}
	if v := q.Get("provider"); v != "" {
		add("t.provider_id = $%d::uuid", v)
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("t.created_at >= $%d", t)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("t.created_at <= $%d", t)
		}
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

// listTicketsHandler serves GET /payment-tickets (contract C9).
func (m *Module) listTicketsHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	callerID := ""
	if mgr != nil {
		callerID = mgr.ID
	}
	scope := auth.ScopeFilter(r.Context())
	where, args := ticketFilters(r, scope, callerID, 1)

	if len(page.Cursor) == 2 {
		conj := " WHERE "
		if where != "" {
			conj = " AND "
		}
		where += fmt.Sprintf("%s(t.created_at, t.id) < ($%d::timestamptz, $%d::uuid)", conj, len(args)+1, len(args)+2)
		args = append(args, page.Cursor[0], page.Cursor[1])
	}
	q := `SELECT ` + ticketListColumns + `
	        FROM payment_tickets t JOIN subscribers s ON s.id = t.subscriber_id` + where +
		fmt.Sprintf(" ORDER BY t.created_at DESC, t.id DESC LIMIT %d", page.Limit+1)

	rows, err := m.db.Query(r.Context(), q, args...)
	if err != nil {
		m.internalError(w, "list payment tickets", err)
		return
	}
	defer rows.Close()
	items := []ticketItem{}
	for rows.Next() {
		it, err := scanTicketItem(rows)
		if err != nil {
			m.internalError(w, "scan payment ticket", err)
			return
		}
		it.CreatedAt = it.CreatedAt.UTC()
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		m.internalError(w, "payment ticket rows", err)
		return
	}
	next := ""
	if len(items) > page.Limit {
		last := items[page.Limit-1]
		items = items[:page.Limit]
		next = httpapi.EncodeCursor(last.CreatedAt.Format(time.RFC3339Nano), last.ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(items, next))
}

type ticketEventView struct {
	EventType      string    `json:"event_type"`
	ActorManagerID *string   `json:"actor_manager_id"`
	Note           *string   `json:"note"`
	At             time.Time `json:"at"`
}

type ticketAttachmentView struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type ticketDetailView struct {
	ticketItem
	Events      []ticketEventView      `json:"events"`
	Attachments []ticketAttachmentView `json:"attachments"`
}

// ticketDetailHandler serves GET /payment-tickets/{id} (contract C8): the
// ticket plus its ordered timeline plus attachment metadata (never bytes —
// C10's separate authenticated endpoint owns those).
func (m *Module) ticketDetailHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := m.canSeeTicket(r.Context(), id)
	if err != nil {
		m.internalError(w, "ticket authz", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this ticket")
		return
	}
	it, err := scanTicketItem(m.db.QueryRow(r.Context(),
		`SELECT `+ticketListColumns+` FROM payment_tickets t JOIN subscribers s ON s.id = t.subscriber_id
		  WHERE t.id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "payment ticket not found")
		return
	}
	if err != nil {
		m.internalError(w, "ticket detail", err)
		return
	}
	it.CreatedAt = it.CreatedAt.UTC()

	out := ticketDetailView{ticketItem: it, Events: []ticketEventView{}, Attachments: []ticketAttachmentView{}}

	evRows, err := m.db.Query(r.Context(),
		`SELECT event_type, actor_manager_id::text, note, at FROM payment_ticket_events
		  WHERE ticket_id = $1::uuid ORDER BY at ASC`, id)
	if err != nil {
		m.internalError(w, "ticket events", err)
		return
	}
	for evRows.Next() {
		var ev ticketEventView
		if err := evRows.Scan(&ev.EventType, &ev.ActorManagerID, &ev.Note, &ev.At); err != nil {
			evRows.Close()
			m.internalError(w, "scan ticket event", err)
			return
		}
		ev.At = ev.At.UTC()
		out.Events = append(out.Events, ev)
	}
	evRows.Close()

	atRows, err := m.db.Query(r.Context(),
		`SELECT id::text, filename, content_type, size_bytes FROM payment_ticket_attachments
		  WHERE ticket_id = $1::uuid ORDER BY uploaded_at ASC`, id)
	if err != nil {
		m.internalError(w, "ticket attachments", err)
		return
	}
	for atRows.Next() {
		var av ticketAttachmentView
		if err := atRows.Scan(&av.ID, &av.Filename, &av.ContentType, &av.SizeBytes); err != nil {
			atRows.Close()
			m.internalError(w, "scan ticket attachment", err)
			return
		}
		out.Attachments = append(out.Attachments, av)
	}
	atRows.Close()

	httpapi.JSON(w, http.StatusOK, out)
}

// approveTicketHandler serves POST /payment-tickets/{id}/approve.
func (m *Module) approveTicketHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := m.canSeeTicket(r.Context(), id)
	if err != nil {
		m.internalError(w, "ticket authz", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this ticket")
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	approverID := ""
	if mgr != nil {
		approverID = mgr.ID
	}
	rr, err := m.approveTicket(r.Context(), id, approverID)
	if m.writeTicketError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "payment_ticket.approve", "payment_ticket", id, nil, map[string]any{"ledger_tx_id": rr.LedgerTxID})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "approved", "new_expires_at": rr.NewExpiresAt})
}

type rejectTicketRequest struct {
	Reason string `json:"reason"`
}

// rejectTicketHandler serves POST /payment-tickets/{id}/reject.
func (m *Module) rejectTicketHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := m.canSeeTicket(r.Context(), id)
	if err != nil {
		m.internalError(w, "ticket authz", err)
		return
	}
	if !ok {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this ticket")
		return
	}
	var in rejectTicketRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	approverID := ""
	if mgr != nil {
		approverID = mgr.ID
	}
	err = m.rejectTicket(r.Context(), id, approverID, in.Reason)
	if m.writeTicketError(w, err) {
		return
	}
	_ = auth.Audit(r.Context(), "payment_ticket.reject", "payment_ticket", id, nil, map[string]any{"reason": in.Reason})
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "state": "rejected"})
}

// writeTicketError maps tickets.go's sentinel errors to their HTTP shape;
// reports true (already written) when err is non-nil.
func (m *Module) writeTicketError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errTicketNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "payment ticket not found")
	case errors.Is(err, errTicketNotPending):
		httpapi.Error(w, http.StatusConflict, "not_pending", "payment ticket is no longer pending")
	case errors.Is(err, errMethodNotAllowed):
		httpapi.Error(w, http.StatusForbidden, "method_not_allowed", "this payment method is not enabled")
	default:
		m.internalError(w, "payment ticket decision", err)
	}
	return true
}
