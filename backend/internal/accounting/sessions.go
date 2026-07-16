package accounting

// Session + usage_point upsert (contract C6 steps b/c, FR-37.2, FR-58 tagging).
// Runs inside the consumer's per-record transaction. The open session row is
// SELECT ... FOR UPDATE locked so delta computation is correct even if the
// consumer group ever runs more than one worker.
//
// RADIUS octet direction (RFC 2866): Acct-Input-Octets is traffic the NAS
// received FROM the user = upload; Acct-Output-Octets is traffic the NAS sent TO
// the user = download. So bytes_in = upload total, bytes_out = download total,
// and the live down-rate is derived from the output (download) delta.
//
// Time discipline (brief edge case): usage-point time and the dedup key use the
// NAS event_time; session liveness (last_interim_at, used by the reaper) and the
// rate-averaging interval use the server receipt time, which cannot be skewed by
// a NAS with a wrong clock.

import (
	"context"
	"time"

	"github.com/hikrad/hikrad/internal/live/livestate"
	"github.com/jackc/pgx/v5"
)

// applyResult is what upsertSession returns to the consumer: the live state to
// publish (open sessions), whether the session is now closed (→ remove from
// live), and the identity fields needed to clean the live index sets.
type applyResult struct {
	State        livestate.State
	Closed       bool
	SubscriberID string
	Service      string
	OrphanStop   bool
}

// binding is who/where a record belongs to: the NAS, the FR-62 service instance
// it resolved to, its coarse service, and the subscriber. Grouped rather than
// passed as four adjacent positional strings, which the writers below would
// happily accept in the wrong order.
type binding struct {
	NASID        string
	Service      string // pppoe | hotspot
	ServiceID    string // nas_services.id; "" when unresolved (unregistered NAS)
	SubscriberID string
}

// upsertSession applies one accounting record to the sessions table and inserts
// the usage-point delta. tx is the consumer's transaction; the binding comes
// from the NAS resolver + username lookup.
func upsertSession(ctx context.Context, tx pgx.Tx, rec Record, b binding) (applyResult, error) {
	nasID, service, subscriberID := b.NASID, b.Service, b.SubscriberID
	var (
		haveRow     bool
		lastIn      int64
		lastOut     int64
		startedAt   *time.Time
		lastInterim *time.Time
		wasStopped  bool
	)
	err := tx.QueryRow(ctx,
		`SELECT bytes_in, bytes_out, started_at, last_interim_at, (stopped_at IS NOT NULL)
		   FROM sessions WHERE nas_id = $1 AND acct_session_id = $2 FOR UPDATE`,
		nasID, rec.AcctSessionID).
		Scan(&lastIn, &lastOut, &startedAt, &lastInterim, &wasStopped)
	switch {
	case err == nil:
		haveRow = true
	case err == pgx.ErrNoRows:
		haveRow = false
	default:
		return applyResult{}, err
	}

	// A Start, or any record on a previously-stopped session, resets the delta
	// baseline (session-id reuse / reopen after reap). Otherwise deltas accrue
	// from the stored totals.
	reset := rec.RecordType == RecordStart || wasStopped
	var newIn, newOut, dUp, dDown uint64
	if reset || !haveRow {
		newIn = totalBytes(rec.BytesIn, rec.GigawordsIn)
		newOut = totalBytes(rec.BytesOut, rec.GigawordsOut)
		dUp, dDown = newIn, newOut
	} else {
		newIn, dUp = advance(uint64(lastIn), rec.BytesIn, rec.GigawordsIn)
		newOut, dDown = advance(uint64(lastOut), rec.BytesOut, rec.GigawordsOut)
	}

	evt := rec.eventTime()
	res := applyResult{SubscriberID: subscriberID, Service: service}

	switch rec.RecordType {
	case RecordStart:
		if err := writeStart(ctx, tx, rec, b, newIn, newOut, evt); err != nil {
			return applyResult{}, err
		}
	case RecordInterim:
		if err := writeInterim(ctx, tx, rec, b, newIn, newOut, evt, haveRow); err != nil {
			return applyResult{}, err
		}
	case RecordStop:
		res.OrphanStop = !haveRow
		if err := writeStop(ctx, tx, rec, b, newIn, newOut, evt, haveRow); err != nil {
			return applyResult{}, err
		}
		res.Closed = true
	}

	// Usage point keyed by NAS event time (FR-37.4 out-of-order tolerance). Skip
	// empty deltas (a typical Start) to keep the hypertable lean.
	if dUp > 0 || dDown > 0 {
		if err := insertUsagePoint(ctx, tx, evt, subscriberID, nasID, dDown, dUp, service); err != nil {
			return applyResult{}, err
		}
	}

	// Build the live state for open sessions; a Stop removes the row instead.
	if !res.Closed {
		start := evt
		switch {
		case rec.RecordType == RecordStart:
			start = evt
		case startedAt != nil:
			start = (*startedAt).UTC()
		default:
			start = rec.ReceiptTime
		}
		// Interval for the last delta: previous interim → now, in server time.
		intervalStart := rec.ReceiptTime
		if rec.RecordType != RecordStart {
			if lastInterim != nil {
				intervalStart = (*lastInterim).UTC()
			} else if startedAt != nil {
				intervalStart = (*startedAt).UTC()
			}
		}
		rateDown, rateUp := rates(dDown, dUp, intervalStart, rec.ReceiptTime)
		res.State = livestate.State{
			Username:      rec.Username,
			SubscriberID:  subscriberID,
			NASID:         nasID,
			AcctSessionID: rec.AcctSessionID,
			IP:            rec.FramedIP,
			MAC:           rec.CallingStationID,
			StartedAt:     start.UTC(),
			LastInterimAt: rec.ReceiptTime,
			BytesIn:       int64(newIn),
			BytesOut:      int64(newOut),
			RateDownBps:   rateDown,
			RateUpBps:     rateUp,
			Stale:         false,
			Service:       service,
		}
	}
	return res, nil
}

func writeStart(ctx context.Context, tx pgx.Tx, rec Record, b binding, in, out uint64, evt time.Time) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO sessions
		   (nas_id, acct_session_id, subscriber_id, username, ip, mac, started_at,
		    last_interim_at, stopped_at, bytes_in, bytes_out, service, nas_service_id, stale, reaped)
		 VALUES ($1,$2,$3,$4,NULLIF($5::text,'')::inet,$6,$7,$8,NULL,$9,$10,$11,$12::uuid,false,false)
		 ON CONFLICT (nas_id, acct_session_id) DO UPDATE SET
		    subscriber_id = EXCLUDED.subscriber_id,
		    username = EXCLUDED.username,
		    ip = EXCLUDED.ip,
		    mac = EXCLUDED.mac,
		    started_at = EXCLUDED.started_at,
		    last_interim_at = EXCLUDED.last_interim_at,
		    stopped_at = NULL,
		    terminate_cause = '',
		    bytes_in = EXCLUDED.bytes_in,
		    bytes_out = EXCLUDED.bytes_out,
		    service = EXCLUDED.service,
		    nas_service_id = EXCLUDED.nas_service_id,
		    stale = false,
		    reaped = false`,
		b.NASID, rec.AcctSessionID, nullUUID(b.SubscriberID), rec.Username, rec.FramedIP, rec.CallingStationID,
		evt, rec.ReceiptTime, int64(in), int64(out), b.Service, nullUUID(b.ServiceID))
	return err
}

func writeInterim(ctx context.Context, tx pgx.Tx, rec Record, b binding, in, out uint64, evt time.Time, haveRow bool) error {
	if !haveRow {
		// Interim before Start (NAS reboot race): synthesize an open session.
		_, err := tx.Exec(ctx,
			`INSERT INTO sessions
			   (nas_id, acct_session_id, subscriber_id, username, ip, mac, started_at,
			    last_interim_at, bytes_in, bytes_out, service, nas_service_id, stale, reaped)
			 VALUES ($1,$2,$3,$4,NULLIF($5::text,'')::inet,$6,$7,$8,$9,$10,$11,$12::uuid,false,false)
			 ON CONFLICT (nas_id, acct_session_id) DO NOTHING`,
			b.NASID, rec.AcctSessionID, nullUUID(b.SubscriberID), rec.Username, rec.FramedIP, rec.CallingStationID,
			evt, rec.ReceiptTime, int64(in), int64(out), b.Service, nullUUID(b.ServiceID))
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE sessions SET
		    subscriber_id = COALESCE($3::uuid, subscriber_id),
		    username = CASE WHEN $4::text <> '' THEN $4::text ELSE username END,
		    ip = COALESCE(NULLIF($5::text,'')::inet, ip),
		    mac = CASE WHEN $6::text <> '' THEN $6::text ELSE mac END,
		    last_interim_at = $7,
		    stopped_at = NULL,
		    bytes_in = $8,
		    bytes_out = $9,
		    stale = false,
		    reaped = false
		  WHERE nas_id = $1 AND acct_session_id = $2`,
		b.NASID, rec.AcctSessionID, nullUUID(b.SubscriberID), rec.Username, rec.FramedIP, rec.CallingStationID,
		rec.ReceiptTime, int64(in), int64(out))
	return err
}

func writeStop(ctx context.Context, tx pgx.Tx, rec Record, b binding, in, out uint64, evt time.Time, haveRow bool) error {
	if !haveRow {
		// Stop for an unknown session: record a closed row (orphan_stops++).
		_, err := tx.Exec(ctx,
			`INSERT INTO sessions
			   (nas_id, acct_session_id, subscriber_id, username, ip, mac, started_at,
			    last_interim_at, stopped_at, terminate_cause, bytes_in, bytes_out, service, nas_service_id, stale, reaped)
			 VALUES ($1,$2,$3,$4,NULLIF($5::text,'')::inet,$6,$7,$8,$9,$10,$11,$12,$13,$14::uuid,false,false)
			 ON CONFLICT (nas_id, acct_session_id) DO NOTHING`,
			b.NASID, rec.AcctSessionID, nullUUID(b.SubscriberID), rec.Username, rec.FramedIP, rec.CallingStationID,
			evt, rec.ReceiptTime, evt, rec.TerminateCause, int64(in), int64(out), b.Service, nullUUID(b.ServiceID))
		return err
	}
	// A Stop does not rewrite identity fields (username/ip/mac) — only closes the
	// session — so those params are intentionally absent: passing them unused
	// would leave Postgres unable to infer their type.
	_, err := tx.Exec(ctx,
		`UPDATE sessions SET
		    subscriber_id = COALESCE($3::uuid, subscriber_id),
		    stopped_at = $4,
		    terminate_cause = $5,
		    last_interim_at = $6,
		    bytes_in = $7,
		    bytes_out = $8,
		    stale = false,
		    reaped = false
		  WHERE nas_id = $1 AND acct_session_id = $2`,
		b.NASID, rec.AcctSessionID, nullUUID(b.SubscriberID),
		evt, rec.TerminateCause, rec.ReceiptTime, int64(in), int64(out))
	return err
}

func insertUsagePoint(ctx context.Context, tx pgx.Tx, evt time.Time, sub, nasID string, dDown, dUp uint64, service string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO usage_points (time, subscriber_id, nas_id, delta_down, delta_up, exempt, service)
		 VALUES ($1, $2, $3, $4, $5, false, $6)`,
		evt, nullUUID(sub), nasID, int64(dDown), int64(dUp), service)
	return err
}

// rates computes the last-interval average bit rates (FR-31.2). A non-positive
// interval yields zero rather than a divide-by-zero.
func rates(dDown, dUp uint64, from, to time.Time) (down, up int64) {
	secs := to.Sub(from).Seconds()
	if secs <= 0 {
		return 0, 0
	}
	return int64(float64(dDown*8) / secs), int64(float64(dUp*8) / secs)
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
