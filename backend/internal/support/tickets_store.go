// Package support owns FR-164 (support tickets): the queue, the message
// thread, and the data-level internal/public split. Deliberately named
// "support" and its tables "support_tickets"/"support_ticket_messages" —
// distinct from billing's existing payment_tickets (a payment-confirmation
// workflow, not customer support) so the two are never confused by name.
package support

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Priority/status enums (migration 0628's CHECK constraints — kept as Go
// constants so callers never hand-type the strings).
const (
	PriorityNormal   = "normal"
	PriorityModerate = "moderate"
	PriorityCritical = "critical"

	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
	StatusWithdrawn  = "withdrawn"
)

var (
	errTicketNotFound = errors.New("support: ticket not found")
	errNotWithdrawable = errors.New("support: ticket is not in a withdrawable state")
)

// Ticket is one support_tickets row.
type Ticket struct {
	ID              string
	Number          string
	SubscriberID    string
	SubscriberName  string
	OwnerManagerID  string // subscribers.owner_manager_id, joined in — the scoping key
	Subject         string
	Priority        string
	Status          string
	CreatedByType   string
	CreatedByID     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResolvedAt      *time.Time
	ClosedAt        *time.Time
}

const ticketColumns = `t.id::text, t.number, t.subscriber_id::text, COALESCE(s.name, s.username), COALESCE(s.owner_manager_id::text,''),
	t.subject, t.priority, t.status, t.created_by_type, COALESCE(t.created_by_id::text,''),
	t.created_at, t.updated_at, t.resolved_at, t.closed_at`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.Number, &t.SubscriberID, &t.SubscriberName, &t.OwnerManagerID,
		&t.Subject, &t.Priority, &t.Status, &t.CreatedByType, &t.CreatedByID,
		&t.CreatedAt, &t.UpdatedAt, &t.ResolvedAt, &t.ClosedAt)
	return t, err
}

// Message is one support_ticket_messages row — the FULL, staff-facing
// shape. Internal is the data-level flag every subscriber-facing surface
// in this package is responsible for stripping before serializing
// (public.go), never a display-time filter left to a caller.
type Message struct {
	ID         string
	TicketID   string
	AuthorType string
	AuthorID   string
	Body       string
	Internal   bool
	CreatedAt  time.Time
}

// createTicket inserts a new ticket with the next sequential human number.
func createTicket(ctx context.Context, db *pgxpool.Pool, subscriberID, subject, priority, createdByType, createdByID string) (Ticket, error) {
	if priority == "" {
		priority = PriorityNormal
	}
	var id string
	err := db.QueryRow(ctx, `
		INSERT INTO support_tickets (number, subscriber_id, subject, priority, created_by_type, created_by_id)
		VALUES ('TCK-' || lpad(nextval('support_ticket_seq')::text, 6, '0'), $1::uuid, $2, $3, $4, NULLIF($5,'')::uuid)
		RETURNING id::text`, subscriberID, subject, priority, createdByType, createdByID).Scan(&id)
	if err != nil {
		return Ticket{}, err
	}
	// Re-fetch through the joined view so SubscriberName/OwnerManagerID are
	// populated identically to every other read path.
	return getTicket(ctx, db, id)
}

func getTicket(ctx context.Context, db *pgxpool.Pool, id string) (Ticket, error) {
	return scanTicket(db.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets t JOIN subscribers s ON s.id = t.subscriber_id WHERE t.id = $1::uuid`, id))
}

// ticketFilters mirrors billing/tickets_api.go's ticketFilters exactly:
// scope=mine (default) pins to the caller's own owner_manager_id via
// auth.ScopeFilter; scope=all is honoured only for an unscoped caller.
// Reused pattern, not reimplemented logic (kickoff instruction).
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
	if v := q.Get("status"); v != "" {
		add("t.status = $%d", v)
	}
	if v := q.Get("priority"); v != "" {
		add("t.priority = $%d", v)
	}
	if v := q.Get("search"); v != "" {
		clauses = append(clauses, fmt.Sprintf("(t.number ILIKE $%d OR t.subject ILIKE $%d OR s.username ILIKE $%d)", arg, arg, arg))
		args = append(args, "%"+v+"%")
		arg++
	}
	// Withdrawn tickets are hidden by default (FR-164) unless explicitly asked for.
	if q.Get("status") == "" && q.Get("include_withdrawn") != "true" {
		clauses = append(clauses, "t.status != 'withdrawn'")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func listTickets(ctx context.Context, db *pgxpool.Pool, where string, args []any, cursor []string, limit int) ([]Ticket, []string, error) {
	if len(cursor) == 2 {
		conj := " WHERE "
		if where != "" {
			conj = " AND "
		}
		where += fmt.Sprintf("%s(t.created_at, t.id) < ($%d::timestamptz, $%d::uuid)", conj, len(args)+1, len(args)+2)
		args = append(args, cursor[0], cursor[1])
	}
	q := `SELECT ` + ticketColumns + ` FROM support_tickets t JOIN subscribers s ON s.id = t.subscriber_id` + where +
		fmt.Sprintf(" ORDER BY t.created_at DESC, t.id DESC LIMIT %d", limit+1)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var items []Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next []string
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		next = []string{last.CreatedAt.Format(time.RFC3339Nano), last.ID}
	}
	return items, next, nil
}

// openCount is the queue's sidebar badge (FR-164), scoped identically to
// the list.
func openCount(ctx context.Context, db *pgxpool.Pool, where string, args []any) (int64, error) {
	openClause := "t.status IN ('open','in_progress')"
	if where == "" {
		where = " WHERE " + openClause
	} else {
		where += " AND " + openClause
	}
	var n int64
	err := db.QueryRow(ctx, `SELECT count(*) FROM support_tickets t JOIN subscribers s ON s.id = t.subscriber_id`+where, args...).Scan(&n)
	return n, err
}

func setStatus(ctx context.Context, db *pgxpool.Pool, id, status string) (Ticket, error) {
	var resolvedSet, closedSet string
	switch status {
	case StatusResolved:
		resolvedSet = ", resolved_at = now()"
	case StatusClosed:
		closedSet = ", closed_at = now()"
	}
	ct, err := db.Exec(ctx, `UPDATE support_tickets SET status = $2`+resolvedSet+closedSet+` WHERE id = $1::uuid`, id, status)
	if err != nil {
		return Ticket{}, err
	}
	if ct.RowsAffected() == 0 {
		return Ticket{}, errTicketNotFound
	}
	return getTicket(ctx, db, id)
}

// withdraw is the subscriber's own action (FR-164): only from a
// not-yet-terminal state, and only the ticket's own subscriber may do it
// (enforced by the caller, which knows who is asking).
func withdraw(ctx context.Context, db *pgxpool.Pool, id, subscriberID string) error {
	ct, err := db.Exec(ctx, `
		UPDATE support_tickets SET status = 'withdrawn'
		 WHERE id = $1::uuid AND subscriber_id = $2::uuid AND status IN ('open','in_progress')`,
		id, subscriberID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotWithdrawable
	}
	return nil
}

// --- messages -----------------------------------------------------------

func addMessage(ctx context.Context, db *pgxpool.Pool, ticketID, authorType, authorID, body string, internal bool) (Message, error) {
	var m Message
	err := db.QueryRow(ctx, `
		INSERT INTO support_ticket_messages (ticket_id, author_type, author_id, body, internal)
		VALUES ($1::uuid, $2, NULLIF($3,'')::uuid, $4, $5)
		RETURNING id::text, ticket_id::text, author_type, COALESCE(author_id::text,''), body, internal, created_at`,
		ticketID, authorType, authorID, body, internal,
	).Scan(&m.ID, &m.TicketID, &m.AuthorType, &m.AuthorID, &m.Body, &m.Internal, &m.CreatedAt)
	return m, err
}

// listMessages returns the FULL thread (internal notes included) — staff
// endpoints only. Every subscriber/bot-facing read MUST go through
// public.go's PublicMessages filter, never this function directly.
func listMessages(ctx context.Context, db *pgxpool.Pool, ticketID string) ([]Message, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, ticket_id::text, author_type, COALESCE(author_id::text,''), body, internal, created_at
		  FROM support_ticket_messages WHERE ticket_id = $1::uuid ORDER BY created_at`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TicketID, &m.AuthorType, &m.AuthorID, &m.Body, &m.Internal, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ticketBelongsToSubscriber is the bot/portal ownership check — a
// subscriber may only ever see or act on their own ticket.
func ticketBelongsToSubscriber(ctx context.Context, db *pgxpool.Pool, ticketID, subscriberID string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM support_tickets WHERE id = $1::uuid AND subscriber_id = $2::uuid)`,
		ticketID, subscriberID).Scan(&ok)
	return ok, err
}
