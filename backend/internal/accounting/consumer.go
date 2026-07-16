package accounting

// Stream consumer (contract C6 steps a–f, FR-37.2/37.3/37.4). One consumer
// group drains acct:stream: dedup → upsert session → usage point → live hash →
// quota flag → DB commit → XACK. The stream entry is acked (and deleted, to keep
// XLEN ≈ the in-flight backlog for the FR-40 invariant) ONLY after the DB
// commit. When the DB is down the consumer stops acking; entries pile up in the
// stream (and ingest spills to disk past Redis memory), then drain in order on
// recovery — zero loss.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/redis/go-redis/v9"
)

const (
	readCount = 128
	readBlock = 2 * time.Second
	dbBackoff = 500 * time.Millisecond
)

func (s *Service) runConsumer(ctx context.Context) {
	for {
		if err := ensureGroup(ctx, s.rdb); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("acct consumer: create group failed, retrying", "error", err)
			if sleep(ctx, dbBackoff) {
				return
			}
			continue
		}
		break
	}

	// Reclaim this consumer's own pending entries (delivered but unacked before a
	// crash) before taking new ones.
	backlog := "0"
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := s.read(ctx, backlog)
		if err != nil {
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				if backlog == "0" {
					backlog = ">"
				}
				continue
			}
			s.log.Warn("acct consumer: read failed", "error", err)
			if sleep(ctx, dbBackoff) {
				return
			}
			continue
		}
		if len(msgs) == 0 {
			if backlog == "0" {
				backlog = ">" // PEL drained; take new entries now
			}
			continue
		}
		failed := false
		for _, m := range msgs {
			ack, perr := s.processMessage(ctx, m)
			if perr != nil {
				// Treat as DB unavailable: leave the entry unacked so it is
				// retried, and back off. The stream grows; ingest spills.
				//
				// Force the next read back to id="0" (our own pending-entries
				// list) regardless of what phase we were in: once an entry has
				// been delivered via ">" it will NEVER be redelivered by a
				// later ">" read (that only returns entries no consumer has
				// ever seen) — only "0" redelivers a consumer's own unacked
				// entries. Without this, a DB outage that hits mid-batch during
				// steady-state (not the initial backlog replay) permanently
				// strands every message from the failure point on: the FR-40
				// invariant never recovers (in_queue never drains) even after
				// the DB comes back. Found by the Phase-5 chaos suite's
				// kill-postgres scenario.
				s.log.Warn("acct consumer: process failed, will retry", "error", perr, "id", m.ID)
				backlog = "0"
				failed = true
				if sleep(ctx, dbBackoff) {
					return
				}
				break
			}
			if ack {
				s.ackDelete(ctx, m.ID)
			}
		}
		if failed {
			continue
		}
		if backlog == "0" && len(msgs) < readCount {
			backlog = ">"
		}
	}
}

func (s *Service) read(ctx context.Context, id string) ([]redis.XMessage, error) {
	res, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: s.consumerName,
		Streams:  []string{streamKey, id},
		Count:    readCount,
		Block:    readBlock,
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	return res[0].Messages, nil
}

// ackDelete's underlying data is already durably safe by the time it's
// called (the DB commit that makes it terminal happens first) — but if the
// XAck/XDel pipeline itself fails (e.g. Redis restarting right at this
// instant, as the Phase-5 chaos suite's kill-redis scenario found live), a
// single-shot fire-and-forget leaves that entry permanently in the stream
// for the rest of this process's life: it is not in this consumer's own
// pending-entries list either (XAck never happened), so the startup-only
// "reclaim id=0" recovery never picks it back up, and the FR-40 in_queue
// invariant wedges at N>0 forever even though nothing was lost.
//
// A short bounded retry (5 attempts, ~1s total) turned out not to be enough:
// a Redis container takes a few seconds past "docker start" before it accepts
// connections again (AOF replay), so a kill landing right as this call fires
// still exhausted a 1s budget in testing. Instead retry until it succeeds or
// the process is shutting down — exactly the same trade-off already made for
// a DB-down processMessage failure (s.read's own backoff loop blocks the
// consumer the same way). This entry is the only thing waiting on this call;
// blocking it costs nothing else, and the data is never at risk either way.
func (s *Service) ackDelete(ctx context.Context, id string) {
	backoff := 300 * time.Millisecond
	for {
		pipe := s.rdb.Pipeline()
		pipe.XAck(ctx, streamKey, consumerGroup, id)
		pipe.XDel(ctx, streamKey, id)
		_, err := pipe.Exec(ctx)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			s.log.Warn("acct consumer: ack/del abandoned (shutting down)", "error", err, "id", id)
			return
		}
		s.log.Warn("acct consumer: ack/del failed, retrying", "error", err, "id", id)
		if sleep(ctx, backoff) {
			return
		}
	}
}

// processMessage runs the full pipeline for one stream entry. It returns ack=true
// when the entry is terminal (persisted or a counted duplicate) and must be
// acked; err != nil signals a retryable failure (DB down) — do NOT ack.
func (s *Service) processMessage(ctx context.Context, m redis.XMessage) (ack bool, err error) {
	raw, _ := m.Values[streamField].(string)
	var rec Record
	if uerr := json.Unmarshal([]byte(raw), &rec); uerr != nil {
		// A corrupt entry can never be processed; drop it (ack) so it does not
		// wedge the group. This is not a lost NAS packet — it is an internal
		// encoding fault, logged loudly.
		s.log.Error("acct consumer: undecodable entry dropped", "error", uerr, "id", m.ID)
		return true, nil
	}

	nas := s.nas.byIP(ctx, rec.NASIP)
	// FR-62: the session's service comes from the instance it ran on, resolved
	// per record via the vendor seam — a NAS no longer has one service to
	// inherit. serviceID is "" for an unregistered NAS or an unresolvable
	// instance; the session is still recorded (M2 outranks attribution).
	service, serviceID := nas.resolveService(rec)
	sub := subscriberByUsername(ctx, s.db, rec.Username)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())

	// (a) Dedup: the unique key rejects a NAS retransmit.
	dct, err := tx.Exec(ctx,
		`INSERT INTO acct_dedup (nas_id, acct_session_id, record_type, event_time)
		 VALUES ($1::uuid, $2::text, $3::text, $4::timestamptz) ON CONFLICT DO NOTHING`,
		nas.ID, rec.AcctSessionID, rec.RecordType, rec.eventTime())
	if err != nil {
		return false, err
	}
	if dct.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		s.counters.deduplicated.Add(1)
		bumpCounterDurable(ctx, s.db, "deduplicated")
		// A duplicate Stop still clears any live ghost left by a crash between
		// commit and live-remove (idempotent).
		if rec.RecordType == RecordStop {
			_ = livestate.Remove(ctx, s.rdb, nas.ID, rec.AcctSessionID, sub, service)
		}
		return true, nil
	}

	// (b/c) Session upsert + usage point.
	res, err := upsertSession(ctx, tx, rec, binding{
		NASID: nas.ID, Service: service, ServiceID: serviceID, SubscriberID: sub,
	})
	if err != nil {
		return false, err
	}
	// Bump the durable counter mirror INSIDE this same transaction so it
	// commits atomically with the data it counts — see bumpPersistedInTx's
	// doc comment for why the periodic 5s flush alone isn't enough.
	if err := bumpPersistedInTx(ctx, tx, res.OrphanStop); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	s.counters.persisted.Add(1)
	if res.OrphanStop {
		s.counters.orphanStops.Add(1)
	}

	// (d) Live hash (post-commit; Redis is the derived view, DB is truth).
	if res.Closed {
		_ = livestate.Remove(ctx, s.rdb, nas.ID, rec.AcctSessionID, res.SubscriberID, res.Service)
	} else {
		_ = livestate.Upsert(ctx, s.rdb, res.State)
	}

	// (e) Quota flag on interim/stop (C8).
	if rec.RecordType != RecordStart {
		s.applyQuota(ctx, sub)
	}
	return true, nil
}

// applyQuota recomputes and publishes the quota-exhausted flag (C8).
func (s *Service) applyQuota(ctx context.Context, subscriberID string) {
	if subscriberID == "" {
		return
	}
	exhausted, evaluated := s.quota.evaluate(ctx, subscriberID)
	if !evaluated || s.rdb == nil {
		return
	}
	key := quotaKeyPrefix + subscriberID
	if exhausted {
		_ = s.rdb.Set(ctx, key, true, 0).Err()
	} else {
		_ = s.rdb.Del(ctx, key).Err()
	}
}

// sleep waits d or until ctx is done; returns true if ctx ended.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}
