// Package telegrambot is the FR-165 self-service Telegram bot: account
// linking by one-time code, and (once linked) usage/invoice lookups, ticket
// submission and follow-up, all read/written directly against the same DB
// pool the rest of hikrad-monitor uses — no HTTP hop, since the bot runs
// in-process. Internet-dependent by nature (NFR-7): Run degrades to a
// silent no-op when unconfigured or unreachable, never blocks or affects
// any other loop in the process, and never grows an unbounded queue while
// Telegram is down (the bot has no outbound queue of its own at all — see
// bot.go's doc comment).
package telegrambot

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const linkCodeTTL = 10 * time.Minute

var errAlreadyLinked = errors.New("telegrambot: chat already linked to a different subscriber")

// IssueLinkCode generates a fresh 6-digit one-time code for subscriberID,
// stored in telegram_links (upsert — one row per subscriber, re-requesting
// a code simply replaces the pending one). Called by portalapi's
// POST /api/v1/portal/telegram/link-code (the portal button; the button
// itself is v4-11's UI, this endpoint is this phase's deliverable).
func IssueLinkCode(ctx context.Context, db *pgxpool.Pool, subscriberID string) (code string, expiresAt time.Time, err error) {
	code, err = randomDigits(6)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(linkCodeTTL)
	_, err = db.Exec(ctx, `
		INSERT INTO telegram_links (subscriber_id, link_code, link_code_expires_at)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (subscriber_id) DO UPDATE
		   SET link_code = EXCLUDED.link_code, link_code_expires_at = EXCLUDED.link_code_expires_at`,
		subscriberID, code, expiresAt)
	return code, expiresAt, err
}

// consumeLinkCode matches an unexpired code to its subscriber and binds
// chatID to it — link_code is cleared on success so it can never be reused
// (a fresh /link always requires a fresh portal-issued code).
func consumeLinkCode(ctx context.Context, db *pgxpool.Pool, code, chatID string) (subscriberID string, err error) {
	// A chat already linked to someone else must not silently steal a second
	// subscriber's identity — one chat, one subscriber.
	var existing string
	err = db.QueryRow(ctx, `SELECT subscriber_id::text FROM telegram_links WHERE chat_id = $1`, chatID).Scan(&existing)
	if err == nil {
		return "", errAlreadyLinked
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = db.QueryRow(ctx, `
		UPDATE telegram_links
		   SET chat_id = $2, linked_at = now(), link_code = NULL, link_code_expires_at = NULL
		 WHERE link_code = $1 AND link_code_expires_at > now()
		RETURNING subscriber_id::text`, code, chatID).Scan(&subscriberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("invalid or expired code")
	}
	return subscriberID, err
}

// subscriberIDForChat resolves an already-linked chat to its subscriber, or
// ok=false if this chat has never linked an account.
func subscriberIDForChat(ctx context.Context, db *pgxpool.Pool, chatID string) (string, bool) {
	var id string
	err := db.QueryRow(ctx, `SELECT subscriber_id::text FROM telegram_links WHERE chat_id = $1 AND linked_at IS NOT NULL`, chatID).Scan(&id)
	return id, err == nil
}

func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, c := range b {
		out[i] = byte('0' + int(c)%10)
	}
	return string(out), nil
}
