package notify

// Scanner drives the day-based subscriber events — renewal_reminder,
// expiry_notice, quota_warning — off notification_rules.days_before (FR-162's
// "timing (e.g. days before expiry)" knob). This replaces monitorsvc's old
// digestPerSubscriber, which hardcoded its own "days" straight off the
// admin expiring_digest alert_rules row; the Rules & timers tab is now the
// one place that timing is configured, and it enqueues through the queue
// like everything else rather than sending WhatsApp inline.

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const scanInterval = time.Hour

// Scanner ticks on its own schedule; a Redis daily claim per (event,
// subscriber) keeps an hourly tick from re-sending the same reminder twice
// in one calendar day (Asia/Baghdad) — same technique monitorsvc's digest
// claim already used, one level more granular (per subscriber, not per rule).
type Scanner struct {
	db  *pgxpool.Pool
	rdb *redis.Client
	log *slog.Logger
	now func() time.Time
	loc *time.Location
}

func NewScanner(db *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) *Scanner {
	return &Scanner{db: db, rdb: rdb, log: log, now: time.Now, loc: baghdad()}
}

func baghdad() *time.Location {
	if loc, err := time.LoadLocation("Asia/Baghdad"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Baghdad", 3*60*60)
}

func (s *Scanner) Run(ctx context.Context) {
	if s.db == nil {
		return
	}
	s.Tick(ctx)
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs every day-based scan once — exported so tests can call it
// deterministically instead of waiting on the hourly ticker.
func (s *Scanner) Tick(ctx context.Context) {
	s.scanExpiryWindow(ctx, EventRenewalReminder)
	s.scanExpiryWindow(ctx, EventExpiryNotice)
	s.scanQuotaWarning(ctx)
}

// scanExpiryWindow handles both renewal_reminder (days_before > 0: still
// active, expiring soon) and expiry_notice (days_before = 0: has just
// expired) off the same rule shape.
func (s *Scanner) scanExpiryWindow(ctx context.Context, eventKey string) {
	rule, ok, err := getRule(ctx, s.db, eventKey)
	if err != nil || !ok || !rule.Enabled || rule.DaysBefore == nil {
		return
	}
	days := *rule.DaysBefore

	var rows pgxRows
	var qerr error
	if days > 0 {
		rows, qerr = s.db.Query(ctx, `
			SELECT id::text, GREATEST(0, CEIL(EXTRACT(EPOCH FROM (expires_at - now())) / 86400))::int
			  FROM subscribers
			 WHERE status = 'active' AND expires_at IS NOT NULL
			   AND expires_at >= now() AND expires_at < now() + make_interval(days => $1::int)`, days)
	} else {
		// expiry_notice: subscribers whose expiry crossed in the last 24h —
		// "just expired", not every subscriber who has ever expired.
		rows, qerr = s.db.Query(ctx, `
			SELECT id::text, 0
			  FROM subscribers
			 WHERE status = 'expired' AND expires_at IS NOT NULL
			   AND expires_at >= now() - interval '1 day' AND expires_at < now()`)
	}
	if qerr != nil {
		if s.log != nil {
			s.log.Warn("notify scanner: expiry window query failed", "event", eventKey, "error", qerr)
		}
		return
	}
	defer rows.Close()
	type target struct {
		id       string
		daysLeft int
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.daysLeft); err != nil {
			return
		}
		targets = append(targets, t)
	}
	if rows.Err() != nil {
		return
	}
	for _, t := range targets {
		if !s.claimDaily(ctx, eventKey, t.id) {
			continue
		}
		_, err := EnqueueEvent(ctx, s.db, EnqueueInput{
			EventKey: eventKey, SubscriberID: t.id,
			Params: map[string]string{"days_left": strconv.Itoa(t.daysLeft)},
		})
		if err != nil && s.log != nil {
			s.log.Warn("notify scanner: enqueue failed", "event", eventKey, "subscriber_id", t.id, "error", err)
		}
	}
}

// scanQuotaWarning targets subscribers at or above 90% of a metered plan's
// data quota, consumed live off usage_points since quota_cycle_anchor — the
// same derivation accounting/quota.go trusts elsewhere (balances are always
// derived, never stored-and-edited).
func (s *Scanner) scanQuotaWarning(ctx context.Context) {
	rule, ok, err := getRule(ctx, s.db, EventQuotaWarning)
	if err != nil || !ok || !rule.Enabled {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT s.id::text
		  FROM subscribers s
		  JOIN subscriber_quota_view qv ON qv.subscriber_id = s.id
		  LEFT JOIN usage_points up ON up.subscriber_id = s.id AND up.time >= s.quota_cycle_anchor
		 WHERE s.status = 'active' AND qv.quota_mode <> 'unlimited' AND qv.quota_total_bytes > 0
		 GROUP BY s.id, qv.quota_total_bytes
		HAVING COALESCE(SUM(up.delta_down + up.delta_up), 0) >= qv.quota_total_bytes * 0.9`)
	if err != nil {
		if s.log != nil {
			s.log.Warn("notify scanner: quota warning query failed", "error", err)
		}
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return
	}
	for _, id := range ids {
		if !s.claimDaily(ctx, EventQuotaWarning, id) {
			continue
		}
		if _, err := EnqueueEvent(ctx, s.db, EnqueueInput{EventKey: EventQuotaWarning, SubscriberID: id}); err != nil && s.log != nil {
			s.log.Warn("notify scanner: enqueue failed", "event", EventQuotaWarning, "subscriber_id", id, "error", err)
		}
	}
}

func (s *Scanner) claimDaily(ctx context.Context, eventKey, subscriberID string) bool {
	if s.rdb == nil {
		return true
	}
	key := "notify:scan:" + eventKey + ":" + subscriberID + ":" + s.now().In(s.loc).Format("2006-01-02")
	ok, err := s.rdb.SetNX(ctx, key, "1", 25*time.Hour).Result()
	if err != nil {
		return true // never let a Redis hiccup silently drop the reminder
	}
	return ok
}

// pgxRows is the minimal surface used above (matches pgx.Rows) — named so
// the two query shapes in scanExpiryWindow can share one variable.
type pgxRows interface {
	Close()
	Next() bool
	Scan(dest ...any) error
	Err() error
}
