// Package notify owns the v4-10 Notification Center: the one throttled
// queue every outbound message traverses (broadcast, transactional and
// staff alert alike — binding rule, no side channel), the channel senders
// (telegram/email/whatsapp/sms), per-event trilingual templates, and the
// per-event rules that gate timing/channels/opt-out. It is the new home for
// delivery mechanics that used to live, less visibly, inside monitorsvc —
// monitorsvc's alert engine and subscriber-event consumer now call into
// this package to enqueue instead of sending inline (see alerts.go /
// subscriber_events.go in that package).
package notify

// Durable notification outbox storage (FR-160, migration 0625). This is the
// ONLY place notification_outbox rows are read or written; enqueue.go
// (writers), dispatcher.go (the worker-tick state machine) and module.go
// (the panel surface) all go through it. Modeled directly on
// internal/radius's coa_outbox_store.go — the phase brief's own template
// for "a durable queue with a truthful state machine".

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the frozen channel enum (matches the notification_outbox CHECK
// constraint and the senders registered in channels.go).
type Channel string

const (
	ChannelInApp    Channel = "inapp"
	ChannelTelegram Channel = "telegram"
	ChannelEmail    Channel = "email"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelSMS      Channel = "sms"
)

// OutboxState is the frozen state machine: queued -> sending -> {sent,
// skipped} or back to queued (retry) until maxAttempts is exhausted, then
// failed. No other transition exists anywhere in this package.
type OutboxState string

const (
	StateQueued  OutboxState = "queued"
	StateSending OutboxState = "sending"
	StateSent    OutboxState = "sent"
	StateFailed  OutboxState = "failed"
	StateSkipped OutboxState = "skipped"
)

// Skip reasons (gate item 2: the outbox must be TRUTHFUL — a skip is never
// folded into the generic "failed" bucket).
const (
	SkipOptedOut        = "opted_out"
	SkipNoContact       = "no_contact"
	SkipChannelDisabled = "channel_disabled"
	SkipCancelled       = "cancelled"
	SkipNoTemplate      = "no_template"
	SkipRuleDisabled    = "rule_disabled"
)

// maxAttempts caps retries so a persistently unreachable channel (Telegram
// down, an unconfigured gateway) can never grow the queue without bound —
// NFR-7 / gate item 12. A row that exhausts its budget sits in `failed`
// with its real last_error intact, not silently dropped.
const maxAttempts = 5

// OutboxRow is one notification_outbox record.
type OutboxRow struct {
	ID              string
	Source          string
	SourceID        string
	Event           string
	SubscriberID    string
	ManagerID       string
	Address         string
	Channel         string
	Locale          string
	TemplateKey     string
	TemplateVersion *int
	Subject         string
	Body            string
	IgnoreOptOut    bool
	State           string
	AttemptCount    int
	LastError       string
	SkipReason      string
	NextAttemptAt   time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SentAt          *time.Time
}

const outboxColumns = `id::text, source, COALESCE(source_id::text,''), event, COALESCE(subscriber_id::text,''),
	COALESCE(manager_id::text,''), address, channel, locale, template_key, template_version, subject, body,
	ignore_opt_out, state, attempt_count, last_error, skip_reason, next_attempt_at, created_at, updated_at, sent_at`

func scanOutbox(row pgx.Row) (OutboxRow, error) {
	var r OutboxRow
	err := row.Scan(&r.ID, &r.Source, &r.SourceID, &r.Event, &r.SubscriberID, &r.ManagerID, &r.Address,
		&r.Channel, &r.Locale, &r.TemplateKey, &r.TemplateVersion, &r.Subject, &r.Body, &r.IgnoreOptOut,
		&r.State, &r.AttemptCount, &r.LastError, &r.SkipReason, &r.NextAttemptAt, &r.CreatedAt, &r.UpdatedAt, &r.SentAt)
	return r, err
}

// OutboxInsert is enqueue.go's write shape.
type OutboxInsert struct {
	Source          string
	SourceID        string // "" -> NULL
	Event           string
	SubscriberID    string // "" -> NULL
	ManagerID       string // "" -> NULL
	Address         string
	Channel         Channel
	Locale          string
	TemplateKey     string
	TemplateVersion *int
	Subject         string
	Body            string
	IgnoreOptOut    bool
}

func insertOutbox(ctx context.Context, db *pgxpool.Pool, in OutboxInsert) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		INSERT INTO notification_outbox
			(source, source_id, event, subscriber_id, manager_id, address, channel, locale,
			 template_key, template_version, subject, body, ignore_opt_out)
		VALUES ($1, NULLIF($2,'')::uuid, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6, $7, $8,
			$9, $10, $11, $12, $13)
		RETURNING id::text`,
		in.Source, in.SourceID, in.Event, in.SubscriberID, in.ManagerID, in.Address, string(in.Channel), in.Locale,
		in.TemplateKey, in.TemplateVersion, in.Subject, in.Body, in.IgnoreOptOut,
	).Scan(&id)
	return id, err
}

// insertSkipped writes a row that goes straight to `skipped` — used when
// enqueue.go already knows, before ever queuing an attempt, that this
// recipient/channel combination must not be sent to (opted out, no
// contact address, channel disabled). It still appears in the outbox
// (gate item 1: everything appears there) with its real skip_reason
// (gate item 2: truthful, never silently dropped).
func insertSkipped(ctx context.Context, db *pgxpool.Pool, in OutboxInsert, reason string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		INSERT INTO notification_outbox
			(source, source_id, event, subscriber_id, manager_id, address, channel, locale,
			 template_key, template_version, subject, body, ignore_opt_out, state, skip_reason)
		VALUES ($1, NULLIF($2,'')::uuid, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6, $7, $8,
			$9, $10, $11, $12, $13, 'skipped', $14)
		RETURNING id::text`,
		in.Source, in.SourceID, in.Event, in.SubscriberID, in.ManagerID, in.Address, string(in.Channel), in.Locale,
		in.TemplateKey, in.TemplateVersion, in.Subject, in.Body, in.IgnoreOptOut, reason,
	).Scan(&id)
	return id, err
}

func getOutbox(ctx context.Context, db *pgxpool.Pool, id string) (OutboxRow, error) {
	return scanOutbox(db.QueryRow(ctx, `SELECT `+outboxColumns+` FROM notification_outbox WHERE id = $1::uuid`, id))
}

// OutboxFilter is the panel Outbox tab's filter set.
type OutboxFilter struct {
	State   string
	Channel string
	Event   string
	Search  string // matches address, event or last_error, case-insensitive substring
}

func (f OutboxFilter) where(startArg int) (string, []any) {
	var clauses []string
	var args []any
	arg := startArg
	add := func(cond string, val any) {
		clauses = append(clauses, fmt.Sprintf(cond, arg))
		args = append(args, val)
		arg++
	}
	if f.State != "" {
		add("state = $%d", f.State)
	}
	if f.Channel != "" {
		add("channel = $%d", f.Channel)
	}
	if f.Event != "" {
		add("event = $%d", f.Event)
	}
	if f.Search != "" {
		clauses = append(clauses, fmt.Sprintf("(address ILIKE $%d OR event ILIKE $%d OR last_error ILIKE $%d)", arg, arg, arg))
		args = append(args, "%"+f.Search+"%")
		arg++
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

func listOutbox(ctx context.Context, db *pgxpool.Pool, f OutboxFilter, cursor []string, limit int) ([]OutboxRow, []string, error) {
	where, args := f.where(1)
	if len(cursor) == 2 {
		conj := " WHERE "
		if where != "" {
			conj = " AND "
		}
		where += fmt.Sprintf("%s(created_at, id) < ($%d::timestamptz, $%d::uuid)", conj, len(args)+1, len(args)+2)
		args = append(args, cursor[0], cursor[1])
	}
	q := `SELECT ` + outboxColumns + ` FROM notification_outbox` + where +
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d", limit+1)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var items []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, r)
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

// OutboxCounters is the Overview tab's live counts.
type OutboxCounters struct {
	Queued  int64 `json:"queued"`
	Sending int64 `json:"sending"`
	Sent    int64 `json:"sent_today"`
	Failed  int64 `json:"failed"`
	Skipped int64 `json:"skipped"`
	Total   int64 `json:"total"`
}

func outboxCounters(ctx context.Context, db *pgxpool.Pool) (OutboxCounters, error) {
	var c OutboxCounters
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'queued'),
			count(*) FILTER (WHERE state = 'sending'),
			count(*) FILTER (WHERE state = 'sent' AND sent_at >= date_trunc('day', now() AT TIME ZONE 'Asia/Baghdad')),
			count(*) FILTER (WHERE state = 'failed'),
			count(*) FILTER (WHERE state = 'skipped'),
			count(*)
		FROM notification_outbox`).Scan(&c.Queued, &c.Sending, &c.Sent, &c.Failed, &c.Skipped, &c.Total)
	return c, err
}

// --- dispatcher-only mutations -----------------------------------------

// claimBatch atomically transitions up to limit due `queued` rows to
// `sending` and returns them, FOR UPDATE SKIP LOCKED so a concurrent tick
// (or a second process) never double-claims a row.
func claimBatch(ctx context.Context, db *pgxpool.Pool, limit int) ([]OutboxRow, error) {
	rows, err := db.Query(ctx, `
		UPDATE notification_outbox
		   SET state = 'sending'
		 WHERE id IN (
		     SELECT id FROM notification_outbox
		      WHERE state = 'queued' AND next_attempt_at <= now()
		      ORDER BY next_attempt_at
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING `+outboxColumns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// reclaimStuckSending recovers rows a dead process left `sending` (crash
// safety — the queue survives a restart by construction).
func reclaimStuckSending(ctx context.Context, db *pgxpool.Pool, olderThan time.Duration) (int64, error) {
	ct, err := db.Exec(ctx, `
		UPDATE notification_outbox SET state = 'queued', next_attempt_at = now()
		 WHERE state = 'sending' AND updated_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// resolveSent marks a successful send terminal.
func resolveSent(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `
		UPDATE notification_outbox SET state = 'sent', sent_at = now(), attempt_count = attempt_count + 1
		 WHERE id = $1::uuid`, id)
	return err
}

// resolveSkippedRow marks an already-claimed row skipped (a recipient state
// discovered only at send time, e.g. opted out since enqueue).
func resolveSkippedRow(ctx context.Context, db *pgxpool.Pool, id, reason string) error {
	_, err := db.Exec(ctx, `
		UPDATE notification_outbox SET state = 'skipped', skip_reason = $2 WHERE id = $1::uuid`, id, reason)
	return err
}

// resolveFailedAttempt records the TRUTHFUL error (gate item 2) and either
// re-queues with backoff (attempts remain) or terminates in `failed` (budget
// exhausted) — the maxAttempts cap that keeps a dead channel from growing
// the queue without bound (gate item 12).
func resolveFailedAttempt(ctx context.Context, db *pgxpool.Pool, id string, attemptCount int, errText string) error {
	if attemptCount+1 >= maxAttempts {
		_, err := db.Exec(ctx, `
			UPDATE notification_outbox
			   SET state = 'failed', attempt_count = attempt_count + 1, last_error = $2
			 WHERE id = $1::uuid`, id, errText)
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE notification_outbox
		   SET state = 'queued', attempt_count = attempt_count + 1, last_error = $2,
		       next_attempt_at = now() + ($3 || ' seconds')::interval
		 WHERE id = $1::uuid`, id, errText, backoffSeconds(attemptCount+1))
	return err
}

func backoffSeconds(attempt int) int {
	s := 15 * attempt
	if s > 300 {
		return 300
	}
	return s
}

// cancelOutboxRow cancels an unsent row.
func cancelOutboxRow(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	ct, err := db.Exec(ctx, `
		UPDATE notification_outbox SET state = 'skipped', skip_reason = $2
		 WHERE id = $1::uuid AND state = 'queued'`, id, SkipCancelled)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// retryOutboxRow re-queues a failed row from scratch.
func retryOutboxRow(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	ct, err := db.Exec(ctx, `
		UPDATE notification_outbox
		   SET state = 'queued', attempt_count = 0, last_error = '', next_attempt_at = now(), sent_at = NULL
		 WHERE id = $1::uuid AND state = 'failed'`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}
