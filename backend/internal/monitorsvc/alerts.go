package monitorsvc

// Alerts engine (FR-36). A rule fire flows: match enabled rules of the fired
// type → per-rule cooldown gate (alert-storm damping) → quiet-hours filter
// (Asia/Baghdad; suppresses telegram/email/whatsapp, never in-app) →
// enqueue into the v4-10 Notification Center's ONE queue (internal/notify) →
// one alert_events row recording which channels were queued. Quiet-hours +
// cooldown stay pure/injectable so the matrix is unit-tested with no clock,
// DB or network.
//
// v4-10: this used to dispatch each channel inline (its own senders,
// concurrent, with retry). That moved to internal/notify's outbox +
// dispatcher — a staff alert now traverses the exact same throttled queue
// as a broadcast or a subscriber's renewal reminder (binding rule: one
// queue, no side channel). alert_events is still written here, unchanged in
// shape, but "delivery" now means "queued" — the outbox is the source of
// truth for whether it actually sent.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/notify"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/push"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// baghdad is the fixed business timezone for quiet-hours math (Asia/Baghdad is
// UTC+3, no DST). Falls back to a fixed zone if the tzdata lookup fails so the
// engine never depends on the host tz database.
var baghdad = loadBaghdad()

func loadBaghdad() *time.Location {
	if loc, err := time.LoadLocation("Asia/Baghdad"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Baghdad", 3*60*60)
}

// rule is one alert_rules row the engine acts on.
type rule struct {
	ID         string
	Name       string
	Type       string
	Threshold  map[string]any
	Channels   []string
	Recipients map[string]json.RawMessage
	QuietHours *quietHours
	CooldownS  int
}

// quietHours is a daily suppression window in Asia/Baghdad ("22:00"–"07:00").
type quietHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// fireInput is one condition asking the engine to fire its rule type.
type fireInput struct {
	ruleType string
	state    string // firing | resolved
	summary  string
	payload  map[string]any
	// match, when set, further filters which rules of the type fire (used by the
	// threshold conditions, e.g. only the disk_low rule whose percent ≤ observed).
	match func(r rule) bool
	// alwaysRecord writes an in-app-only alert_events row even when no rule
	// matched, so reachability transitions always show in the feed.
	alwaysRecord bool
}

// delivery is the per-channel outcome recorded on the alert_events row. Since
// v4-10 this records QUEUE ADMISSION ("queued", true), not a completed send —
// the notify outbox (Overview/Outbox tabs) is the truthful record of whether
// a channel actually delivered.
type delivery struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
}

// alertEngine wires rule storage, cooldown, the clock, and the notify queue.
type alertEngine struct {
	db       *pgxpool.Pool
	settings platform.Settings
	log      *slog.Logger
	cooldown cooldownStore
	now      func() time.Time
	loc      *time.Location
}

func newAlertEngine(db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) *alertEngine {
	return &alertEngine{
		db:       db,
		settings: settings,
		log:      log,
		cooldown: newCooldownStore(rdb),
		now:      time.Now,
		loc:      baghdad,
	}
}

// Fire evaluates every enabled rule of the input's type and enqueues matches
// into the notify outbox.
func (a *alertEngine) Fire(ctx context.Context, in fireInput) {
	rules, err := a.loadRules(ctx, in.ruleType)
	if err != nil {
		a.log.Warn("alerts: load rules failed", "type", in.ruleType, "error", err)
		return
	}
	fired := false
	for _, r := range rules {
		if in.match != nil && !in.match(r) {
			continue
		}
		cd := time.Duration(r.CooldownS) * time.Second
		if cd > 0 && !a.cooldown.claim(ctx, r.ID, cd) {
			continue // still cooling down — storm damping
		}
		channels := a.effectiveChannels(r)
		if len(channels) == 0 {
			continue
		}
		// Push (FR-54.4) is a distinct, pre-existing infrastructure — the
		// admin panel's own Web Push subscription, not one of the v4-10
		// Notification Center's five queue channels — so it is delivered
		// directly here, same delivery-isolated posture it always had,
		// while everything else enqueues into the one shared queue.
		queueChannels := make([]string, 0, len(channels))
		for _, c := range channels {
			if c == chPush {
				a.deliverPush(ctx, in)
				continue
			}
			queueChannels = append(queueChannels, c)
		}
		if len(queueChannels) > 0 && a.db != nil {
			if err := notify.EnqueueAlert(ctx, a.db, a.settings, notify.AlertEnqueueInput{
				RuleID: r.ID, RuleType: in.ruleType, Summary: in.summary, Channels: queueChannels, Recipients: r.Recipients,
			}); err != nil {
				a.log.Warn("alerts: enqueue failed", "type", in.ruleType, "error", err)
			}
		}
		a.record(ctx, &r.ID, in, channels)
		fired = true
	}
	if !fired && in.alwaysRecord {
		// No rule configured, but keep reachability transitions in the feed.
		if a.db != nil {
			_ = notify.EnqueueAlert(ctx, a.db, a.settings, notify.AlertEnqueueInput{
				RuleType: in.ruleType, Summary: in.summary, Channels: []string{chInApp},
			})
		}
		a.record(ctx, nil, in, []string{chInApp})
	}
}

// deliverPush sends the admin panel's Web Push notification for a fire —
// keys+params only, never the rendered summary text (the PWA localizes
// client-side); failure is logged and never blocks anything else.
func (a *alertEngine) deliverPush(ctx context.Context, in fireInput) {
	params := make(map[string]any, len(in.payload)+1)
	params["state"] = in.state
	for k, v := range in.payload {
		params[k] = v
	}
	if err := push.DeliverPanel(ctx, push.Payload{
		TitleKey: "push.alert." + in.ruleType + ".title",
		BodyKey:  "push.alert." + in.ruleType + ".body",
		Params:   params,
		URL:      "/alerts",
	}); err != nil {
		a.log.Warn("alerts: push delivery failed", "type", in.ruleType, "error", err)
	}
}

// effectiveChannels drops the alert-out channels during quiet hours, always
// keeping in-app (contract C5 / gate item 6). Quiet-hours boundary events fire
// once — the cooldown gate above prevents a second fire in the same window.
func (a *alertEngine) effectiveChannels(r rule) []string {
	quiet := r.QuietHours != nil && inQuietHours(a.now().In(a.loc), *r.QuietHours)
	out := make([]string, 0, len(r.Channels))
	for _, c := range r.Channels {
		if quiet && c != chInApp {
			continue
		}
		out = append(out, c)
	}
	return out
}

// record writes the alert_events row (contract: every fire → a row showing
// which channels were queued). ruleID nil for the always-record fallback.
func (a *alertEngine) record(ctx context.Context, ruleID *string, in fireInput, channels []string) {
	if a.db == nil {
		return
	}
	payload, _ := json.Marshal(in.payload)
	deliveries := make([]delivery, 0, len(channels))
	for _, c := range channels {
		deliveries = append(deliveries, delivery{Channel: c, OK: true, Detail: "queued"})
	}
	dj, _ := json.Marshal(deliveries)
	_, err := a.db.Exec(ctx,
		`INSERT INTO alert_events (rule_id, state, type, summary, payload, deliveries)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		ruleID, in.state, in.ruleType, in.summary, payload, dj)
	if err != nil {
		a.log.Warn("alerts: record event failed", "type", in.ruleType, "error", err)
	}
}

// loadRules returns the enabled rules of a type.
func (a *alertEngine) loadRules(ctx context.Context, ruleType string) ([]rule, error) {
	if a.db == nil {
		return nil, nil
	}
	rows, err := a.db.Query(ctx,
		`SELECT id::text, name, type, threshold, channels, recipients, quiet_hours, cooldown_s
		   FROM alert_rules WHERE enabled AND type = $1`, ruleType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rule
	for rows.Next() {
		var r rule
		var threshold, channels, recipients, quiet []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &threshold, &channels, &recipients, &quiet, &r.CooldownS); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(threshold, &r.Threshold)
		_ = json.Unmarshal(channels, &r.Channels)
		_ = json.Unmarshal(recipients, &r.Recipients)
		if len(quiet) > 0 && string(quiet) != "null" {
			var qh quietHours
			if json.Unmarshal(quiet, &qh) == nil && (qh.Start != "" || qh.End != "") {
				r.QuietHours = &qh
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// inQuietHours reports whether now (already in Asia/Baghdad) falls inside the
// window, handling the overnight wrap (start > end, e.g. 22:00–07:00). Pure.
func inQuietHours(now time.Time, qh quietHours) bool {
	start, ok1 := parseHM(qh.Start)
	end, ok2 := parseHM(qh.End)
	if !ok1 || !ok2 || start == end {
		return false
	}
	mins := now.Hour()*60 + now.Minute()
	if start < end {
		return mins >= start && mins < end
	}
	// Overnight wrap: quiet if after start OR before end.
	return mins >= start || mins < end
}

// parseHM parses "HH:MM" to minutes-since-midnight.
func parseHM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// --- cooldown store ---------------------------------------------------------

// cooldownStore records the last fire of a rule so a repeat within the cooldown
// window is suppressed. Redis-backed so damping survives a monitor restart; an
// in-memory fallback keeps unit tests (and a nil-Redis degraded mode) working.
type cooldownStore interface {
	claim(ctx context.Context, ruleID string, d time.Duration) bool
}

func newCooldownStore(rdb *redis.Client) cooldownStore {
	if rdb == nil {
		return newMemoryCooldown(time.Now)
	}
	return redisCooldown{rdb: rdb}
}

type redisCooldown struct{ rdb *redis.Client }

func (c redisCooldown) claim(ctx context.Context, ruleID string, d time.Duration) bool {
	ok, err := c.rdb.SetNX(ctx, "alert:cooldown:"+ruleID, "1", d).Result()
	if err != nil {
		// On Redis error, allow the fire (never silently drop an alert).
		return true
	}
	return ok
}

type memoryCooldown struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newMemoryCooldown(now func() time.Time) *memoryCooldown {
	return &memoryCooldown{last: map[string]time.Time{}, now: now}
}

func (c *memoryCooldown) claim(_ context.Context, ruleID string, d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if t, ok := c.last[ruleID]; ok && now.Sub(t) < d {
		return false
	}
	c.last[ruleID] = now
	return true
}
