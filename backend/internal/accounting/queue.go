package accounting

// Redis stream queue (contract C6, FR-37.1). The ack path appends the record
// here; a consumer group drains it. Redis runs with AOF appendfsync=everysec
// (compose), so an XADD that returns is durable within the ≤1 s window the NAS
// retransmit already covers. When Redis is unreachable the caller falls back to
// the disk spill instead.

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

const (
	streamKey     = "acct:stream"
	consumerGroup = "acct-consumers"
	streamField   = "d"
)

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
