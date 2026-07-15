package accounting

// Stale-session reaper (FR-38). A session that stops getting interims is first
// dimmed (stale) and then, after a longer timeout, closed with a synthesized
// Stop flagged reaped — never silently deleted. A real Stop or interim that
// arrives later supersedes/reopens it (handled in the consumer's upsert, which
// clears stale/reaped on any live record).
//
// Interval source: the nas table carries no per-NAS interim column (B stores it
// only in the RouterOS config snippet, hardcoded 300 s), so the expected
// interval is a single service-wide value (env HIKRAD_ACCT_INTERIM_SECS, default
// 300) — see the package README "Contract deviations". Thresholds:
//   stale  after 2× interval           (FR-38.1)
//   reap   after 3× interval + 5 min   (FR-38.2, overridable)

import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
)

func (s *Service) runReaper(ctx context.Context) {
	tick := s.interimInterval // scan about once per expected interval
	if tick < 15*time.Second {
		tick = 15 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.reapOnce(ctx, s.now()); err != nil {
				s.log.Warn("acct reaper: pass failed", "error", err)
			}
		}
	}
}

// reapOnce marks newly-stale sessions and reaps the long-silent ones. now is
// injectable for tests.
func (s *Service) reapOnce(ctx context.Context, now time.Time) error {
	staleAfter := 2 * s.interimInterval
	reapAfter := 3*s.interimInterval + 5*time.Minute

	rows, err := s.db.Query(ctx,
		`SELECT nas_id::text, acct_session_id, COALESCE(subscriber_id::text,''), service,
		        last_interim_at, stale
		   FROM sessions
		  WHERE stopped_at IS NULL`)
	if err != nil {
		return err
	}
	type openSession struct {
		nasID, acctID, sub, service string
		lastInterim                 *time.Time
		stale                       bool
	}
	var open []openSession
	for rows.Next() {
		var o openSession
		if err := rows.Scan(&o.nasID, &o.acctID, &o.sub, &o.service, &o.lastInterim, &o.stale); err != nil {
			rows.Close()
			return err
		}
		open = append(open, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, o := range open {
		last := now
		if o.lastInterim != nil {
			last = (*o.lastInterim).UTC()
		}
		age := now.Sub(last)
		switch {
		case age >= reapAfter:
			if err := s.reapSession(ctx, now, o.nasID, o.acctID, o.sub, o.service); err != nil {
				s.log.Warn("acct reaper: reap failed", "error", err, "acct_session_id", o.acctID)
			}
		case age >= staleAfter && !o.stale:
			if err := s.markStale(ctx, o.nasID, o.acctID, o.sub, o.service); err != nil {
				s.log.Warn("acct reaper: mark stale failed", "error", err, "acct_session_id", o.acctID)
			}
		}
	}
	return nil
}

// markStale dims a session in history and in the live feed (FR-38.1).
func (s *Service) markStale(ctx context.Context, nasID, acctID, sub, service string) error {
	if _, err := s.db.Exec(ctx,
		`UPDATE sessions SET stale = true
		  WHERE nas_id = $1 AND acct_session_id = $2 AND stopped_at IS NULL`,
		nasID, acctID); err != nil {
		return err
	}
	// Reflect the stale flag in the live hash without dropping the row.
	if s.rdb != nil {
		field := livestate.Field(nasID, acctID)
		if raw, err := s.rdb.HGet(ctx, livestate.HashKey, field).Bytes(); err == nil {
			if st, uerr := livestate.Unmarshal(raw); uerr == nil {
				st.Stale = true
				_ = livestate.Upsert(ctx, s.rdb, st)
			}
		}
	}
	return nil
}

// reapSession closes a long-silent session with a synthesized Stop (FR-38.2):
// reaped=true, usage frozen at the last interim, removed from the live feed.
func (s *Service) reapSession(ctx context.Context, now time.Time, nasID, acctID, sub, service string) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE sessions SET
		    stopped_at = COALESCE(last_interim_at, started_at, $3::timestamptz),
		    terminate_cause = 'Reaper-Timeout',
		    stale = true,
		    reaped = true
		  WHERE nas_id = $1 AND acct_session_id = $2 AND stopped_at IS NULL`,
		nasID, acctID, now)
	if err != nil {
		return err
	}
	if ct.RowsAffected() > 0 {
		s.counters.reaped.Add(1)
		bumpCounterDurable(ctx, s.db, "reaped")
	}
	if s.rdb != nil {
		_ = livestate.Remove(ctx, s.rdb, nasID, acctID, sub, service)
	}
	return nil
}
