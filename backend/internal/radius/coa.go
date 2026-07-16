package radius

// CoA / Disconnect service (contract C5 / FR-15). Exposes Disconnect,
// ApplyRate and MovePool as an in-process Go API consumed this phase by the
// panel disconnect button (E via C) and in Phase 3 by renewals (D). Every
// attempt sends UDP to the NAS's coa_port with its per-NAS secret, times out at
// 5 s with one retry, and returns a typed result so callers can fall back
// (FR-15.3/15.4). NAK/timeout do not fall back automatically here — the caller
// decides (renewal → Disconnect).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
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
	coaTimeout = 5 * time.Second
	coaRetries = 1
	coaNATNote = "CoA to a NAS behind NAT is unsupported in v1 (reply cannot route back)"
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
	db  *pgxpool.Pool
	rdb *redis.Client
	log *slog.Logger
	// exchange is the packet round-trip, seam-injectable for tests.
	exchange func(ctx context.Context, p *radius.Packet, addr string) (*radius.Packet, error)
	now      func() time.Time
	inflight chan struct{}
}

func newCoAService(db *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) *coaService {
	return &coaService{
		db: db, rdb: rdb, log: log, exchange: radius.Exchange, now: time.Now,
		inflight: make(chan struct{}, coaMaxInflight),
	}
}

// --- package-level C5 API (E-via-C this phase; D's renewals in Phase 3) -----
//
// Consumers call radius.Disconnect/ApplyRate/MovePool with a SessionRef; the
// result is audited with the Manager in ctx (C2). Wired at boot by Register.

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

// Disconnect ends a session (C5), auditing the result.
func Disconnect(ctx context.Context, ref SessionRef) CoAResult {
	c := currentCoA()
	if c == nil {
		return CoAResult{Outcome: CoAError, Err: errors.New("coa: service not configured")}
	}
	res := c.Disconnect(ctx, ref)
	auditCoA(ctx, "coa.disconnect", ref, "", res)
	return res
}

// ApplyRate changes a session's rate in place (C5), auditing the result.
func ApplyRate(ctx context.Context, ref SessionRef, rate string) CoAResult {
	c := currentCoA()
	if c == nil {
		return CoAResult{Outcome: CoAError, Err: errors.New("coa: service not configured")}
	}
	res := c.ApplyRate(ctx, ref, rate)
	auditCoA(ctx, "coa.apply_rate", ref, rate, res)
	return res
}

// MovePool re-assigns a session's pool (C5), auditing the result.
func MovePool(ctx context.Context, ref SessionRef, pool string) CoAResult {
	c := currentCoA()
	if c == nil {
		return CoAResult{Outcome: CoAError, Err: errors.New("coa: service not configured")}
	}
	res := c.MovePool(ctx, ref, pool)
	auditCoA(ctx, "coa.move_pool", ref, pool, res)
	return res
}

type coaAuditDetail struct {
	NASID         string `json:"nas_id"`
	AcctSessionID string `json:"acct_session_id"`
	Username      string `json:"username"`
	Param         string `json:"param,omitempty"`
	Outcome       string `json:"outcome"`
	Error         string `json:"error,omitempty"`
}

// auditCoA writes the audit entry on a context detached from ctx's deadline:
// a caller retrying a CoA exchange across several attempts (e.g. the
// enforcement worker's fixed per-cycle budget, FR-9/FR-10) can have already
// burned that deadline by the time a later attempt's result is ready to
// audit, which would otherwise drop the entry silently.
func auditCoA(ctx context.Context, action string, ref SessionRef, param string, res CoAResult) {
	d := coaAuditDetail{
		NASID: ref.NASID, AcctSessionID: ref.AcctSessionID, Username: ref.Username,
		Param: param, Outcome: string(res.Outcome),
	}
	if res.Err != nil {
		d.Error = res.Err.Error()
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = auth.Audit(actx, action, "session", ref.AcctSessionID, nil, d)
}

// Disconnect ends a session (FR-15.2). No vendor attributes needed — session
// identity attributes carry the target.
func (c *coaService) Disconnect(ctx context.Context, ref SessionRef) CoAResult {
	return c.send(ctx, ref, radius.CodeDisconnectRequest, nil)
}

// ApplyRate changes a session's rate in place (FR-15.4). Falls back to
// Disconnect at the caller when the NAS NAKs.
func (c *coaService) ApplyRate(ctx context.Context, ref SessionRef, rate string) CoAResult {
	return c.send(ctx, ref, radius.CodeCoARequest, []vendor.Attr{{Intent: vendor.IntentRateLimit, Value: rate}})
}

// MovePool re-assigns a session's address pool (FR-15.2, key flow 2 step 4).
func (c *coaService) MovePool(ctx context.Context, ref SessionRef, pool string) CoAResult {
	return c.send(ctx, ref, radius.CodeCoARequest, []vendor.Attr{{Intent: vendor.IntentAddressPool, Value: pool}})
}

func (c *coaService) send(ctx context.Context, ref SessionRef, code radius.Code, attrs []vendor.Attr) CoAResult {
	n, err := getNAS(ctx, c.db, ref.NASID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: nas %s not found", ref.NASID)}
	}
	if err != nil {
		return CoAResult{Outcome: CoAError, Err: err}
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
			return res
		}
	}

	secret, err := decryptToString(n.SecretEnc)
	if err != nil {
		return CoAResult{Outcome: CoAError, Err: fmt.Errorf("coa: decrypt secret: %w", err)}
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
			return CoAResult{Outcome: CoAError, Err: err}
		}
	}

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
			return res
		}
	}

	res := c.exchangeWithRetry(ctx, pkt, addr, code)
	c.log.Info("coa attempt",
		"nas", ref.NASID, "op", codeName(code), "outcome", res.Outcome,
		"username", ref.Username, "acct_session_id", ref.AcctSessionID, "error", res.Err)
	c.recordMetric(ctx, codeName(code), res.Outcome)
	return res
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
	for attempt := 0; attempt <= coaRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, coaTimeout)
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
