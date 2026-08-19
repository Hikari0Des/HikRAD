package radius

// DB-backed CoA outbox/dispatcher suite (gated on HIKRAD_TEST_DB_URL +
// HIKRAD_TEST_REDIS_URL, matching the repo pattern — see db_phase4_test.go).
// Exercises the real coa_outbox/coa_attempts tables and the real dispatcher
// tick loop end to end; only the UDP round trip is faked (coaService.exchange
// is already an injected seam, same one coa_test.go uses), so this proves the
// actual state-machine transitions (gate items 2-8) without a real NAS.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/hikrad/hikrad/internal/platform/crypto"
	"github.com/hikrad/hikrad/internal/radius/vendor"
	"layeh.com/radius"
)

func coaOutboxTestModule(t *testing.T) *module {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL/HIKRAD_TEST_REDIS_URL not set; skipping coa outbox DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 41)
	}
	if err := crypto.Configure(key); err != nil {
		t.Fatalf("configure crypto: %v", err)
	}
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(db.Close)
	rdb, err := platform.NewRedis(ctx, platform.Config{RedisURL: redisURL})
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	m := &module{db: db, rdb: rdb, log: log}
	m.nas = newNASRegistry(db, log)
	return m
}

// fakeExchangeNAKThenACK NAKs the first `naks` calls then ACKs; matches the
// harness's -coa-nak posture but driven deterministically for the test.
func fakeExchangeNAKThenACK(naks int) func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
	calls := 0
	return func(_ context.Context, p *radius.Packet, _ string) (*radius.Packet, error) {
		calls++
		ackCode, nakCode := radius.CodeCoAACK, radius.CodeCoANAK
		if p.Code == radius.CodeDisconnectRequest {
			ackCode, nakCode = radius.CodeDisconnectACK, radius.CodeDisconnectNAK
		}
		if calls <= naks {
			return reply(nakCode), nil
		}
		return reply(ackCode), nil
	}
}

func fakeExchangeAlwaysNAK() func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
	return func(_ context.Context, p *radius.Packet, _ string) (*radius.Packet, error) {
		nakCode := radius.CodeCoANAK
		if p.Code == radius.CodeDisconnectRequest {
			nakCode = radius.CodeDisconnectNAK
		}
		return reply(nakCode), nil
	}
}

func mustInsertTestNAS(t *testing.T, m *module) nasRow {
	t.Helper()
	return mustInsertNAS(t, m, "", "", "")
}

func runTicks(t *testing.T, m *module, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		m.coaDispatchTick(context.Background())
	}
}

// TestCoAOutbox_ReaperInteraction_ResolvesDone proves gate item 8: a
// disconnect enqueued for a session the reaper has already closed (no live
// state entry) resolves `done`, never `failed`.
func TestCoAOutbox_ReaperInteraction_ResolvesDone(t *testing.T) {
	m := coaOutboxTestModule(t)
	m.coa = &coaService{db: m.db, rdb: m.rdb, log: m.log, now: time.Now,
		exchange: fakeExchangeAlwaysNAK(), inflight: make(chan struct{}, 8)}
	setDefaultCoA(m.coa)
	n := mustInsertTestNAS(t, m)

	// No livestate.Upsert call — the session is not live (already reaped).
	id, err := insertOutbox(context.Background(), m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: "reaped-sess", Username: "ghost",
		Action: CoAActionDisconnect, Reason: "test.reaper",
	})
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	runTicks(t, m, 1)

	row, err := getOutbox(context.Background(), m.db, id)
	if err != nil {
		t.Fatalf("get outbox: %v", err)
	}
	if row.State != OutboxDone {
		t.Fatalf("state = %q, want done", row.State)
	}
	if row.LastOutcome != "skipped_reaped" {
		t.Fatalf("last_outcome = %q, want skipped_reaped", row.LastOutcome)
	}
}

// TestCoAOutbox_HybridFallback_NACKsThenFallsBackToDisconnect proves gate
// items 3/4: CoA-Update NAKed past the retry limit automatically falls back
// to Disconnect-Request, and the row visibly records that it happened.
func TestCoAOutbox_HybridFallback_NACKsThenFallsBackToDisconnect(t *testing.T) {
	m := coaOutboxTestModule(t)
	m.coa = &coaService{db: m.db, rdb: m.rdb, log: m.log, now: time.Now,
		exchange: fakeExchangeAlwaysNAK(), inflight: make(chan struct{}, 8)}
	setDefaultCoA(m.coa)
	n := mustInsertTestNAS(t, m)

	sess := livestate.State{
		Username: "u1", SubscriberID: "sub-1", NASID: n.ID, AcctSessionID: "live-sess",
		StartedAt: time.Now(), LastInterimAt: time.Now(),
	}
	if err := livestate.Upsert(context.Background(), m.rdb, sess); err != nil {
		t.Fatalf("seed live state: %v", err)
	}

	id, err := insertOutbox(context.Background(), m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: sess.AcctSessionID, Username: sess.Username,
		Action: CoAActionUpdate, Attributes: []vendor.Attr{{Intent: vendor.IntentRateLimit, Value: "1M/1M"}},
		Reason: "test.fallback", SubscriberID: sess.SubscriberID,
	})
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}

	// coa_retry_max defaults to 3 (migration 0615's seed). Each tick: one
	// dispatch attempt (-> nack) then the sweep decides retry/fallback/fail.
	// Attempts 1-3 NAK and retry; the 4th tick's sweep (after the 3rd nack)
	// triggers the fallback; a further tick dispatches the disconnect (still
	// NAKing, since fakeExchangeAlwaysNAK covers both codes) until IT also
	// exhausts its own retry budget and the row fails terminally. Run enough
	// ticks to reach that terminal state either way.
	for i := 0; i < 20; i++ {
		row, err := getOutbox(context.Background(), m.db, id)
		if err != nil {
			t.Fatalf("get outbox: %v", err)
		}
		if row.State == OutboxFailed {
			break
		}
		m.coaDispatchTick(context.Background())
	}

	row, err := getOutbox(context.Background(), m.db, id)
	if err != nil {
		t.Fatalf("get outbox: %v", err)
	}
	if !row.FallbackUsed {
		t.Fatalf("expected fallback_used=true, row=%+v", row)
	}
	if row.FallbackReason == "" {
		t.Fatalf("expected a non-empty fallback_reason (visible in the row, gate item 4)")
	}
	if row.CurrentOp != CoAActionDisconnect {
		t.Fatalf("current_op = %q, want disconnect after fallback", row.CurrentOp)
	}
	if row.Action != CoAActionUpdate {
		t.Fatalf("action = %q, want the ORIGINAL update (frozen: action never changes)", row.Action)
	}

	attempts, err := listAttempts(context.Background(), m.db, id)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	sawUpdate, sawDisconnect := false, false
	for _, a := range attempts {
		if a.Op == CoAActionUpdate {
			sawUpdate = true
		}
		if a.Op == CoAActionDisconnect {
			sawDisconnect = true
			if len(a.WireAttributes) == 0 {
				t.Errorf("disconnect attempt has no captured wire attributes")
			}
		}
	}
	if !sawUpdate || !sawDisconnect {
		t.Fatalf("expected both update and disconnect attempts recorded, got %+v", attempts)
	}
}

// TestCoAOutbox_RetryAndCancel proves gate item 5.
func TestCoAOutbox_RetryAndCancel(t *testing.T) {
	m := coaOutboxTestModule(t)
	n := mustInsertTestNAS(t, m)
	ctx := context.Background()

	// Cancel: only an unsent (pending) row can be cancelled.
	pendingID, err := insertOutbox(ctx, m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: "s1", Username: "u1", Action: CoAActionDisconnect, Reason: "t",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	ok, err := cancelOutboxRow(ctx, m.db, pendingID)
	if err != nil || !ok {
		t.Fatalf("cancel pending row: ok=%v err=%v", ok, err)
	}
	row, _ := getOutbox(ctx, m.db, pendingID)
	if row.State != OutboxFailed {
		t.Fatalf("cancelled row state = %q, want failed", row.State)
	}

	// A row already resolved `done` cannot be cancelled.
	doneID, err := insertOutbox(ctx, m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: "s2", Username: "u2", Action: CoAActionDisconnect, Reason: "t",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := resolveDone(ctx, m.db, doneID, "ack"); err != nil {
		t.Fatalf("resolve done: %v", err)
	}
	ok, err = cancelOutboxRow(ctx, m.db, doneID)
	if err != nil || ok {
		t.Fatalf("cancel of a done row should refuse: ok=%v err=%v", ok, err)
	}

	// Retry: re-queues a failed row from scratch.
	failedID, err := insertOutbox(ctx, m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: "s3", Username: "u3", Action: CoAActionDisconnect, Reason: "t",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := m.db.Exec(ctx, `UPDATE coa_outbox SET state = 'failed', attempt_count = 5 WHERE id = $1::uuid`, failedID); err != nil {
		t.Fatalf("force failed: %v", err)
	}
	ok, err = retryOutboxRow(ctx, m.db, failedID)
	if err != nil || !ok {
		t.Fatalf("retry failed row: ok=%v err=%v", ok, err)
	}
	row, _ = getOutbox(ctx, m.db, failedID)
	if row.State != OutboxPending || row.AttemptCount != 0 {
		t.Fatalf("retried row = %+v, want pending/attempt_count=0", row)
	}
}

// TestCoAOutbox_Counters proves gate item 7: the six counters reconcile
// against the table.
func TestCoAOutbox_Counters(t *testing.T) {
	m := coaOutboxTestModule(t)
	n := mustInsertTestNAS(t, m)
	ctx := context.Background()

	before, err := outboxCounters(ctx, m.db)
	if err != nil {
		t.Fatalf("counters: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := insertOutbox(ctx, m.db, outboxInsert{
			NASID: n.ID, AcctSessionID: "cnt-sess", Username: "u", Action: CoAActionDisconnect, Reason: "t",
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	after, err := outboxCounters(ctx, m.db)
	if err != nil {
		t.Fatalf("counters: %v", err)
	}
	if after.Pending != before.Pending+3 {
		t.Fatalf("pending = %d, want %d", after.Pending, before.Pending+3)
	}
	if after.Total != before.Total+3 {
		t.Fatalf("total = %d, want %d", after.Total, before.Total+3)
	}
	sum := after.Pending + after.Sent + after.Nack + after.Done + after.Failed
	if sum != after.Total {
		t.Fatalf("counters do not reconcile: pending+sent+nack+done+failed=%d, total=%d", sum, after.Total)
	}
}

// TestCoAOutbox_ReclaimStuckSent proves gate item 2: a row a dead process
// left `sent` is reclaimed back to `pending` (never lost, never stuck).
func TestCoAOutbox_ReclaimStuckSent(t *testing.T) {
	m := coaOutboxTestModule(t)
	n := mustInsertTestNAS(t, m)
	ctx := context.Background()

	id, err := insertOutbox(ctx, m.db, outboxInsert{
		NASID: n.ID, AcctSessionID: "stuck-sess", Username: "u", Action: CoAActionDisconnect, Reason: "t",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := m.db.Exec(ctx,
		`UPDATE coa_outbox SET state = 'sent', sent_at = now() - interval '5 minutes' WHERE id = $1::uuid`, id); err != nil {
		t.Fatalf("force stuck sent: %v", err)
	}

	n2, err := reclaimStuckSent(ctx, m.db, 30*time.Second)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if n2 < 1 {
		t.Fatalf("expected at least 1 row reclaimed, got %d", n2)
	}
	row, err := getOutbox(ctx, m.db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.State != OutboxPending {
		t.Fatalf("state = %q, want pending", row.State)
	}
}
