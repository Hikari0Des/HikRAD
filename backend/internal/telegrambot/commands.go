package telegrambot

// Command dispatch (FR-165): account linking, usage, invoices, ticket
// submit + follow. Every command that touches a subscriber's own data
// resolves the caller strictly from the linked chat_id — a chat can never
// ask about a subscriber_id it wasn't linked to, mirroring portalapi's IDOR
// posture for the same reason. Ticket replies route through
// internal/support's public.go, which drops internal notes by construction
// — the bot has no code path that could ever see one.

import (
	"context"
	"fmt"
	"strings"

	"github.com/hikrad/hikrad/internal/support"
)

func (b *bot) handleUpdate(ctx context.Context, token string, u tgUpdate) {
	if u.Message == nil || u.Message.Text == "" {
		return
	}
	chatID := u.Message.Chat.ID
	text := strings.TrimSpace(u.Message.Text)
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.ToLower(cmd)
	arg = strings.TrimSpace(arg)

	if cmd == "/start" || cmd == "/link" {
		b.handleLink(ctx, chatID, arg)
		return
	}

	subscriberID, linked := subscriberIDForChat(ctx, b.db, fmt.Sprintf("%d", chatID))
	if !linked {
		b.reply(ctx, chatID, "This chat isn't linked to a HikRAD account yet.\nOpen the subscriber portal, request a link code, then send:\n/link <code>")
		return
	}

	switch cmd {
	case "/usage":
		b.handleUsage(ctx, chatID, subscriberID)
	case "/invoices":
		b.handleInvoices(ctx, chatID, subscriberID)
	case "/tickets":
		b.handleTickets(ctx, chatID, subscriberID)
	case "/newticket":
		b.handleNewTicket(ctx, chatID, subscriberID, arg)
	case "/help", "/start":
		b.reply(ctx, chatID, helpText)
	default:
		b.reply(ctx, chatID, "Unknown command. "+helpText)
	}
}

const helpText = "Commands:\n/usage — your current usage and expiry\n/invoices — your recent invoices\n/tickets — your support tickets\n/newticket <text> — open a new ticket\n/help — this message"

func (b *bot) handleLink(ctx context.Context, chatID int64, code string) {
	if code == "" {
		b.reply(ctx, chatID, "Send /link <code> using the one-time code shown in the subscriber portal.")
		return
	}
	subscriberID, err := consumeLinkCode(ctx, b.db, code, fmt.Sprintf("%d", chatID))
	if err != nil {
		if err == errAlreadyLinked {
			b.reply(ctx, chatID, "This Telegram chat is already linked to a different account.")
			return
		}
		b.reply(ctx, chatID, "That code is invalid or has expired. Request a new one from the portal and try again.")
		return
	}
	_ = subscriberID
	b.reply(ctx, chatID, "Linked! "+helpText)
}

func (b *bot) handleUsage(ctx context.Context, chatID int64, subscriberID string) {
	var username, status, planName string
	var expiresAt *string
	err := b.db.QueryRow(ctx, `
		SELECT s.username, s.status, COALESCE(p.name,'-'), to_char(s.expires_at, 'YYYY-MM-DD HH24:MI')
		  FROM subscribers s LEFT JOIN profiles p ON p.id = s.profile_id
		 WHERE s.id = $1::uuid`, subscriberID).Scan(&username, &status, &planName, &expiresAt)
	if err != nil {
		b.reply(ctx, chatID, "Couldn't load your usage right now — please try again shortly.")
		return
	}
	expiry := "—"
	if expiresAt != nil {
		expiry = *expiresAt
	}
	b.reply(ctx, chatID, fmt.Sprintf("Account: %s\nPlan: %s\nStatus: %s\nExpires: %s", username, planName, status, expiry))
}

func (b *bot) handleInvoices(ctx context.Context, chatID int64, subscriberID string) {
	rows, err := b.db.Query(ctx, `
		SELECT invoice_no, total, status, issue_date::text
		  FROM invoices WHERE subscriber_id = $1::uuid
		 ORDER BY issue_date DESC LIMIT 5`, subscriberID)
	if err != nil {
		b.reply(ctx, chatID, "Couldn't load your invoices right now — please try again shortly.")
		return
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var no, status, date string
		var total int64
		if err := rows.Scan(&no, &total, &status, &date); err != nil {
			break
		}
		lines = append(lines, fmt.Sprintf("%s — %d IQD — %s — %s", no, total, status, date))
	}
	if len(lines) == 0 {
		b.reply(ctx, chatID, "No invoices yet.")
		return
	}
	b.reply(ctx, chatID, "Recent invoices:\n"+strings.Join(lines, "\n"))
}

func (b *bot) handleTickets(ctx context.Context, chatID int64, subscriberID string) {
	tickets, err := support.ListForSubscriber(ctx, b.db, subscriberID)
	if err != nil {
		b.reply(ctx, chatID, "Couldn't load your tickets right now — please try again shortly.")
		return
	}
	if len(tickets) == 0 {
		b.reply(ctx, chatID, "You have no support tickets. Send /newticket <describe your issue> to open one.")
		return
	}
	var lines []string
	for _, t := range tickets {
		lines = append(lines, fmt.Sprintf("%s — %s — %s", t.Number, t.Subject, t.Status))
	}
	b.reply(ctx, chatID, "Your tickets:\n"+strings.Join(lines, "\n"))
}

func (b *bot) handleNewTicket(ctx context.Context, chatID int64, subscriberID, body string) {
	if body == "" {
		b.reply(ctx, chatID, "Send /newticket followed by a short description, e.g.\n/newticket My internet has been down since this morning")
		return
	}
	subject := body
	if len(subject) > 80 {
		subject = subject[:80]
	}
	t, err := support.CreateForSubscriber(ctx, b.db, subscriberID, subject, body, "bot")
	if err != nil {
		b.reply(ctx, chatID, "Couldn't open a ticket right now — please try again shortly.")
		return
	}
	b.reply(ctx, chatID, fmt.Sprintf("Ticket %s opened. We'll follow up here and in the portal.", t.Number))
}
