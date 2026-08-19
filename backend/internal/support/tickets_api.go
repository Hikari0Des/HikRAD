package support

// Staff-facing HTTP surface (FR-164): the queue, detail (with internal
// notes — staff only), reply/note, status transitions, and the open-count
// badge. Scoped by the existing auth.ScopeFilter, reused exactly as
// billing/tickets_api.go's ticketFilters/canSeeTicket already establish the
// pattern (kickoff instruction: reuse, don't reimplement).

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

const (
	PermView   = "support.view"
	PermManage = "support.manage"
)

func internalErr(w http.ResponseWriter, what string, err error) {
	if pkgLog != nil {
		pkgLog.Error("support: "+what+" failed", "error", err)
	}
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal server error")
}

type ticketView struct {
	ID             string     `json:"id"`
	Number         string     `json:"number"`
	SubscriberID   string     `json:"subscriber_id"`
	SubscriberName string     `json:"subscriber_username"`
	OwnerManagerID string     `json:"owner_manager_id,omitempty"`
	Subject        string     `json:"subject"`
	Priority       string     `json:"priority"`
	Status         string     `json:"status"`
	CreatedByType  string     `json:"created_by_type"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
}

func toTicketView(t Ticket) ticketView {
	return ticketView{
		ID: t.ID, Number: t.Number, SubscriberID: t.SubscriberID, SubscriberName: t.SubscriberName,
		OwnerManagerID: t.OwnerManagerID, Subject: t.Subject, Priority: t.Priority, Status: t.Status,
		CreatedByType: t.CreatedByType, CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC(),
		ResolvedAt: t.ResolvedAt, ClosedAt: t.ClosedAt,
	}
}

type messageView struct {
	ID         string    `json:"id"`
	AuthorType string    `json:"author_type"`
	AuthorID   string     `json:"author_id,omitempty"`
	Body       string    `json:"body"`
	Internal   bool      `json:"internal"`
	CreatedAt  time.Time `json:"created_at"`
}

// listTicketsHandler serves GET /support-tickets: filter chips (status,
// priority), search, scoped (an agent sees only their own subscribers'
// tickets), withdrawn hidden by default.
func listTicketsHandler(w http.ResponseWriter, r *http.Request) {
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
	items, next, err := listTickets(r.Context(), pkgDB, where, args, page.Cursor, page.Limit)
	if err != nil {
		internalErr(w, "list tickets", err)
		return
	}
	views := make([]ticketView, 0, len(items))
	for _, t := range items {
		views = append(views, toTicketView(t))
	}
	nextCursor := ""
	if len(next) == 2 {
		nextCursor = httpapi.EncodeCursor(next[0], next[1])
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(views, nextCursor))
}

// countsHandler serves GET /support-tickets/counts — the sidebar open badge,
// scoped identically to the list.
func countsHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	callerID := ""
	if mgr != nil {
		callerID = mgr.ID
	}
	scope := auth.ScopeFilter(r.Context())
	where, args := ticketFilters(r, scope, callerID, 1)
	n, err := openCount(r.Context(), pkgDB, where, args)
	if err != nil {
		internalErr(w, "ticket counts", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"open": n})
}

// canSeeTicket mirrors billing/ticket_attachments.go's canSeeTicket exactly:
// unscoped (admin) sees everything; a scoped caller only their own
// subscribers' tickets.
func canSeeTicket(ctx context.Context, ticketID string) (bool, error) {
	scope := auth.ScopeFilter(ctx)
	if scope == nil {
		return true, nil
	}
	var ownerID *string
	err := pkgDB.QueryRow(ctx,
		`SELECT s.owner_manager_id::text FROM support_tickets t
		   JOIN subscribers s ON s.id = t.subscriber_id WHERE t.id = $1::uuid`, ticketID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ownerID != nil && *ownerID == scope.ManagerID, nil
}

func requireTicketAccess(w http.ResponseWriter, r *http.Request, id string) bool {
	ok, err := canSeeTicket(r.Context(), id)
	if err != nil {
		internalErr(w, "ticket authz", err)
		return false
	}
	if !ok {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you do not have access to this ticket")
		return false
	}
	return true
}

type ticketDetailView struct {
	ticketView
	Messages []messageView `json:"messages"`
}

// getTicketHandler serves GET /support-tickets/{id} — the FULL thread,
// internal notes included: a staff-only endpoint (PermView-gated, further
// cut by canSeeTicket's scoping), never reachable by a subscriber.
func getTicketHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !requireTicketAccess(w, r, id) {
		return
	}
	t, err := getTicket(r.Context(), pkgDB, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "support ticket not found")
		return
	}
	if err != nil {
		internalErr(w, "get ticket", err)
		return
	}
	msgs, err := listMessages(r.Context(), pkgDB, id)
	if err != nil {
		internalErr(w, "list messages", err)
		return
	}
	views := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		views = append(views, messageView{ID: m.ID, AuthorType: m.AuthorType, AuthorID: m.AuthorID, Body: m.Body, Internal: m.Internal, CreatedAt: m.CreatedAt.UTC()})
	}
	httpapi.JSON(w, http.StatusOK, ticketDetailView{ticketView: toTicketView(t), Messages: views})
}

type createTicketRequest struct {
	SubscriberID string `json:"subscriber_id"`
	Subject      string `json:"subject"`
	Body         string `json:"body"`
	Priority     string `json:"priority"`
}

// createTicketHandler serves POST /support-tickets — admin/agent opens a
// ticket for any customer (FR-164).
func createTicketHandler(w http.ResponseWriter, r *http.Request) {
	var in createTicketRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.SubscriberID == "" || in.Subject == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "subscriber_id and subject are required",
			httpapi.FieldError{Field: "subscriber_id", Message: "required"})
		return
	}
	switch in.Priority {
	case "", PriorityNormal, PriorityModerate, PriorityCritical:
	default:
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid priority",
			httpapi.FieldError{Field: "priority", Message: "must be normal, moderate or critical"})
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	callerID := ""
	if mgr != nil {
		callerID = mgr.ID
	}
	t, err := createTicket(r.Context(), pkgDB, in.SubscriberID, in.Subject, in.Priority, "manager", callerID)
	if err != nil {
		internalErr(w, "create ticket", err)
		return
	}
	if in.Body != "" {
		if _, err := addMessage(r.Context(), pkgDB, t.ID, "manager", callerID, in.Body, false); err != nil {
			internalErr(w, "create ticket first message", err)
			return
		}
	}
	_ = auth.Audit(r.Context(), "support_ticket.create", "support_ticket", t.ID, nil, map[string]any{"subscriber_id": in.SubscriberID})
	httpapi.JSON(w, http.StatusCreated, toTicketView(t))
}

type addMessageRequest struct {
	Body     string `json:"body"`
	Internal bool   `json:"internal"`
}

// addMessageHandler serves POST /support-tickets/{id}/messages — a staff
// reply or internal note. internal is an explicit field the caller sets;
// there is no inference from role, and a subscriber never reaches this
// endpoint at all (it is PermManage-gated, staff-only).
func addMessageHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !requireTicketAccess(w, r, id) {
		return
	}
	var in addMessageRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Body == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "body is required",
			httpapi.FieldError{Field: "body", Message: "required"})
		return
	}
	mgr, _ := auth.ManagerFrom(r.Context())
	callerID := ""
	if mgr != nil {
		callerID = mgr.ID
	}
	m, err := addMessage(r.Context(), pkgDB, id, "manager", callerID, in.Body, in.Internal)
	if err != nil {
		internalErr(w, "add ticket message", err)
		return
	}
	// A public reply reopens a resolved/closed ticket back to in_progress —
	// an internal note never changes ticket status.
	if !in.Internal {
		if t, err := getTicket(r.Context(), pkgDB, id); err == nil && (t.Status == StatusResolved || t.Status == StatusClosed) {
			_, _ = setStatus(r.Context(), pkgDB, id, StatusInProgress)
		}
	}
	_ = auth.Audit(r.Context(), "support_ticket.message", "support_ticket", id, nil, map[string]any{"internal": in.Internal})
	httpapi.JSON(w, http.StatusCreated, messageView{ID: m.ID, AuthorType: m.AuthorType, AuthorID: m.AuthorID, Body: m.Body, Internal: m.Internal, CreatedAt: m.CreatedAt.UTC()})
}

type setStatusRequest struct {
	Status string `json:"status"`
}

func setStatusHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !requireTicketAccess(w, r, id) {
		return
	}
	var in setStatusRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	switch in.Status {
	case StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusWithdrawn:
	default:
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "invalid status",
			httpapi.FieldError{Field: "status", Message: "must be one of open, in_progress, resolved, closed, withdrawn"})
		return
	}
	t, err := setStatus(r.Context(), pkgDB, id, in.Status)
	if errors.Is(err, errTicketNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "support ticket not found")
		return
	}
	if err != nil {
		internalErr(w, "set ticket status", err)
		return
	}
	_ = auth.Audit(r.Context(), "support_ticket.status", "support_ticket", id, nil, map[string]any{"status": in.Status})
	httpapi.JSON(w, http.StatusOK, toTicketView(t))
}

// withdrawHandler serves POST /support-tickets/{id}/withdraw — staff acting
// on a customer's behalf (a subscriber's own withdrawal goes through the
// bot/portal's subscriber-facing path, public.go's WithdrawForSubscriber).
func withdrawHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !requireTicketAccess(w, r, id) {
		return
	}
	t, err := getTicket(r.Context(), pkgDB, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "support ticket not found")
		return
	}
	if err != nil {
		internalErr(w, "withdraw lookup", err)
		return
	}
	if err := withdraw(r.Context(), pkgDB, id, t.SubscriberID); err != nil {
		if errors.Is(err, errNotWithdrawable) {
			httpapi.Error(w, http.StatusConflict, "not_withdrawable", "ticket is already resolved, closed or withdrawn")
			return
		}
		internalErr(w, "withdraw ticket", err)
		return
	}
	_ = auth.Audit(r.Context(), "support_ticket.withdraw", "support_ticket", id, nil, nil)
	httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "status": StatusWithdrawn})
}
