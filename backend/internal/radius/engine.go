package radius

// engine holds the wired dependencies of the authorize policy path. It is
// constructed once in module.Register and stashed as the package default so
// the InvalidatePolicy hook (a package-level function D calls) can reach Redis.
// Unit tests build engines directly with fakes for the DB/Redis-backed seams.

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/redis/go-redis/v9"
)

type engine struct {
	rdb *redis.Client
	log *slog.Logger
	now func() time.Time
	// decrypt opens an AES-GCM-sealed subscriber password (platform/crypto C3).
	// A seam so unit tests can supply their own key without the process global.
	decrypt func([]byte) ([]byte, error)
	// nasKnown reports whether an enabled NAS with this source IP is registered
	// (FR-13.2). Backed by the DB-driven client registry; stubbed in tests.
	nasKnown func(ctx context.Context, ip string) (bool, error)
}

var (
	defaultEngineMu sync.RWMutex
	defaultEng      *engine
)

func setDefaultEngine(e *engine) {
	defaultEngineMu.Lock()
	defaultEng = e
	defaultEngineMu.Unlock()
}

func defaultEngine() *engine {
	defaultEngineMu.RLock()
	defer defaultEngineMu.RUnlock()
	return defaultEng
}

// newEngine builds the production engine from the wired NAS registry.
func newEngine(rdb *redis.Client, log *slog.Logger, reg *nasRegistry) *engine {
	return &engine{
		rdb:      rdb,
		log:      log,
		now:      time.Now,
		decrypt:  crypto.Decrypt,
		nasKnown: reg.known,
	}
}

// --- decision event stream (FR-39 groundwork; contract handoff) ------------

const decisionStream = "radius:decisions"
const decisionStreamMaxLen = 5000

// nasSeenAuthPrefix keys the last-Access-Request timestamp per NAS IP, read by
// the FR-14.4 "seen since created" status endpoint. Accounting (C) writes the
// parallel nas:seen:acct:<ip> key.
const nasSeenAuthPrefix = "nas:seen:auth:"

// markSeen records that a NAS source IP just sent an Access-Request. Best-effort.
func (e *engine) markSeen(ctx context.Context, ip string) {
	if e.rdb == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = e.rdb.Set(wctx, nasSeenAuthPrefix+canonicalIP(ip), e.now().UTC().Format(time.RFC3339), 0).Err()
}

// decisionEvent is one authorize attempt, recorded to a capped Redis stream so
// Phase 3's RADIUS debug tool (FR-39) can replay recent decisions.
type decisionEvent struct {
	Username string   `json:"username"`
	Service  string   `json:"service"`
	NASIP    string   `json:"nas_ip"`
	Outcome  string   `json:"outcome"` // accept | reject
	Reason   string   `json:"reason"`
	Checks   []string `json:"checks"` // ordered trace of stages passed
	At       string   `json:"at"`
}

// record writes the decision to the capped stream, best-effort. A logging
// failure must never change the auth outcome.
func (e *engine) record(ctx context.Context, ev decisionEvent) {
	if e.rdb == nil {
		return
	}
	ev.At = e.now().UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// Detach from the request context so a client disconnect can't drop the
	// audit trail write.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := e.rdb.XAdd(wctx, &redis.XAddArgs{
		Stream: decisionStream,
		MaxLen: decisionStreamMaxLen,
		Approx: true,
		Values: map[string]any{"event": payload},
	}).Err(); err != nil {
		e.log.Debug("radius decision stream write failed", "error", err)
	}
}
