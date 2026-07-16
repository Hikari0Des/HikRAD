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
	"github.com/jackc/pgx/v5/pgxpool"
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
	// nasByIP returns the enabled NAS registered at this source IP. The
	// authorize path needs the row itself from v2 phase 1 on: its id is what an
	// FR-64 scope is compared against, and its vendor selects the adapter that
	// resolves the request's service instance (C7). Stubbed in tests.
	nasByIP func(ctx context.Context, ip string) (nasIdentity, bool, error)
	// servicesOf returns a NAS's enabled service instances — the candidate set
	// for C7 resolution. Stubbed in tests.
	servicesOf func(ctx context.Context, nasID string) ([]serviceRow, error)
	// sink receives every finished decision (FR-39). Production writes the capped
	// Redis stream the debug tail reads; a nil sink drops the event, which is the
	// contract everywhere: recording must never change an auth outcome.
	sink func(ctx context.Context, ev decisionEvent)
}

// nasIdentity is the slice of a NAS the authorize path needs: enough to scope
// and to pick a vendor adapter, and nothing sensitive (no secret).
type nasIdentity struct {
	ID     string
	Vendor string
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
func newEngine(rdb *redis.Client, log *slog.Logger, reg *nasRegistry, db *pgxpool.Pool) *engine {
	e := &engine{
		rdb:      rdb,
		log:      log,
		now:      time.Now,
		decrypt:  crypto.Decrypt,
		nasKnown: reg.known,
		nasByIP:  reg.lookup,
		servicesOf: func(ctx context.Context, nasID string) ([]serviceRow, error) {
			return enabledServices(ctx, db, nasID)
		},
	}
	e.sink = e.writeDecision
	return e
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
	// Instance is the resolved nas_services label/name (FR-62), so an operator
	// can see WHICH of a multi-service NAS's zones a request landed on.
	Instance string `json:"instance,omitempty"`
	// Attributes is the accept's reply intent set — what HikRAD actually told
	// the router to do. Recorded because an accept is not the same as a working
	// login: the router still has to honour the reply, and when it can't
	// ("no address from ip pool" = HikRAD named an address_pool that does not
	// exist as an /ip pool on that router) the reply is the only place the
	// mismatch is visible. Reject events carry none.
	//
	// Safe to record: these are vendor-neutral intents, never a credential.
	Attributes []attribute `json:"attributes,omitempty"`
	At         string      `json:"at"`
}

// record stamps the decision and hands it to the sink. A nil sink drops it.
func (e *engine) record(ctx context.Context, ev decisionEvent) {
	ev.At = e.now().UTC().Format(time.RFC3339Nano)
	if e.sink == nil {
		return
	}
	e.sink(ctx, ev)
}

// writeDecision is the production sink: the capped stream, best-effort. A
// logging failure must never change the auth outcome.
func (e *engine) writeDecision(ctx context.Context, ev decisionEvent) {
	if e.rdb == nil {
		return
	}
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
