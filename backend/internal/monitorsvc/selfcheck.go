package monitorsvc

// Monitor-process self-checks that produce the handoff values only this process
// can measure and the dashboard/health API then read:
//   - FreeRADIUS liveness (Status-Server probe when a secret is configured, else
//     inferred from recent decision-stream activity) → health:freeradius:up
//   - accounting drain rate (Δpersisted / Δt sampled here) → health:acct:drain_rate
//   - the per-minute online-session count → online_samples (dashboard sparkline)

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"layeh.com/radius"
)

type selfCheck struct {
	db  *pgxpool.Pool
	rdb *redis.Client
	log *slog.Logger

	lastPersisted int64
	lastSampledAt time.Time
}

func newSelfCheck(db *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) *selfCheck {
	return &selfCheck{db: db, rdb: rdb, log: log}
}

// runOnlineSampler records the live-session count once a minute (FR-32 sparkline).
func (s *selfCheck) runOnlineSampler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	s.sampleOnline(ctx) // one immediately so a fresh dashboard isn't blank
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sampleOnline(ctx)
		}
	}
}

func (s *selfCheck) sampleOnline(ctx context.Context) {
	if s.db == nil || s.rdb == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	n, err := s.rdb.HLen(c, livestate.HashKey).Result()
	if err != nil {
		return
	}
	if _, err := s.db.Exec(c, `INSERT INTO online_samples (online) VALUES ($1)`, n); err != nil {
		s.log.Warn("selfcheck: online sample write failed", "error", err)
	}
}

// runHealthProbes updates the FreeRADIUS liveness + acct drain-rate handoff keys
// on a short cadence so /health and /dashboard stay ≤ 10 s fresh.
func (s *selfCheck) runHealthProbes(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.probeFreeRADIUS(ctx)
			s.sampleDrainRate(ctx)
		}
	}
}

func (s *selfCheck) probeFreeRADIUS(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	up := s.freeRADIUSUp(ctx)
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = s.rdb.Set(c, keyFreeRADIUSUp, up, 90*time.Second).Err()
}

// freeRADIUSUp tries a Status-Server exchange when a secret is configured; if the
// status virtual server isn't enabled (or no secret), it falls back to "did we
// see any RADIUS decision in the last 2 minutes".
func (s *selfCheck) freeRADIUSUp(ctx context.Context) bool {
	addr := os.Getenv("HIKRAD_FREERADIUS_ADDR")
	if addr == "" {
		addr = "freeradius:1812"
	}
	if secret := os.Getenv("HIKRAD_RADIUS_STATUS_SECRET"); secret != "" {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		packet := radius.New(radius.CodeStatusServer, []byte(secret))
		if resp, err := radius.Exchange(c, packet, addr); err == nil && resp != nil {
			return true
		}
		// Status-Server unanswered — fall through to the activity heuristic rather
		// than declaring down (the status vserver may simply be disabled).
	}
	total, _ := decisionCounts(ctx, s.rdb, 2*time.Minute)
	return total > 0
}

// sampleDrainRate computes records-persisted-per-second between two samples and
// publishes it for /health's queue.drain_rate.
func (s *selfCheck) sampleDrainRate(ctx context.Context) {
	snap := acctSnapshot(ctx, s.db, s.rdb)
	if snap == nil {
		return
	}
	persisted, ok := toInt64(snap["persisted"])
	if !ok {
		return
	}
	now := time.Now()
	if !s.lastSampledAt.IsZero() {
		dt := now.Sub(s.lastSampledAt).Seconds()
		if dt > 0 {
			rate := float64(persisted-s.lastPersisted) / dt
			if rate < 0 {
				rate = 0 // counter reset / restart
			}
			if s.rdb != nil {
				c, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = s.rdb.Set(c, keyAcctDrain, strconv.FormatFloat(rate, 'f', 2, 64), 90*time.Second).Err()
				cancel()
			}
		}
	}
	s.lastPersisted = persisted
	s.lastSampledAt = now
}
