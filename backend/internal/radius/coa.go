package radius

// CoA / Disconnect low-level exchange (contract C5 / FR-15). As of v4-8
// (FR-150/151) this service is no longer called directly by anything outside
// this package: coa_enqueue.go's Enqueue is the only way any caller sends a
// CoA, and coa_dispatcher.go is the only caller of send() below — the actual
// UDP round trip, still exactly this shape (per-NAS secret, coa_port, a
// timeout + one safety retry per attempt). What changed in v4-8 is who
// decides WHEN to attempt and what happens after a NAK: that state machine
// now lives entirely in coa_dispatcher.go / coa_outbox_store.go, durable
// across a restart, instead of being decided inline by whoever wanted a CoA
// sent.
//
// coaTimeout/coaRetries below are exchangeWithRetry's per-attempt safety net
// (one retry within a single dispatch attempt, distinct from the dispatcher's
// own row-level retry budget which spans separate worker ticks) — the
// timeout is now sourced from the coa_timeout_seconds hot-reloadable setting
// via timeoutDuration, falling back to the old hardcoded default when no
// Settings service is wired (unit tests).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

// SessionRef identifies the live session a CoA/Disconnect targets (FR-15.1).
type SessionRef struct {
	NASID         string
	AcctSessionID string
	Username      string
	FramedIP      string
	// Service ("pppoe"|"hotspot") keys the FR-15.4 ROS quirk matrix. Until v2
	// phase 1 this was read off nas.type — sound only while a NAS ran exactly
	// one service. A multi-service NAS (FR-62) makes the session's own service
	// the only accurate answer, so callers pass it from the live session state.
	// Empty falls back to "pppoe", the quirk matrix's v1 default.
	Service string
}

func (r SessionRef) service() string {
	if r.Service == "" {
		return "pppoe"
	}
	return r.Service
}

// CoAOutcome is the typed result of a CoA/Disconnect attempt.
type CoAOutcome string

const (
	CoAACK         CoAOutcome = "ack"
	CoANAK         CoAOutcome = "nak"
	CoATimeout     CoAOutcome = "timeout"
	CoAError       CoAOutcome = "error"
	CoAUnsupported CoAOutcome = "unsupported" // ROS-matrix quirk: known not to work in-place, no packet sent
)

// CoAResult is what every CoA operation returns; Err is non-nil for NAK/timeout
// so `if err != nil` fallbacks are natural, while Outcome distinguishes them.
type CoAResult struct {
	Outcome CoAOutcome
	Err     error
}

func (r CoAResult) Ok() bool { return r.Outcome == CoAACK }

const (
	// coaTimeout is the fallback used only when no Settings service is wired
	// (coaService built directly in unit tests); production reads
	// coa_timeout_seconds via timeoutDuration below.
	coaTimeout = 5 * time.Second
	coaRetries = 1
	coaNATNote = "CoA to a NAS behind NAT is unsupported in v1 (reply cannot route back)"
	// coaRetryMaxSettingKey/coaTimeoutSettingKey are the FR-151 "existing
	// hot-reloadable settings" (seeded by migration 0615) governing the
	// dispatcher's row-level retry budget and each attempt's UDP timeout.
	// coa_port stays per-NAS (nas.coa_port, already existing) — a retry/
	// timeout budget is process-wide but a CoA port is a per-router fact.
	coaRetryMaxSettingKey = "radius.coa_retry_max"
	coaTimeoutSettingKey  = "radius.coa_timeout_seconds"
	defaultCoARetryMax    = 3
	// coaMaxInflight bounds concurrent in-flight CoA/Disconnect exchanges
	// across the whole process (storm safety): the enforcement worker can
	// fan out a burst of moves in parallel (e.g. a midnight expiry sweep
	// crossing hundreds of subscribers at once) — without a cap that burst
	// opens one UDP socket + goroutine per session simultaneously, which
	// self-inflicts the packet loss/timeouts the retry logic exists to
	// tolerate. A blocked acquire simply queues; nothing is dropped.
	coaMaxInflight = 64
)

// CoAMetricsKey is the Redis hash of "<op>:<outcome>" -> count, incremented on
// every CoA/Disconnect attempt (frozen key name; C's health page reads it,
// mirroring EnforcementFailuresKey's contract).
const CoAMetricsKey = "coa:metrics"

type coaService struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	log      *slog.Logger
	settings platform.Settings
	// exchange is the packet round-trip, seam-injectable for tests.
	exchange func(ctx context.Context, p *radius.Packet, addr string) (*radius.Packet, error)
	now      func() time.Time
	inflight chan struct{}
}

func newCoAService(db *pgxpool.Pool, rdb *redis.Client, log *slog.Logger, settings platform.Settings) *coaService {
	return &coaService{
		db: db, rdb: rdb, log: log, settings: settings, exchange: radius.Exchange, now: time.Now,
		inflight: make(chan struct{}, coaMaxInflight),
	}
}

// timeoutDuration reads coa_timeout_seconds (hot-reloadable, FR-151), falling
// back to the old hardcoded default when unset or when built without a
// Settings service (unit tests construct coaService directly).
func (c *coaService) timeoutDuration(ctx context.Context) time.Duration {
	if c.settings == nil {
		return coaTimeout
	}
	secs, err := platform.Get[int](ctx, c.settings, coaTimeoutSettingKey)
	if err != nil || secs <= 0 {
		return coaTimeout
	}
	return time.Duration(secs) * time.Second
}

// retryMax reads coa_retry_max (hot-reloadable, FR-151) — the dispatcher's
// row-level retry budget (coa_dispatcher.go), distinct from exchangeWithRetry's
// own single in-attempt safety retry (coaRetries).
func (c *coaService) retryMax(ctx context.Context) int {
	if c.settings == nil {
		return defaultCoARetryMax
	}
	n, err := platform.Get[int](ctx, c.settings, coaRetryMaxSettingKey)
	if err != nil || n <= 0 {
		return defaultCoARetryMax
	}
	return n
}

var (
	defaultCoAMu sync.RWMutex
	defaultCoA   *coaService
)

func setDefaultCoA(c *coaService) {
	defaultCoAMu.Lock()
	defaultCoA = c
	defaultCoAMu.Unlock()
}

func currentCoA() *coaService {
	defaultCoAMu.RLock()
	defer defaultCoAMu.RUnlock()
	return defaultCoA
}

// send builds and dispatches ONE CoA/Disconnect exchange attempt against
// ref's NAS, returning the typed outcome plus the exact wire-level attributes
// placed on the outgoing packet (dumpWireAttributes, captured before the
// exchange so it reflects what was sent regardless of outcome) — the
// evidence coa_dispatcher.go persists into coa_attempts.wire_attributes for
// the FR-152 inspect view (gate item 6). This is the package's ONLY sender;
// as of v4-8 it is called from exactly one place, coa_dispatcher.go's
// worker-tick loop — every other caller enqueues via coa_enqueue.go.
func (c *coaService) send(ctx context.Context, ref SessionRef, code radius.Code, attrs []vendor.Attr) (CoAResult, []WireAttr) {
	n, err := getNAS(ctx, c.db, ref.NASID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: nas %s not found", ref.NASID)}, nil
	}
	if err != nil {
		return CoAResult{Outcome: CoAError, Err: err}, nil
	}

	// Version-aware short-circuit (FR-15.4, ROS quirk matrix): skip the
	// round trip entirely for an intent this NAS's ROS version/type is known
	// not to honor in-place, instead of burning a timeout+retry on a change
	// that will only ever NAK or hang.
	adapter := vendor.For(n.Vendor)
	ros := ""
	if n.ROSVersion != nil {
		ros = *n.ROSVersion
	}
	for _, a := range attrs {
		if !adapter.SupportsInPlace(ros, ref.service(), a.Intent) {
			res := CoAResult{Outcome: CoAUnsupported, Err: fmt.Errorf(
				"coa: %s unsupported in-place on %s ROS %q %s session — falling back", a.Intent, n.Vendor, ros, ref.service())}
			c.log.Info("coa attempt skipped (version-aware quirk)",
				"nas", ref.NASID, "op", codeName(code), "intent", a.Intent, "ros_version", ros, "service", ref.service())
			c.recordMetric(ctx, codeName(code), res.Outcome)
			return res, nil
		}
	}

	secret, err := decryptToString(n.SecretEnc)
	if err != nil {
		return CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: decrypt secret: %w", err)}, nil
	}

	pkt := radius.New(code, []byte(secret))
	if ref.Username != "" {
		_ = rfc2865.UserName_SetString(pkt, ref.Username)
	}
	if ref.AcctSessionID != "" {
		_ = rfc2866.AcctSessionID_SetString(pkt, ref.AcctSessionID)
	}
	if ip := net.ParseIP(ref.FramedIP); ip != nil {
		_ = rfc2865.FramedIPAddress_Set(pkt, ip)
	}
	if len(attrs) > 0 {
		if err := adapter.Apply(pkt, attrs); err != nil {
			return CoAResult{Outcome: CoAError, Err: err}, nil
		}
	}

	// Captured now — the packet is fully built, nothing after this point
	// changes what is on the wire — so the FR-150 §4 "stored as sent, never
	// regenerated" evidence exists regardless of what the exchange returns.
	wire := dumpWireAttributes(pkt)

	addr := net.JoinHostPort(n.IP, itoa(n.CoAPort))

	// Storm safety: bound concurrent in-flight exchanges process-wide. A full
	// queue waits rather than drops; ctx's own deadline still applies while
	// waiting so a caller with a tight budget fails fast instead of hanging
	// forever behind the cap. (inflight is nil only for a coaService built
	// directly by unit tests, which skip the cap entirely.)
	if c.inflight != nil {
		select {
		case c.inflight <- struct{}{}:
			defer func() { <-c.inflight }()
		case <-ctx.Done():
			res := CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: %w waiting for an inflight slot", ctx.Err())}
			c.recordMetric(ctx, codeName(code), res.Outcome)
			return res, wire
		}
	}

	res := c.exchangeWithRetry(ctx, pkt, addr, code)
	c.log.Info("coa attempt",
		"nas", ref.NASID, "op", codeName(code), "outcome", res.Outcome,
		"username", ref.Username, "acct_session_id", ref.AcctSessionID, "error", res.Err)
	c.recordMetric(ctx, codeName(code), res.Outcome)
	return res, wire
}

// recordMetric increments the CoAMetricsKey hash field "<op>:<outcome>" —
// best-effort, on a context detached from ctx's deadline so a caller that hit
// its own timeout right at the finish line doesn't also lose the metric
// (mirrors auditCoA's WithoutCancel rationale).
func (c *coaService) recordMetric(ctx context.Context, op string, outcome CoAOutcome) {
	if c.rdb == nil {
		return
	}
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = c.rdb.HIncrBy(mctx, CoAMetricsKey, op+":"+string(outcome), 1).Err()
}

// CoAMetrics reads the per-operation/result counters (C's health page). A nil
// Redis client (unit context) returns an empty map, not an error.
func CoAMetrics(ctx context.Context) (map[string]int64, error) {
	c := currentCoA()
	if c == nil || c.rdb == nil {
		return map[string]int64{}, nil
	}
	raw, err := c.rdb.HGetAll(ctx, CoAMetricsKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(raw))
	for k, v := range raw {
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			out[k] = n
		}
	}
	return out, nil
}

func (c *coaService) exchangeWithRetry(ctx context.Context, pkt *radius.Packet, addr string, code radius.Code) CoAResult {
	var last CoAResult
	timeout := c.timeoutDuration(ctx)
	for attempt := 0; attempt <= coaRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err := c.exchange(attemptCtx, pkt, addr)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				last = CoAResult{Outcome: CoATimeout, Err: fmt.Errorf("coa: timeout to %s (%s)", addr, coaNATNote)}
				continue
			}
			last = CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: exchange: %w", err)}
			continue
		}
		switch resp.Code {
		case radius.CodeDisconnectACK, radius.CodeCoAACK:
			return CoAResult{Outcome: CoAACK}
		case radius.CodeDisconnectNAK, radius.CodeCoANAK:
			return CoAResult{Outcome: CoANAK, Err: fmt.Errorf("coa: %s NAK from %s", codeName(code), addr)}
		default:
			last = CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: unexpected reply code %v", resp.Code)}
		}
	}
	return last
}

func codeName(code radius.Code) string {
	switch code {
	case radius.CodeDisconnectRequest:
		return "disconnect"
	case radius.CodeCoARequest:
		return "coa"
	default:
		return "unknown"
	}
}

// itoa avoids importing strconv across the package for a single small int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
