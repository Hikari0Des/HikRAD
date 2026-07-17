package subscribers

// In-package DB tests for the pieces that need unexported access: the AuthView
// read-model loader (override inheritance, FR-7/C4), LearnMac write-through
// (FR-5), and expiry-sweep idempotence (FR-1.2). Gated on HIKRAD_TEST_DB_URL.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func internalDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("HIKRAD_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("HIKRAD_TEST_DB_URL not set; skipping subscribers internal DB tests")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := platform.Migrate(dbURL, "../../migrations", log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := platform.NewDB(context.Background(), platform.Config{DBURL: dbURL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func mkProfile(t *testing.T, db *pgxpool.Pool, down, up int, hsDown, hsUp *int) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO profiles
		   (name, price, duration_days, rate_down_kbps, rate_up_kbps,
		    session_limit_default, hotspot_rate_down_kbps, hotspot_rate_up_kbps)
		 VALUES ($1, 1000, 30, $2, $3, 3, $4, $5) RETURNING id::text`,
		uniqName("prof"), down, up, hsDown, hsUp).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func uniqName(p string) string {
	return p + "-" + time.Now().Format("150405.000000000")
}

func TestAuthViewOverrideInheritance(t *testing.T) {
	db := internalDB(t)
	ctx := context.Background()
	p := &policyProvider{db: db}

	hsDown, hsUp := 5120, 5120
	profID := mkProfile(t, db, 20480, 20480, &hsDown, &hsUp)

	username := uniqName("av")
	var subID string
	if err := db.QueryRow(ctx,
		`INSERT INTO subscribers (username, password_enc, status, profile_id, service_type)
		 VALUES ($1, '\x0102'::bytea, 'active', $2::uuid, 'dual') RETURNING id::text`,
		username, profID).Scan(&subID); err != nil {
		t.Fatal(err)
	}

	// Inherits profile rate + session default + hotspot rate.
	v, err := p.GetAuthView(ctx, username)
	if err != nil {
		t.Fatal(err)
	}
	if v.RateLimit != "20M/20M" {
		t.Errorf("RateLimit = %q, want 20M/20M", v.RateLimit)
	}
	if v.SessionLimit != 3 {
		t.Errorf("SessionLimit = %d, want 3 (profile default)", v.SessionLimit)
	}
	if v.HotspotRateLimit != "5M/5M" {
		t.Errorf("HotspotRateLimit = %q, want 5M/5M", v.HotspotRateLimit)
	}
	if v.ServiceType != "dual" {
		t.Errorf("ServiceType = %q, want dual", v.ServiceType)
	}

	// Apply overrides: rate + session limit win over the profile.
	if _, err := db.Exec(ctx,
		`UPDATE subscribers SET rate_override = '50M/25M', session_limit_override = 9 WHERE id = $1::uuid`,
		subID); err != nil {
		t.Fatal(err)
	}
	v, err = p.GetAuthView(ctx, username)
	if err != nil {
		t.Fatal(err)
	}
	if v.RateLimit != "50M/25M" {
		t.Errorf("override RateLimit = %q, want 50M/25M", v.RateLimit)
	}
	if v.SessionLimit != 9 {
		t.Errorf("override SessionLimit = %d, want 9", v.SessionLimit)
	}

	// Unknown user maps to ErrNoSubscriber.
	if _, err := p.GetAuthView(ctx, uniqName("nobody")); err == nil {
		t.Error("expected ErrNoSubscriber for unknown user")
	}
}

func TestLearnMacWriteThrough(t *testing.T) {
	db := internalDB(t)
	ctx := context.Background()
	p := &policyProvider{db: db}

	username := uniqName("lm")
	var subID string
	if err := db.QueryRow(ctx,
		`INSERT INTO subscribers (username, password_enc, status, mac_lock_mode)
		 VALUES ($1, '\x01'::bytea, 'active', 'learn') RETURNING id::text`,
		username).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	if err := p.LearnMac(ctx, subID, "AA:BB:CC:DD:EE:FF"); err != nil {
		t.Fatal(err)
	}
	var learned *string
	if err := db.QueryRow(ctx, `SELECT learned_mac FROM subscribers WHERE id = $1::uuid`, subID).Scan(&learned); err != nil {
		t.Fatal(err)
	}
	if learned == nil || *learned != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("learned_mac = %v, want AA:BB:CC:DD:EE:FF", learned)
	}
	// Learn is once-only: a second attempt must not overwrite an existing MAC.
	if err := p.LearnMac(ctx, subID, "11:22:33:44:55:66"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT learned_mac FROM subscribers WHERE id = $1::uuid`, subID).Scan(&learned); err != nil {
		t.Fatal(err)
	}
	if *learned != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("learned_mac overwritten to %q", *learned)
	}
}

func TestExpirySweepIdempotence(t *testing.T) {
	db := internalDB(t)
	ctx := context.Background()
	m := &Module{db: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// active but already past expiry → should flip to expired.
	past := uniqName("swp_past")
	var pastID string
	if err := db.QueryRow(ctx,
		`INSERT INTO subscribers (username, password_enc, status, expires_at)
		 VALUES ($1, '\x01'::bytea, 'active', now() - interval '1 day') RETURNING id::text`,
		past).Scan(&pastID); err != nil {
		t.Fatal(err)
	}
	// expired-labelled but renewed into the future → should flip back to active.
	renewed := uniqName("swp_renew")
	var renewedID string
	if err := db.QueryRow(ctx,
		`INSERT INTO subscribers (username, password_enc, status, expires_at)
		 VALUES ($1, '\x01'::bytea, 'expired', now() + interval '10 days') RETURNING id::text`,
		renewed).Scan(&renewedID); err != nil {
		t.Fatal(err)
	}

	if _, err := m.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, db, pastID, "expired")
	assertStatus(t, db, renewedID, "active")

	// Idempotence: a second sweep must change neither of our rows.
	if err := db.QueryRow(ctx, `SELECT 1`).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	before := rowStatuses(t, db, pastID, renewedID)
	if _, err := m.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	after := rowStatuses(t, db, pastID, renewedID)
	if before[pastID] != after[pastID] || before[renewedID] != after[renewedID] {
		t.Errorf("sweep not idempotent: before=%v after=%v", before, after)
	}

	// Flap guard: a row renewed into the future while active must stay active.
	if _, err := db.Exec(ctx,
		`UPDATE subscribers SET status='active', expires_at = now() + interval '30 days' WHERE id = $1::uuid`,
		pastID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.sweepOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, db, pastID, "active")
}

func assertStatus(t *testing.T, db *pgxpool.Pool, id, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(context.Background(), `SELECT status FROM subscribers WHERE id = $1::uuid`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("subscriber %s status = %q, want %q", id, got, want)
	}
}

func rowStatuses(t *testing.T, db *pgxpool.Pool, ids ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, id := range ids {
		var s string
		if err := db.QueryRow(context.Background(), `SELECT status FROM subscribers WHERE id = $1::uuid`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		out[id] = s
	}
	return out
}
