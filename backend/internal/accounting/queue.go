package accounting

// Redis stream queue (contract C6, FR-37.1). The ack path appends the record
// here; a consumer group drains it. Redis runs with AOF appendfsync=everysec
// (compose), so an XADD that returns is durable within the ≤1 s window the NAS
// retransmit already covers. When Redis is unreachable the caller falls back to
// the disk spill instead.

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	streamKey     = "acct:stream"
	consumerGroup = "acct-consumers"
	streamField   = "d"
)

// counterReceivedKey/counterEnqueuedKey mirror the received/enqueued
// counters into Redis on the same synchronous round trip ingest already
// makes to durably enqueue (bumpRedisCounter is called right alongside
// enqueueStream/the received Add) — piggy-backing on an already-open
// connection, not a new dependency. Unlike the Postgres periodic flush
// (runCounterFlusher), this survives an unclean hikrad-acct crash exactly
// like the stream itself does: at boot, counters.load() prefers whichever
// source (Postgres mirror vs. this) has seen more. Postgres remains the
// long-term durable home (these keys are trivial rebuildable caches, not the
// source of truth in the way the stream/spill are for the actual data) but
// closes the gap the Phase-5 chaos suite found — see runCounterFlusher's doc
// comment for the full story.
const (
	counterReceivedKey = "acct:counters:received"
	counterEnqueuedKey = "acct:counters:enqueued"
)

// bumpRedisCounter is fire-and-forget: a failure here only means boot falls
// back to the (slightly staler) Postgres mirror, never affects the data path.
func bumpRedisCounter(ctx context.Context, rdb *redis.Client, key string) {
	if rdb == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	rdb.Incr(cctx, key)
}

// redisCounterValue reads one of the above, treating "unset"/any error as 0
// (a fresh Redis, or Redis genuinely unreachable — either way the Postgres
// mirror is the fallback).
func redisCounterValue(ctx context.Context, rdb *redis.Client, key string) int64 {
	if rdb == nil {
		return 0
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := rdb.Get(cctx, key).Int64()
	if err != nil {
		return 0
	}
	return n
}

// enqueue appends a record payload to the stream. Returns the assigned stream id.
func enqueueStream(ctx context.Context, rdb *redis.Client, payload []byte) (string, error) {
	if rdb == nil {
		return "", errors.New("accounting: redis not configured")
	}
	return rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{streamField: payload},
	}).Result()
}

// ensureGroup creates the consumer group at the stream head, tolerating the
// BUSYGROUP error when it already exists. MKSTREAM makes the first call work
// before any packet has arrived.
func ensureGroup(ctx context.Context, rdb *redis.Client) error {
	err := rdb.XGroupCreateMkStream(ctx, streamKey, consumerGroup, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}
	return nil
}

func isBusyGroup(err error) bool {
	return err != nil && (err.Error() == "BUSYGROUP Consumer Group name already exists" ||
		containsBusyGroup(err.Error()))
}

func containsBusyGroup(s string) bool {
	const marker = "BUSYGROUP"
	for i := 0; i+len(marker) <= len(s); i++ {
		if s[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}
