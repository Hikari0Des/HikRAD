package accounting

// DB + Redis-gated pipeline/chaos suite (FR-37.5, NFR-2), gated on
// HIKRAD_TEST_DB_URL / HIKRAD_TEST_REDIS_URL exactly like the other suites in
// this repo; skipped when unset. These assert the FR-40 conservation invariant
// through the scenarios the brief names: a flood drains with zero loss, a
// retransmit storm dedups, out-of-order interims land by event time, the disk
// spill replays losslessly, an acct restart resumes a backlog, and the reaper
// lifecycle runs. The full kill-Postgres-container orchestration is gate item 3
// (run against the compose stack); here the same guarantees are exercised at the
// code level, with DB-down modelled by processing a backlog after the fact.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func migrateForTest(dbURL string, log *slog.Logger) error {
	return platform.Migrate(dbURL, "../../migrations", log)
}

func testEnv(t *testing.T) (*Service, context.Context, func()) {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	redisURL := os.Getenv("HIKRAD_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL / HIKRAD_TEST_REDIS_URL not set; skipping pipeline suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := migrateForTest(dbURL, log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	ropts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	rdb := redis.NewClient(ropts)

	// Clean only the pipeline's own Redis keys (never FLUSHALL — the CI Redis is
	// shared with other suites). The stream + group are reset so each test drains
	// a known set.
	rdb.Del(ctx, streamKey)
	cleanLiveKeys(ctx, rdb)

	svc, cleanup, err := New(ctx, Config{SpillDir: t.TempDir(), InterimInterval: 60 * time.Second}, db, rdb, log)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if err := ensureGroup(ctx, rdb); err != nil {
		t.Fatalf("group: %v", err)
	}
	done := func() {
		cleanup()
		rdb.Del(ctx, streamKey)
		cleanLiveKeys(ctx, rdb)
		_ = rdb.Close()
		db.Close()
	}
	return svc, ctx, done
}

func cleanLiveKeys(ctx context.Context, rdb *redis.Client) {
	rdb.Del(ctx, livestate.HashKey)
	for _, pat := range []string{"live:sub:*", "live:nas:*", "quota:exhausted:*"} {
		iter := rdb.Scan(ctx, 0, pat, 1000).Iterator()
		for iter.Next(ctx) {
			rdb.Del(ctx, iter.Val())
		}
	}
}

func runID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// testIP returns a random 10.x.x.x address so each test run gets a distinct NAS
// id — the shared CI DB keeps rows across runs, so per-run isolation of
// sessions/usage_points relies on a unique nas_id (and unique usernames).
func testIP() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("10.%d.%d.%d", b[0], b[1], 1+int(b[2])%254)
}

// drainAll synchronously processes every currently-queued stream entry, acking
// as the consumer would. Deterministic replacement for runConsumer in tests.
func drainAll(t *testing.T, ctx context.Context, s *Service) {
	t.Helper()
	for {
		msgs, err := s.read(ctx, ">")
		if err == redis.Nil {
			return
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(msgs) == 0 {
			return
		}
		for _, m := range msgs {
			ack, perr := s.processMessage(ctx, m)
			if perr != nil {
				t.Fatalf("process: %v", perr)
			}
			if ack {
				s.ackDelete(ctx, m.ID)
			}
		}
	}
}

// post drives one accounting record through the real ingest HTTP handler so the
// received/enqueued counters and the durability contract are exercised.
func post(t *testing.T, s *Service, rec map[string]any) {
	t.Helper()
	body, _ := json.Marshal(rec)
	req := httptest.NewRequest(http.MethodPost, "/acct", bytes.NewReader(body))
	rw := httptest.NewRecorder()
	s.ingestHandler(rw, req)
	if rw.Code != http.StatusNoContent {
		t.Fatalf("ingest status %d (want 204): %s", rw.Code, rw.Body.String())
	}
}

// insertTestNAS registers a NAS running one service instance of kind typ. Since
// v2 phase 1 the service lives on nas_services, not a nas.type column (FR-62).
func insertTestNAS(t *testing.T, ctx context.Context, s *Service, ip, typ string) string {
	t.Helper()
	var id string
	err := s.db.QueryRow(ctx,
		`INSERT INTO nas (name, ip, secret_enc) VALUES ($1, $2::inet, $3)
		 ON CONFLICT (ip) DO UPDATE SET name = EXCLUDED.name RETURNING id::text`,
		"test-"+ip, ip, []byte("x")).Scan(&id)
	if err != nil {
		t.Fatalf("insert nas: %v", err)
	}
	if _, err := s.db.Exec(ctx,
		`INSERT INTO nas_services (nas_id, service, label, enabled)
		 SELECT $1::uuid, $2, 'test', true
		  WHERE NOT EXISTS (SELECT 1 FROM nas_services WHERE nas_id = $1::uuid AND service = $2)`,
		id, typ); err != nil {
		t.Fatalf("insert nas service: %v", err)
	}
	return id
}

func insertTestSubscriber(t *testing.T, ctx context.Context, s *Service, username string) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(ctx,
		`INSERT INTO subscribers (username) VALUES ($1) RETURNING id::text`, username).Scan(&id); err != nil {
		t.Fatalf("insert subscriber: %v", err)
	}
	return id
}

func evTime(base time.Time, secs int) string {
	return fmt.Sprintf("%d", base.Add(time.Duration(secs)*time.Second).Unix())
}

// --- Scenarios -------------------------------------------------------------

func TestPipelineFloodNoLoss(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "hotspot")
	sub := insertTestSubscriber(t, ctx, s, "flood-"+rid)
	acct := "sess-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	before := s.counters.snapshot(0)

	post(t, s, rec(nasID, ip, acct, "flood-"+rid, "start", base, 0, 0, 0, 0))
	post(t, s, rec(nasID, ip, acct, "flood-"+rid, "interim", base, 30, 1000, 2000, 0))
	drainAll(t, ctx, s)

	// Start→visible: the live hash carries the session, tagged hotspot (FR-58).
	if got := livestate.Count(ctx, s.rdb, sub, livestate.ServiceHotspot); got != 1 {
		t.Fatalf("live hotspot count after interim: got %d want 1", got)
	}
	if got := livestate.Count(ctx, s.rdb, sub, livestate.ServicePPPoE); got != 0 {
		t.Fatalf("pppoe count should be 0 for a hotspot session: got %d", got)
	}

	post(t, s, rec(nasID, ip, acct, "flood-"+rid, "stop", base, 60, 1500, 3000, 0))
	drainAll(t, ctx, s)

	after := s.counters.snapshot(0)
	if got := after.Persisted - before.Persisted; got != 3 {
		t.Fatalf("persisted delta: got %d want 3", got)
	}
	if s.inQueue(ctx) != 0 {
		t.Fatalf("in_queue after drain: got %d want 0", s.inQueue(ctx))
	}
	if !s.counters.snapshot(s.inQueue(ctx)).InvariantOK {
		t.Fatal("counter invariant broken after flood")
	}

	// Session closed with correct totals; live row removed.
	var bin, bout int64
	var stopped *time.Time
	if err := s.db.QueryRow(ctx,
		`SELECT bytes_in, bytes_out, stopped_at FROM sessions WHERE nas_id=$1 AND acct_session_id=$2`,
		nasID, acct).Scan(&bin, &bout, &stopped); err != nil {
		t.Fatalf("session read: %v", err)
	}
	if bin != 1500 || bout != 3000 || stopped == nil {
		t.Fatalf("final session: in=%d out=%d stopped=%v", bin, bout, stopped)
	}
	if livestate.Count(ctx, s.rdb, sub, "") != 0 {
		t.Fatal("live count should be 0 after Stop")
	}

	// Usage points: hotspot rows exist and sum to the deltas.
	var down, up int64
	var svc string
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(sum(delta_down),0), COALESCE(sum(delta_up),0), max(service)
		   FROM usage_points WHERE nas_id=$1 AND subscriber_id=$2`,
		nasID, sub).Scan(&down, &up, &svc); err != nil {
		t.Fatalf("usage read: %v", err)
	}
	if down != 3000 || up != 1500 {
		t.Fatalf("usage totals: down=%d up=%d want 3000/1500", down, up)
	}
	if svc != "hotspot" {
		t.Fatalf("usage service tag: got %q want hotspot", svc)
	}
}

func TestDedupStorm(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "pppoe")
	acct := "dup-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	before := s.counters.snapshot(0)
	// Each packet delivered 3× (NAS retransmits), same event_time each time.
	for i := 0; i < 3; i++ {
		post(t, s, rec(nasID, ip, acct, "dup-"+rid, "start", base, 0, 0, 0, 0))
		post(t, s, rec(nasID, ip, acct, "dup-"+rid, "interim", base, 30, 500, 700, 0))
		post(t, s, rec(nasID, ip, acct, "dup-"+rid, "stop", base, 60, 500, 700, 0))
	}
	drainAll(t, ctx, s)
	after := s.counters.snapshot(0)

	if got := after.Persisted - before.Persisted; got != 3 {
		t.Fatalf("persisted delta: got %d want 3 (dedup)", got)
	}
	if got := after.Deduplicated - before.Deduplicated; got != 6 {
		t.Fatalf("deduplicated delta: got %d want 6", got)
	}
	// Persisted usage equals single delivery (700 down, 500 up), not 3×: the
	// interim's delta is counted once despite three retransmits.
	var down, up int64
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(sum(delta_down),0), COALESCE(sum(delta_up),0) FROM usage_points WHERE nas_id=$1`,
		nasID).Scan(&down, &up); err != nil {
		t.Fatalf("usage read: %v", err)
	}
	if down != 700 || up != 500 {
		t.Fatalf("dedup usage totals: down=%d up=%d want 700/500", down, up)
	}
}

func TestOutOfOrderInterims(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "pppoe")
	acct := "ooo-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	post(t, s, rec(nasID, ip, acct, "ooo-"+rid, "start", base, 0, 0, 0, 0))
	// Deliver the later interim (t+60, higher counters) BEFORE the earlier one.
	post(t, s, rec(nasID, ip, acct, "ooo-"+rid, "interim", base, 60, 2000, 4000, 0))
	post(t, s, rec(nasID, ip, acct, "ooo-"+rid, "interim", base, 30, 1000, 2000, 0))
	drainAll(t, ctx, s)

	// Usage points are keyed by NAS event time, so both intervals exist and the
	// hypertable holds one row per distinct event time.
	var points int
	if err := s.db.QueryRow(ctx,
		`SELECT count(*) FROM usage_points WHERE nas_id=$1`, nasID).Scan(&points); err != nil {
		t.Fatalf("count usage points: %v", err)
	}
	if points < 2 {
		t.Fatalf("expected >=2 usage points for out-of-order interims, got %d", points)
	}
}

func TestSpillReplayNoLoss(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "pppoe")
	acct := "spill-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	before := s.counters.snapshot(0)
	// Model Redis-down at ingest: append straight to the spill WAL.
	for _, r := range []map[string]any{
		rec(nasID, ip, acct, "spill-"+rid, "start", base, 0, 0, 0, 0),
		rec(nasID, ip, acct, "spill-"+rid, "interim", base, 30, 800, 1600, 0),
		rec(nasID, ip, acct, "spill-"+rid, "stop", base, 60, 800, 1600, 0),
	} {
		body, _ := json.Marshal(mustRecord(t, r))
		if err := s.spill.append(body); err != nil {
			t.Fatal(err)
		}
		s.counters.spilled.Add(1)
	}
	if s.spill.pending() != 3 {
		t.Fatalf("pending spill: got %d want 3", s.spill.pending())
	}

	// Recovery: drain the WAL into the stream, then consume.
	drained, bad, err := s.spill.drain(func(p []byte) error {
		if _, e := enqueueStream(ctx, s.rdb, p); e != nil {
			return e
		}
		s.counters.drained.Add(1)
		return nil
	})
	if err != nil || bad != 0 || drained != 3 {
		t.Fatalf("spill drain: drained=%d bad=%d err=%v", drained, bad, err)
	}
	drainAll(t, ctx, s)

	after := s.counters.snapshot(0)
	if got := after.Persisted - before.Persisted; got != 3 {
		t.Fatalf("persisted after spill replay: got %d want 3", got)
	}
	if s.inQueue(ctx) != 0 {
		t.Fatalf("in_queue after replay: got %d", s.inQueue(ctx))
	}
}

func TestAcctRestartResumesBacklog(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "pppoe")
	acct := "restart-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	// Enqueue a backlog but do NOT consume (models acct killed mid-flood).
	post(t, s, rec(nasID, ip, acct, "restart-"+rid, "start", base, 0, 0, 0, 0))
	post(t, s, rec(nasID, ip, acct, "restart-"+rid, "interim", base, 30, 900, 900, 0))
	post(t, s, rec(nasID, ip, acct, "restart-"+rid, "stop", base, 60, 900, 900, 0))
	if s.inQueue(ctx) != 3 {
		t.Fatalf("backlog before restart: got %d want 3", s.inQueue(ctx))
	}

	// "Restart": a fresh consumer name drains the same group's backlog.
	s.consumerName = "restarted-" + rid
	drainAll(t, ctx, s)
	if s.inQueue(ctx) != 0 {
		t.Fatalf("backlog after restart drain: got %d", s.inQueue(ctx))
	}
	var stopped *time.Time
	if err := s.db.QueryRow(ctx,
		`SELECT stopped_at FROM sessions WHERE nas_id=$1 AND acct_session_id=$2`, nasID, acct).
		Scan(&stopped); err != nil {
		t.Fatalf("session read: %v", err)
	}
	if stopped == nil {
		t.Fatal("session should be closed after backlog drain")
	}
}

func TestReaperLifecycle(t *testing.T) {
	s, ctx, done := testEnv(t)
	defer done()

	rid := runID()
	ip := testIP()
	nasID := insertTestNAS(t, ctx, s, ip, "pppoe")
	sub := insertTestSubscriber(t, ctx, s, "reap-"+rid)
	acct := "reap-" + rid
	base := time.Now().Add(-time.Hour).UTC()

	post(t, s, rec(nasID, ip, acct, "reap-"+rid, "start", base, 0, 0, 0, 0))
	post(t, s, rec(nasID, ip, acct, "reap-"+rid, "interim", base, 30, 100, 100, 0))
	drainAll(t, ctx, s)

	// Force the last interim into the past so the session is silent.
	silent := time.Now().Add(-1 * time.Hour)
	if _, err := s.db.Exec(ctx,
		`UPDATE sessions SET last_interim_at=$3 WHERE nas_id=$1 AND acct_session_id=$2`,
		nasID, acct, silent); err != nil {
		t.Fatal(err)
	}

	// interim=60s → stale after 120s, reap after 3*60+300 = 480s. 1h past both.
	if err := s.reapOnce(ctx, time.Now()); err != nil {
		t.Fatalf("reapOnce: %v", err)
	}
	var stale, reaped bool
	var stopped *time.Time
	var cause string
	if err := s.db.QueryRow(ctx,
		`SELECT stale, reaped, stopped_at, terminate_cause FROM sessions WHERE nas_id=$1 AND acct_session_id=$2`,
		nasID, acct).Scan(&stale, &reaped, &stopped, &cause); err != nil {
		t.Fatal(err)
	}
	if !stale || !reaped || stopped == nil || cause != "Reaper-Timeout" {
		t.Fatalf("reaped session: stale=%v reaped=%v stopped=%v cause=%q", stale, reaped, stopped, cause)
	}
	if livestate.Count(ctx, s.rdb, sub, "") != 0 {
		t.Fatal("reaped session should be gone from live")
	}

	// A late real interim supersedes/reopens the reaped session (FR-38.2).
	post(t, s, rec(nasID, ip, acct, "reap-"+rid, "interim", time.Now().Add(-time.Minute), 120, 200, 200, 0))
	drainAll(t, ctx, s)
	if err := s.db.QueryRow(ctx,
		`SELECT stale, reaped, stopped_at FROM sessions WHERE nas_id=$1 AND acct_session_id=$2`,
		nasID, acct).Scan(&stale, &reaped, &stopped); err != nil {
		t.Fatal(err)
	}
	if stale || reaped || stopped != nil {
		t.Fatalf("late interim should reopen: stale=%v reaped=%v stopped=%v", stale, reaped, stopped)
	}
}

// rec builds a C6 ingest payload. bytes_in is upload, bytes_out is download.
func rec(nasID, ip, acct, user, typ string, base time.Time, secs int, bin, bout, gw uint64) map[string]any {
	return map[string]any{
		"record_type":        typ,
		"nas_ip":             ip,
		"acct_session_id":    acct,
		"username":           user,
		"framed_ip":          "100.64.0.1",
		"calling_station_id": "AA:BB:CC:DD:EE:FF",
		"session_time":       secs,
		"bytes_in":           bin,
		"bytes_out":          bout,
		"gigawords_in":       gw,
		"gigawords_out":      gw,
		"event_time":         evTime(base, secs),
	}
}

func mustRecord(t *testing.T, m map[string]any) Record {
	t.Helper()
	body, _ := json.Marshal(m)
	r, err := parseRecord(body, time.Now())
	if err != nil {
		t.Fatalf("parseRecord: %v", err)
	}
	return r
}
