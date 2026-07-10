// Package accounting is HikRAD's lossless accounting pipeline (Agent 3, FR-37,
// FR-38, FR-40, NFR-2). It is the guts of the hikrad-acct binary: the
// ack-after-durable-enqueue HTTP ingest, the Redis-stream + disk-spill queue,
// the idempotent consumer that upserts sessions/usage into TimescaleDB and the
// live Redis state, the stale-session reaper, and the audit counters that prove
// zero loss (success metric M2).
package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// zeroUUID is the sentinel nas_id used when an accounting packet's source IP is
// not a registered NAS. It keeps the sessions/usage upserts and the acct_dedup
// key comparing concrete values instead of SQL-distinct NULLs (see 0134).
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// Record types (contract C6).
const (
	RecordStart   = "start"
	RecordInterim = "interim"
	RecordStop    = "stop"
)

// Record is the normalized C6 accounting payload as it flows through the queue.
// Ingest parses the FreeRADIUS forward into this, stamps ReceiptTime, and stores
// it verbatim on the stream; the consumer resolves nas_id/subscriber_id and
// computes deltas. NAS event_time is used for usage-point time and the dedup
// key; server ReceiptTime is used for liveness/reaper (edge case in the brief).
type Record struct {
	RecordType       string `json:"record_type"`
	NASIP            string `json:"nas_ip"`
	AcctSessionID    string `json:"acct_session_id"`
	Username         string `json:"username"`
	FramedIP         string `json:"framed_ip"`
	CallingStationID string `json:"calling_station_id"`
	SessionTime      int64  `json:"session_time"`
	BytesIn          uint64 `json:"bytes_in"`
	BytesOut         uint64 `json:"bytes_out"`
	GigawordsIn      uint64 `json:"gigawords_in"`
	GigawordsOut     uint64 `json:"gigawords_out"`
	// EventTimeRaw is the raw NAS Event-Timestamp (string form or unix seconds).
	EventTimeRaw   string `json:"event_time"`
	TerminateCause string `json:"terminate_cause"`
	// ReceiptTime and EventTimeParsed are stamped by ingest and carried on the
	// queue so the consumer never re-parses (and can't disagree about the dedup
	// key). EventTimeParsed is the canonical NAS event time; ReceiptTime is the
	// server clock for liveness/reaper.
	ReceiptTime     time.Time `json:"receipt_time"`
	EventTimeParsed time.Time `json:"event_time_parsed"`
}

var errNoSession = errors.New("accounting: missing acct_session_id")

// parseRecord decodes and validates an ingest body into a Record, stamping the
// server receipt time. A missing record_type/acct_session_id is a hard reject
// (the NAS is sending something that is not a session record).
func parseRecord(body []byte, now time.Time) (Record, error) {
	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return Record{}, err
	}
	r.RecordType = strings.ToLower(strings.TrimSpace(r.RecordType))
	switch r.RecordType {
	case RecordStart, RecordInterim, RecordStop:
	default:
		return Record{}, errors.New("accounting: unknown record_type " + strconv.Quote(r.RecordType))
	}
	if strings.TrimSpace(r.AcctSessionID) == "" {
		return Record{}, errNoSession
	}
	r.ReceiptTime = now.UTC()
	r.EventTimeParsed = parseEventTime(r.EventTimeRaw, r.ReceiptTime)
	return r, nil
}

// eventTimeLayouts are the FreeRADIUS date renderings we accept for
// Event-Timestamp, in addition to bare unix seconds.
var eventTimeLayouts = []string{
	"Jan _2 2006 15:04:05 MST",
	"Jan 2 2006 15:04:05 MST",
	"Jan _2 2006 15:04:05",
	time.RFC3339,
	"2006-01-02 15:04:05",
}

// parseEventTime canonicalizes the NAS Event-Timestamp. Empty or unparseable →
// fallback (server receipt time). NOTE: retransmit dedup relies on the NAS
// supplying a stable Event-Timestamp (MikroTik does); when it is absent the
// fallback makes a retransmit arriving in a later second look distinct — a
// documented limitation, not a real-world MikroTik path.
func parseEventTime(raw string, fallback time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback.UTC()
	}
	if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC()
	}
	for _, layout := range eventTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return fallback.UTC()
}

// eventTime returns the canonical event timestamp (used for usage-point time
// and the dedup key). Falls back to a re-parse if the value did not survive
// serialization (defensive; ingest always stamps it).
func (r Record) eventTime() time.Time {
	if !r.EventTimeParsed.IsZero() {
		return r.EventTimeParsed
	}
	return parseEventTime(r.EventTimeRaw, r.ReceiptTime)
}

// dedupID is a compact deterministic id for the (nas, session, type, event)
// tuple — used as the disk-spill line key and for logging. The DB unique
// constraint is the authoritative dedup; this is a convenience/debug handle.
func (r Record) dedupID(nasID string) string {
	h := sha256.Sum256([]byte(nasID + "|" + r.AcctSessionID + "|" + r.RecordType + "|" + r.eventTime().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:8])
}

// totalBytes reconstructs a full 64-bit counter from 32-bit octets + gigawords
// (RFC 2869 Acct-*-Gigawords). This is the primary 32-bit-wrap handling: the NAS
// increments gigawords each time the low word wraps.
func totalBytes(octets, gigawords uint64) uint64 {
	return gigawords<<32 | (octets & 0xFFFFFFFF)
}

// advance computes the new stored total and the delta since lastTotal for one
// direction, handling all three cases the pipeline must survive (FR-37.2):
//
//   - gigawords: totalBytes already folds the high word in.
//   - 32-bit wrap without gigawords: the low word decreased from a high value to
//     a low one while gigawords stayed 0 → carry the high word up by one.
//   - counter reset (session-id reuse / NAS reboot): a genuine decrease → count
//     the new total from zero rather than emitting a negative delta.
func advance(lastTotal, octets, gigawords uint64) (newTotal, delta uint64) {
	cur := totalBytes(octets, gigawords)
	if gigawords == 0 && lastTotal > 0 {
		lastLow := lastTotal & 0xFFFFFFFF
		if octets < lastLow && lastLow > (1<<31) && octets < (1<<31) {
			// Looks like a single low-word wrap the NAS didn't reflect.
			cur = ((lastTotal>>32)+1)<<32 | octets
		}
	}
	if cur >= lastTotal {
		return cur, cur - lastTotal
	}
	// Genuine decrease → counter reset.
	return cur, cur
}
