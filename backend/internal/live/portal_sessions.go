package live

// Portal session history (v4-11, FR-166: "usage over 7/14/30/90 days,
// session history"). A subscriber-scoped sibling to listSessions
// (sessions_api.go's manager-permission-gated `GET /api/v1/sessions`) —
// deliberately its own function rather than a reused handler: the manager
// route trusts auth.ScopeFilter for visibility, but a portal caller's
// identity comes from the token alone (IDOR rule) and must never accept a
// caller-supplied subscriber_id, which listSessions's query shape allows for
// an unscoped manager. Same columns, same cursor shape, so portalapi's
// client-facing JSON is consistent with the panel's.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionsForSubscriber returns subscriberID's own session history, newest
// first. lastSort/lastID are the last returned item's own sort key, valid
// only when hasMore is true — the caller (portalapi) builds the next opaque
// cursor from them with httpapi.EncodeCursor exactly as listSessions does;
// this function stays free of that encoding so it is testable on its own
// inputs/outputs.
func SessionsForSubscriber(ctx context.Context, db *pgxpool.Pool, subscriberID string, cursorTS *time.Time, cursorID *string, limit int) (items []sessionView, lastSort time.Time, lastID string, hasMore bool, err error) {
	rows, err := db.Query(ctx,
		`SELECT s.id::text, COALESCE(s.nas_id::text,''), s.acct_session_id,
		        COALESCE(s.subscriber_id::text,''), s.username, COALESCE(host(s.ip),''), s.mac,
		        s.started_at, s.stopped_at, s.last_interim_at, s.terminate_cause,
		        s.bytes_in, s.bytes_out, s.stale, s.reaped, s.service,
		        COALESCE(s.nas_service_id::text,''),
		        COALESCE(NULLIF(ns.label,''), ns.ros_server_name, '') AS service_name,
		        COALESCE(s.started_at, s.created_at) AS sort_ts
		   FROM sessions s
		   LEFT JOIN nas_services ns ON ns.id = s.nas_service_id
		  WHERE s.subscriber_id = $1::uuid
		    AND ($2::timestamptz IS NULL
		         OR (COALESCE(s.started_at, s.created_at), s.id) < ($2::timestamptz, $3::uuid))
		  ORDER BY sort_ts DESC, s.id DESC
		  LIMIT $4`,
		subscriberID, cursorTS, cursorID, limit+1)
	if err != nil {
		return nil, time.Time{}, "", false, err
	}
	defer rows.Close()

	items = make([]sessionView, 0, limit)
	var sortTSs []time.Time
	for rows.Next() {
		var s sessionView
		var sortTS time.Time
		if err := rows.Scan(&s.ID, &s.NASID, &s.AcctSessionID, &s.SubscriberID, &s.Username, &s.IP, &s.MAC,
			&s.StartedAt, &s.StoppedAt, &s.LastInterimAt, &s.TerminateCause,
			&s.BytesIn, &s.BytesOut, &s.Stale, &s.Reaped, &s.Service,
			&s.NASServiceID, &s.ServiceName, &sortTS); err != nil {
			return nil, time.Time{}, "", false, err
		}
		s.StartedAt = utcp(s.StartedAt)
		s.StoppedAt = utcp(s.StoppedAt)
		s.LastInterimAt = utcp(s.LastInterimAt)
		items = append(items, s)
		sortTSs = append(sortTSs, sortTS)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, "", false, err
	}
	if len(items) > limit {
		items = items[:limit]
		sortTSs = sortTSs[:limit]
		hasMore = true
		lastSort = sortTSs[len(sortTSs)-1]
		lastID = items[len(items)-1].ID
	}
	return items, lastSort, lastID, hasMore, nil
}
