// Package livestate is the single source of truth for the live-session Redis
// representation shared across process boundaries (contract C6). hikrad-acct's
// consumer (internal/accounting) writes it; hikrad-api's live module
// (internal/live) reads it for the SSE feed, the live.Count/List interface B's
// session-limit check consumes, and the panel history. Keeping the key layout
// and JSON shape here means neither side can drift from the other.
//
// Redis layout:
//
//	live:sessions                     HASH  field "<nasID>:<acctSessionID>" → State JSON
//	live:events                       CHANNEL of Event JSON (SSE deltas)
//	live:sub:<subscriberID>:<service> SET   of live fields → O(1) SCARD for Count (FR-58.2)
//	live:nas:<nasID>                  SET   of live fields → O(1) SCARD for NAS live count
//
// The per-subscriber/per-NAS index sets exist so the hot authorize path never
// has to HGETALL the whole live table to answer a session-limit question
// (NFR-1). SADD/SREM are idempotent, so a duplicate Start or a replayed packet
// cannot double-count — which is exactly what the chaos scenarios exercise.
package livestate

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key/channel names (frozen with the C6 contract).
const (
	HashKey       = "live:sessions"
	EventsChannel = "live:events"
	subSetPrefix  = "live:sub:"
	nasSetPrefix  = "live:nas:"
)

// Services the pipeline distinguishes (FR-58). "" means "all services".
const (
	ServicePPPoE   = "pppoe"
	ServiceHotspot = "hotspot"
)

// State is the JSON value stored per live session (contract C6). Byte counters
// are cumulative session totals; rates are the last interval's average
// (delta ÷ interval), labelled as such in the UI (FR-31.2).
type State struct {
	Username      string    `json:"username"`
	SubscriberID  string    `json:"subscriber_id"`
	NASID         string    `json:"nas_id"`
	AcctSessionID string    `json:"acct_session_id"`
	IP            string    `json:"ip"`
	MAC           string    `json:"mac"`
	StartedAt     time.Time `json:"started_at"`
	LastInterimAt time.Time `json:"last_interim_at"`
	BytesIn       int64     `json:"bytes_in"`
	BytesOut      int64     `json:"bytes_out"`
	RateDownBps   int64     `json:"rate_down_bps"`
	RateUpBps     int64     `json:"rate_up_bps"`
	Stale         bool      `json:"stale"`
	Service       string    `json:"service"`
	// NASServiceID / ServiceName identify WHICH of the NAS's service instances
	// the session is running on (FR-62). Service alone says "hotspot"; on a
	// router with three hotspot zones that does not tell an operator where the
	// user actually is. Both are empty when nothing identified the instance —
	// a session is never dropped for being unattributable (M2).
	//
	// ServiceName is denormalized on purpose: live state is ephemeral and
	// rewritten on every accounting packet, and the SSE push path must not do a
	// per-event lookup to render a name.
	NASServiceID string `json:"nas_service_id"`
	ServiceName  string `json:"service_name"`
}

// Field is the live:sessions hash field for a session.
func Field(nasID, acctSessionID string) string { return nasID + ":" + acctSessionID }

// ParseField splits a hash field back into its NAS id and Acct-Session-Id. The
// NAS id is a UUID (no colon), so a single split on the first ":" is exact even
// when the Acct-Session-Id itself contains one.
func ParseField(field string) (nasID, acctSessionID string) {
	nasID, acctSessionID, _ = strings.Cut(field, ":")
	return nasID, acctSessionID
}

func subSetKey(subscriberID, service string) string {
	return subSetPrefix + subscriberID + ":" + service
}
func nasSetKey(nasID string) string { return nasSetPrefix + nasID }

// Event is a live-feed delta published on EventsChannel and forwarded by the
// SSE handler. Op is "upsert" or "remove"; State is nil for "remove".
type Event struct {
	Op    string `json:"op"`
	Field string `json:"field"`
	State *State `json:"state,omitempty"`
}

const (
	OpUpsert = "upsert"
	OpRemove = "remove"
)

// Marshal encodes a State (helper so callers don't import encoding/json).
func (s State) Marshal() ([]byte, error) { return json.Marshal(s) }

// Unmarshal decodes a State from its stored JSON.
func Unmarshal(raw []byte) (State, error) {
	var s State
	err := json.Unmarshal(raw, &s)
	return s, err
}

// Upsert writes/refreshes a live session: the hash field, the subscriber and
// NAS index sets, and a published upsert Event — all in one pipeline so the SSE
// snapshot and the count indexes never disagree. Idempotent by construction.
func Upsert(ctx context.Context, rdb *redis.Client, s State) error {
	raw, err := s.Marshal()
	if err != nil {
		return err
	}
	field := Field(s.NASID, s.AcctSessionID)
	evt, _ := json.Marshal(Event{Op: OpUpsert, Field: field, State: &s})

	pipe := rdb.Pipeline()
	pipe.HSet(ctx, HashKey, field, raw)
	if s.SubscriberID != "" {
		pipe.SAdd(ctx, subSetKey(s.SubscriberID, s.Service), field)
	}
	if s.NASID != "" {
		pipe.SAdd(ctx, nasSetKey(s.NASID), field)
	}
	pipe.Publish(ctx, EventsChannel, evt)
	_, err = pipe.Exec(ctx)
	return err
}

// Remove deletes a live session and its index memberships, publishing a remove
// Event. subscriberID/service/nasID are needed to clean the index sets; pass
// what the closing record resolved (empty values simply skip that set).
func Remove(ctx context.Context, rdb *redis.Client, nasID, acctSessionID, subscriberID, service string) error {
	field := Field(nasID, acctSessionID)
	evt, _ := json.Marshal(Event{Op: OpRemove, Field: field})

	pipe := rdb.Pipeline()
	pipe.HDel(ctx, HashKey, field)
	if subscriberID != "" {
		pipe.SRem(ctx, subSetKey(subscriberID, service), field)
	}
	if nasID != "" {
		pipe.SRem(ctx, nasSetKey(nasID), field)
	}
	pipe.Publish(ctx, EventsChannel, evt)
	_, err := pipe.Exec(ctx)
	return err
}

// Count returns the number of live sessions for a subscriber, optionally scoped
// to one service (contract C6, FR-58.2). service "" counts pppoe + hotspot.
// O(1) SCARD(s); nil rdb → 0 so callers degrade safely.
func Count(ctx context.Context, rdb *redis.Client, subscriberID, service string) int {
	if rdb == nil || subscriberID == "" {
		return 0
	}
	if service != "" {
		n, err := rdb.SCard(ctx, subSetKey(subscriberID, service)).Result()
		if err != nil {
			return 0
		}
		return int(n)
	}
	var total int64
	for _, svc := range []string{ServicePPPoE, ServiceHotspot} {
		n, err := rdb.SCard(ctx, subSetKey(subscriberID, svc)).Result()
		if err == nil {
			total += n
		}
	}
	return int(total)
}

// NASCount returns the number of live sessions on a NAS (delete-confirmation,
// FR-13.4). O(1) SCARD; nil rdb → 0.
func NASCount(ctx context.Context, rdb *redis.Client, nasID string) int {
	if rdb == nil || nasID == "" {
		return 0
	}
	n, err := rdb.SCard(ctx, nasSetKey(nasID)).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

// All returns every live session (SSE snapshot / List source). Malformed
// entries are skipped rather than failing the whole read.
func All(ctx context.Context, rdb *redis.Client) ([]State, error) {
	if rdb == nil {
		return nil, nil
	}
	m, err := rdb.HGetAll(ctx, HashKey).Result()
	if err != nil {
		return nil, err
	}
	out := make([]State, 0, len(m))
	for _, raw := range m {
		s, err := Unmarshal([]byte(raw))
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
