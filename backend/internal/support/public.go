package support

// The subscriber-facing boundary (FR-164's most severe binding rule): an
// internal staff note must never reach a subscriber, on ANY endpoint. This
// is enforced here, once, as a data-level transform — PublicMessage simply
// has no field an internal message's body could ever occupy, and
// PublicMessages DROPS internal rows entirely rather than tagging them
// hidden. Every subscriber/bot-facing handler in this package (and the
// telegrambot package) MUST build its response through PublicTicket/
// PublicMessages, never by serializing a Ticket/Message directly — see
// internal_note_leak_test.go, which asserts this for every such endpoint.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PublicTicket is everything a subscriber (portal, v4-11, or the bot) may
// ever see about their own ticket.
type PublicTicket struct {
	ID        string    `json:"id"`
	Number    string    `json:"number"`
	Subject   string    `json:"subject"`
	Priority  string    `json:"priority"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toPublicTicket(t Ticket) PublicTicket {
	return PublicTicket{ID: t.ID, Number: t.Number, Subject: t.Subject, Priority: t.Priority, Status: t.Status, CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC()}
}

// PublicMessage is one visible thread entry — no `internal` field exists
// here at all (there is nothing to accidentally leave `false` on): a row
// that shouldn't be seen is never turned into one of these in the first
// place.
type PublicMessage struct {
	ID         string    `json:"id"`
	AuthorType string    `json:"author_type"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// PublicMessages filters msgs down to the subscriber-visible subset —
// internal rows are dropped, not masked. Takes the already-loaded slice
// (rather than querying itself) so it can be unit-tested with a
// hand-built mixed set, no DB required (internal_note_leak_test.go).
func PublicMessages(msgs []Message) []PublicMessage {
	out := make([]PublicMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Internal {
			continue
		}
		out = append(out, PublicMessage{ID: m.ID, AuthorType: m.AuthorType, Body: m.Body, CreatedAt: m.CreatedAt.UTC()})
	}
	return out
}

// PublicTicketDetail is the full subscriber/bot-facing ticket+thread view.
type PublicTicketDetail struct {
	PublicTicket
	Messages []PublicMessage `json:"messages"`
}

// --- subscriber-facing service functions ---------------------------------
// Called directly (no HTTP hop) by the bot, which runs in-process against
// the same DB pool; the future portal HTTP surface (v4-11) wraps these
// identically. Every one returns only Public* shapes.

// ListForSubscriber returns a subscriber's own tickets, newest first,
// withdrawn ones included (a subscriber may still want to see their own
// withdrawal — only the admin queue hides withdrawn by default).
func ListForSubscriber(ctx context.Context, db *pgxpool.Pool, subscriberID string) ([]PublicTicket, error) {
	rows, err := db.Query(ctx, `SELECT `+ticketColumns+`
		FROM support_tickets t JOIN subscribers s ON s.id = t.subscriber_id
		WHERE t.subscriber_id = $1::uuid ORDER BY t.created_at DESC LIMIT 50`, subscriberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicTicket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, toPublicTicket(t))
	}
	return out, rows.Err()
}

// GetForSubscriber returns one ticket + its PUBLIC-only thread, or ok=false
// if it doesn't exist or doesn't belong to subscriberID.
func GetForSubscriber(ctx context.Context, db *pgxpool.Pool, ticketID, subscriberID string) (PublicTicketDetail, bool, error) {
	owns, err := ticketBelongsToSubscriber(ctx, db, ticketID, subscriberID)
	if err != nil || !owns {
		return PublicTicketDetail{}, false, err
	}
	t, err := getTicket(ctx, db, ticketID)
	if err != nil {
		return PublicTicketDetail{}, false, err
	}
	msgs, err := listMessages(ctx, db, ticketID)
	if err != nil {
		return PublicTicketDetail{}, false, err
	}
	return PublicTicketDetail{PublicTicket: toPublicTicket(t), Messages: PublicMessages(msgs)}, true, nil
}

// CreateForSubscriber opens a new ticket on the subscriber's own behalf
// (portal or bot, createdByType is "subscriber" or "bot").
func CreateForSubscriber(ctx context.Context, db *pgxpool.Pool, subscriberID, subject, body, createdByType string) (PublicTicket, error) {
	t, err := createTicket(ctx, db, subscriberID, subject, PriorityNormal, createdByType, subscriberID)
	if err != nil {
		return PublicTicket{}, err
	}
	if body != "" {
		if _, err := addMessage(ctx, db, t.ID, "subscriber", subscriberID, body, false); err != nil {
			return PublicTicket{}, err
		}
	}
	return toPublicTicket(t), nil
}

// ReplyAsSubscriber adds a public (never internal — a subscriber has no
// way to author an internal note, by construction, not by permission
// check) message to the subscriber's own ticket.
func ReplyAsSubscriber(ctx context.Context, db *pgxpool.Pool, ticketID, subscriberID, body string) (PublicMessage, bool, error) {
	owns, err := ticketBelongsToSubscriber(ctx, db, ticketID, subscriberID)
	if err != nil || !owns {
		return PublicMessage{}, false, err
	}
	m, err := addMessage(ctx, db, ticketID, "subscriber", subscriberID, body, false)
	if err != nil {
		return PublicMessage{}, false, err
	}
	return PublicMessage{ID: m.ID, AuthorType: m.AuthorType, Body: m.Body, CreatedAt: m.CreatedAt.UTC()}, true, nil
}

// WithdrawForSubscriber withdraws the subscriber's own ticket.
func WithdrawForSubscriber(ctx context.Context, db *pgxpool.Pool, ticketID, subscriberID string) error {
	return withdraw(ctx, db, ticketID, subscriberID)
}
