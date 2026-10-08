package notify

// Dispatcher (FR-160): the ONLY place a notification_outbox row turns into
// a real send. Runs on its own worker tick — one attempt per due row per
// tick, same shape as internal/radius's CoA dispatcher — so batch size and
// tick interval (the Rules & timers tab's throttling knobs) are genuinely
// respected under a large broadcast (gate item 7) rather than firing every
// row synchronously.

import (
	"context"
	"log/slog"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	defaultTickInterval = 5 * time.Second
	defaultBatchSize    = 50
	stuckSendingAfter   = 30 * time.Second
)

// Dispatcher drains notification_outbox on a ticker.
type Dispatcher struct {
	db      *pgxpool.Pool
	log     *slog.Logger
	senders map[Channel]channelSender
}

// NewDispatcher wires every channel sender against the shared settings/redis
// handles. rdb may be nil (in-app then always fails truthfully with "redis
// not available" rather than panicking — NFR-7 degraded mode).
func NewDispatcher(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) *Dispatcher {
	client := httpClient()
	senders := []channelSender{
		inAppSender{rdb: rdb},
		telegramSender{settings: settings, client: client},
		emailSender{settings: settings},
		whatsAppSender{settings: settings, client: client},
		smsSender{settings: settings, client: client},
	}
	m := make(map[Channel]channelSender, len(senders))
	for _, s := range senders {
		m[s.channel()] = s
	}
	return &Dispatcher{db: db, log: log, senders: m}
}

// Run drains the queue for the process lifetime.
func (d *Dispatcher) Run(ctx context.Context) {
	if d.db == nil {
		return
	}
	d.log.Info("notification dispatcher started", "interval", defaultTickInterval.String())
	ticker := time.NewTicker(defaultTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			d.Tick(tctx, defaultBatchSize)
			cancel()
		}
	}
}

// Tick runs exactly one dispatch pass — exported so tests (and
// subscriber-event integration tests that need a deterministic, single
// drain) can call it directly instead of waiting on the ticker.
func (d *Dispatcher) Tick(ctx context.Context, batchSize int) {
	if n, err := reclaimStuckSending(ctx, d.db, stuckSendingAfter); err != nil {
		d.log.Warn("notify dispatcher: reclaim stuck rows failed", "error", err)
	} else if n > 0 {
		d.log.Warn("notify dispatcher: reclaimed rows stuck in sending (process died mid-send?)", "count", n)
	}

	rows, err := claimBatch(ctx, d.db, batchSize)
	if err != nil {
		d.log.Warn("notify dispatcher: claim batch failed", "error", err)
		return
	}
	for _, row := range rows {
		d.attempt(ctx, row)
	}
}

func (d *Dispatcher) attempt(ctx context.Context, row OutboxRow) {
	sender, ok := d.senders[Channel(row.Channel)]
	if !ok {
		d.logResolveErr(row, "failed", resolveFailedAttempt(ctx, d.db, row.ID, row.AttemptCount, "unknown channel: "+row.Channel))
		return
	}
	if row.Address == "" && row.Channel != string(ChannelInApp) {
		d.logResolveErr(row, "skipped", resolveSkippedRow(ctx, d.db, row.ID, SkipNoContact))
		return
	}
	err := sender.send(ctx, row)
	if err != nil {
		if d.log != nil {
			d.log.Warn("notify dispatcher: send failed", "channel", row.Channel, "event", row.Event, "error", err)
		}
		d.logResolveErr(row, "failed", resolveFailedAttempt(ctx, d.db, row.ID, row.AttemptCount, err.Error()))
		return
	}
	d.logResolveErr(row, "sent", resolveSent(ctx, d.db, row.ID))
}

// logResolveErr surfaces a failure to record an attempt's outcome. Such a
// failure leaves the row in `sending` until reclaimStuckSending re-queues it,
// so it must never be silent (it once hid a permanently broken retry path).
func (d *Dispatcher) logResolveErr(row OutboxRow, outcome string, err error) {
	if err != nil && d.log != nil {
		d.log.Error("notify dispatcher: recording attempt outcome failed — row left in sending",
			"id", row.ID, "channel", row.Channel, "outcome", outcome, "error", err)
	}
}
