package monitorsvc

// Probe engine (FR-34/FR-60). One scheduler drives ICMP every 15 s and SNMP
// every 60 s across both target kinds (NAS + monitored device). ICMP results
// feed each target's state machine; the down/up edge fires the matching alert
// rule type and, on a NAS recovery, publishes nas.recovered (C5) and flags the
// NAS's still-open sessions so missing Stops surface immediately (the reaper
// still owns closure). Every probe is bounded by a timeout and per-target
// in-flight guard so a black-holed target never piles work up (edge case).

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Frozen pub/sub channel (contract C5/C4): a NAS transitioning back up. B may
// reconcile on it; C flags missing Stops locally regardless.
const chanNASRecovered = "nas.recovered"

// Probe cadences (contract C5: ICMP 15 s, SNMP 60 s).
const (
	icmpInterval    = 15 * time.Second
	snmpInterval    = 60 * time.Second
	targetReload    = 60 * time.Second
	probeConcurrent = 32 // bounded fan-out so a flood of targets can't exhaust FDs
)

// Engine runs the probe loops in the hikrad-monitor process.
type Engine struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	log    *slog.Logger
	pinger Pinger
	snmp   SNMPClient
	alerts *alertEngine
	now    func() time.Time

	mu       sync.Mutex
	targets  []target
	states   map[string]*targetState // key -> reachability state
	inflight map[string]bool         // key -> a probe is running (anti-pileup)

	sem chan struct{}
}

// NewEngine builds the probe engine with the production ICMP/SNMP clients.
func NewEngine(db *pgxpool.Pool, rdb *redis.Client, alerts *alertEngine, log *slog.Logger) *Engine {
	return &Engine{
		db:       db,
		rdb:      rdb,
		log:      log,
		pinger:   NewSystemPinger(),
		snmp:     NewUDPSNMP(),
		alerts:   alerts,
		now:      time.Now,
		states:   map[string]*targetState{},
		inflight: map[string]bool{},
		sem:      make(chan struct{}, probeConcurrent),
	}
}

func stateKey(t target) string { return string(t.kind) + ":" + t.id }

// Run drives the probe loops until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	e.reloadTargets(ctx)
	icmpT := time.NewTicker(icmpInterval)
	snmpT := time.NewTicker(snmpInterval)
	reloadT := time.NewTicker(targetReload)
	defer icmpT.Stop()
	defer snmpT.Stop()
	defer reloadT.Stop()

	e.log.Info("probe engine started", "icmp_interval", icmpInterval.String(), "snmp_interval", snmpInterval.String())
	// Prime the first ICMP sweep immediately so state is fresh within one cycle.
	e.sweepICMP(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-icmpT.C:
			e.sweepICMP(ctx)
		case <-snmpT.C:
			e.sweepSNMP(ctx)
		case <-reloadT.C:
			e.reloadTargets(ctx)
		}
	}
}

func (e *Engine) reloadTargets(ctx context.Context) {
	if e.db == nil {
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ts, err := loadTargets(tctx, e.db)
	if err != nil {
		e.log.Warn("probe: reload targets failed", "error", err)
		return
	}
	e.mu.Lock()
	e.targets = ts
	// Drop state for targets that disappeared so the map doesn't grow unbounded.
	live := make(map[string]bool, len(ts))
	for _, t := range ts {
		live[stateKey(t)] = true
	}
	for k := range e.states {
		if !live[k] {
			delete(e.states, k)
		}
	}
	e.mu.Unlock()
}

func (e *Engine) snapshotTargets() []target {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]target, len(e.targets))
	copy(out, e.targets)
	return out
}

// sweepICMP probes every target once, bounded by the semaphore and a per-target
// in-flight guard so a slow target's probe doesn't overlap the next sweep.
func (e *Engine) sweepICMP(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range e.snapshotTargets() {
		key := stateKey(t)
		if !e.tryClaim(key) {
			continue // previous probe of this target still running
		}
		wg.Add(1)
		go func(t target, key string) {
			defer wg.Done()
			defer e.release(key)
			e.acquire()
			defer e.releaseSem()
			e.probeICMP(ctx, t)
		}(t, key)
	}
	wg.Wait()
}

func (e *Engine) sweepSNMP(ctx context.Context) {
	for _, t := range e.snapshotTargets() {
		if t.community == "" {
			continue
		}
		go func(t target) {
			e.acquire()
			defer e.releaseSem()
			e.probeSNMP(ctx, t)
		}(t)
	}
}

// probeICMP runs one echo, records it, and folds it into the state machine.
func (e *Engine) probeICMP(ctx context.Context, t target) {
	pctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	res := e.pinger.Ping(pctx, t.ip)

	loss := 0.0
	if !res.OK {
		loss = 1.0
	}
	e.writeProbe(ctx, probeRow{
		target: t, kind: "icmp", latency: res.LatencyMS, loss: loss, ok: res.OK,
	})
	e.observe(ctx, t, res.OK)
}

// probeSNMP polls the agent and records the CPU/mem/uptime sample. SNMP never
// affects reachability state (that's ICMP's job).
func (e *Engine) probeSNMP(ctx context.Context, t target) {
	pctx, cancel := context.WithTimeout(ctx, snmpTimeout)
	defer cancel()
	m := e.snmp.Poll(pctx, t.ip, t.community)
	if !m.OK {
		return
	}
	e.writeProbe(ctx, probeRow{
		target: t, kind: "snmp", cpu: m.CPU, mem: m.Mem, uptime: m.UptimeSec, ok: true,
	})
}

// observe applies an ICMP outcome and fires transition side effects.
func (e *Engine) observe(ctx context.Context, t target, ok bool) {
	key := stateKey(t)
	e.mu.Lock()
	st := e.states[key]
	if st == nil {
		st = &targetState{}
		e.states[key] = st
	}
	tr := st.observe(ok)
	e.mu.Unlock()

	switch {
	case tr.toDown:
		e.onDown(ctx, t)
	case tr.toUp:
		e.onUp(ctx, t)
	}
}

func (e *Engine) onDown(ctx context.Context, t target) {
	e.log.Warn("target down", "kind", t.kind, "name", t.name, "ip", t.ip)
	ruleType := "nas_down"
	if t.kind == kindDevice {
		ruleType = "device_down"
	}
	if e.alerts != nil {
		e.alerts.Fire(ctx, fireInput{
			ruleType: ruleType,
			state:    "firing",
			summary:  string(t.kind) + " down: " + t.name + " (" + t.ip + ")",
			payload:  map[string]any{"kind": string(t.kind), "id": t.id, "name": t.name, "ip": t.ip},
			// Reachability transitions are always recorded for the feed even if no
			// external rule is configured.
			alwaysRecord: true,
		})
	}
}

func (e *Engine) onUp(ctx context.Context, t target) {
	e.log.Info("target recovered", "kind", t.kind, "name", t.name, "ip", t.ip)
	ruleType := "nas_up"
	if t.kind == kindDevice {
		ruleType = "device_up"
	}
	if e.alerts != nil {
		e.alerts.Fire(ctx, fireInput{
			ruleType:     ruleType,
			state:        "resolved",
			summary:      string(t.kind) + " recovered: " + t.name + " (" + t.ip + ")",
			payload:      map[string]any{"kind": string(t.kind), "id": t.id, "name": t.name, "ip": t.ip},
			alwaysRecord: true,
		})
	}
	if t.kind == kindNAS {
		e.publishRecovered(ctx, t.id)
		e.reconcileMissingStops(ctx, t.id)
	}
}

// publishRecovered emits nas.recovered for B's reconciliation (best-effort).
func (e *Engine) publishRecovered(ctx context.Context, nasID string) {
	if e.rdb == nil {
		return
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := e.rdb.Publish(pctx, chanNASRecovered, `{"nas_id":"`+nasID+`"}`).Err(); err != nil {
		e.log.Warn("probe: publish nas.recovered failed", "error", err, "nas_id", nasID)
	}
}

// reconcileMissingStops flags every session still open on a recovered NAS as
// stale so the panel dims it immediately — a session that survived a NAS outage
// almost certainly missed its Stop. Closure stays the reaper's job (FR-38); we
// only flag (contract: "flag, don't synthesize"). Idempotent.
func (e *Engine) reconcileMissingStops(ctx context.Context, nasID string) {
	if e.db == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	rows, err := e.db.Query(rctx,
		`UPDATE sessions SET stale = true
		  WHERE nas_id = $1::uuid AND stopped_at IS NULL AND stale = false
		  RETURNING acct_session_id, COALESCE(subscriber_id::text,''), service`, nasID)
	if err != nil {
		e.log.Warn("probe: reconcile missing stops failed", "error", err, "nas_id", nasID)
		return
	}
	type flagged struct{ acctID, sub, service string }
	var list []flagged
	for rows.Next() {
		var f flagged
		if err := rows.Scan(&f.acctID, &f.sub, &f.service); err != nil {
			rows.Close()
			e.log.Warn("probe: reconcile scan failed", "error", err)
			return
		}
		list = append(list, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		e.log.Warn("probe: reconcile rows failed", "error", err)
		return
	}
	// Reflect the stale flag in the live hash so the SSE feed dims them too.
	if e.rdb != nil {
		for _, f := range list {
			field := livestate.Field(nasID, f.acctID)
			if raw, herr := e.rdb.HGet(rctx, livestate.HashKey, field).Bytes(); herr == nil {
				if s, uerr := livestate.Unmarshal(raw); uerr == nil {
					s.Stale = true
					_ = livestate.Upsert(rctx, e.rdb, s)
				}
			}
		}
	}
	if len(list) > 0 {
		e.log.Info("probe: flagged sessions with missing stops on NAS recovery",
			"nas_id", nasID, "count", len(list))
	}
}

// --- probe row persistence + concurrency plumbing ---------------------------

type probeRow struct {
	target  target
	kind    string
	latency float64
	loss    float64
	cpu     float64
	mem     float64
	uptime  int64
	ok      bool
}

func (e *Engine) writeProbe(ctx context.Context, r probeRow) {
	if e.db == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var latency, loss, cpu, mem any
	var uptime any
	if r.kind == "icmp" {
		if r.ok {
			latency = r.latency
		}
		loss = r.loss
	} else {
		if r.cpu > 0 {
			cpu = r.cpu
		}
		if r.mem > 0 {
			mem = r.mem
		}
		if r.uptime > 0 {
			uptime = r.uptime
		}
	}
	_, err := e.db.Exec(wctx,
		`INSERT INTO health_probes (at, nas_id, device_id, kind, latency_ms, loss, cpu, mem, uptime, ok)
		 VALUES (now(), $1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		r.target.nasID(), r.target.deviceID(), r.kind, latency, loss, cpu, mem, uptime, r.ok)
	if err != nil {
		e.log.Warn("probe: write failed", "error", err, "kind", r.kind, "target", r.target.name)
	}
}

func (e *Engine) acquire()    { e.sem <- struct{}{} }
func (e *Engine) releaseSem() { <-e.sem }

// tryClaim marks a target's probe in-flight, returning false if one is already
// running (so the next sweep skips it instead of stacking a second probe).
func (e *Engine) tryClaim(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inflight[key] {
		return false
	}
	e.inflight[key] = true
	return true
}

func (e *Engine) release(key string) {
	e.mu.Lock()
	delete(e.inflight, key)
	e.mu.Unlock()
}
