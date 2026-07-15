package live_test

// DB-backed suite for the Phase-4 usage-API polish (task 5, contract C7-C).
// No router/auth needed — usage_points carries a bare subscriber_id uuid with
// no FK, so a random id is enough to isolate rows on a shared CI database.

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/live"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping live usage DB suite")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(ctx, platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func insertUsagePoint(t *testing.T, db *pgxpool.Pool, subscriberID string, at time.Time, down, up int64) {
	t.Helper()
	_, err := db.Exec(context.Background(),
		`INSERT INTO usage_points (time, subscriber_id, delta_down, delta_up, service) VALUES ($1, $2::uuid, $3, $4, 'pppoe')`,
		at, subscriberID, down, up)
	if err != nil {
		t.Fatalf("insert usage_point: %v", err)
	}
}

// A subscriber with no usage yet must get an empty (non-nil) slice, never an
// error and never a bare `null` over the wire.
func TestUsageForSubscriber_EmptyHistory(t *testing.T) {
	db := testDB(t)
	sub := randomUUID()
	from := time.Now().Add(-30 * 24 * time.Hour)
	to := time.Now()
	out, err := live.UsageForSubscriber(context.Background(), db, sub, false, from, to)
	if err != nil {
		t.Fatalf("UsageForSubscriber: %v", err)
	}
	if out == nil {
		t.Fatal("expected a non-nil empty slice, got nil")
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 points, got %d", len(out))
	}
}

// Traffic in the last 3 hours of a Baghdad calendar day (which is still the
// PREVIOUS day/month in UTC, since Baghdad = UTC+3) must be attributed to the
// correct local month, not the UTC one — the bug this task's polish fixes.
func TestUsageForSubscriber_MonthBoundary_Baghdad(t *testing.T) {
	db := testDB(t)
	sub := randomUUID()

	// 2026-03-31 22:00:00 UTC = 2026-04-01 01:00:00 Asia/Baghdad (UTC+3): this
	// traffic belongs to APRIL locally even though its UTC calendar date is
	// still March 31.
	edge := time.Date(2026, 3, 31, 22, 0, 0, 0, time.UTC)
	insertUsagePoint(t, db, sub, edge, 1000, 200)
	// An unambiguous mid-March point for contrast.
	insertUsagePoint(t, db, sub, time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC), 5000, 500)

	// Query one window spanning both months and inspect the returned bucket
	// keys directly — the correctness claim is about which BUCKET each point
	// lands in, not about pre-slicing the request window (from/to are a raw
	// time-range filter per the API's RFC3339 UTC convention, independent of
	// the Baghdad-local bucket boundaries the query groups by).
	pts, err := live.UsageForSubscriber(context.Background(), db, sub, true,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UsageForSubscriber: %v", err)
	}
	byBucket := map[time.Time]int64{}
	for _, p := range pts {
		byBucket[p.T] = p.Down
	}

	// Baghdad-local month starts, expressed as the UTC instants they fall on
	// (Baghdad is UTC+3 year-round, no DST): March 1 00:00 Baghdad = Feb 28
	// 21:00 UTC; April 1 00:00 Baghdad = March 31 21:00 UTC.
	marchBucket := time.Date(2026, 2, 28, 21, 0, 0, 0, time.UTC)
	aprilBucket := time.Date(2026, 3, 31, 21, 0, 0, 0, time.UTC)

	if got := byBucket[marchBucket]; got != 5000 {
		t.Fatalf("March bucket (%v) = %d bytes down, want 5000 (the edge point must NOT be counted in March)", marchBucket, got)
	}
	if got := byBucket[aprilBucket]; got != 1000 {
		t.Fatalf("April bucket (%v) = %d bytes down, want 1000 (the edge point must be counted in April, Baghdad-local): buckets=%v", aprilBucket, got, byBucket)
	}
}

// An over-wide requested range must be capped, not returned unbounded
// (response-size cap, task 5) — and the cap must keep the MOST RECENT data.
func TestUsageForSubscriber_ResponseSizeCap(t *testing.T) {
	db := testDB(t)
	sub := randomUUID()

	base := time.Now().UTC().Truncate(24 * time.Hour)
	_, err := db.Exec(context.Background(),
		`INSERT INTO usage_points (time, subscriber_id, delta_down, delta_up, service)
		   SELECT $1::timestamptz - (n || ' days')::interval, $2::uuid, 100, 50, 'pppoe'
		     FROM generate_series(0, 900) AS n`,
		base, sub)
	if err != nil {
		t.Fatalf("seed usage_points: %v", err)
	}

	from := base.Add(-1000 * 24 * time.Hour)
	to := base.Add(24 * time.Hour)
	out, err := live.UsageForSubscriber(context.Background(), db, sub, false, from, to)
	if err != nil {
		t.Fatalf("UsageForSubscriber: %v", err)
	}
	if len(out) > 800 {
		t.Fatalf("expected the response capped at 800 points, got %d", len(out))
	}
	if len(out) == 0 {
		t.Fatal("expected some points, got none")
	}
	// The cap must keep the newest data: the last point should be at/after base.
	last := out[len(out)-1].T
	if last.Before(base.Add(-24 * time.Hour)) {
		t.Fatalf("cap dropped the most recent data: last point = %v, want close to %v", last, base)
	}
}
