package radius

// Durable CoA outbox storage (FR-150, migrations 0614-0615). This is the ONLY
// place coa_outbox/coa_attempts rows are read or written; coa_enqueue.go
// (writers), coa_dispatcher.go (the worker-tick state machine) and
// coa_outbox_api.go (the panel surface) all go through it.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/radius/vendor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CoAAction is the original requested operation (frozen contract §4) — never
// changes after enqueue, so it stays the FR-152 action filter's ground truth
// even after a hybrid fallback switches what is actually being attempted.
type CoAAction string

const (
	CoAActionUpdate     CoAAction = "update"
	CoAActionDisconnect CoAAction = "disconnect"
)

// OutboxState is the frozen state machine (§4): pending → sent →
// {done|nack|failed}, nack → pending (retry) or nack → sent (fallback). No
// other transition exists anywhere in this package.
type OutboxState string

const (
	OutboxPending OutboxState = "pending"
	OutboxSent    OutboxState = "sent"
	OutboxDone    OutboxState = "done"
	OutboxNack    OutboxState = "nack"
	OutboxFailed  OutboxState = "failed"
)

// OutboxRow is one coa_outbox record.
type OutboxRow struct {
	ID             string
	SubscriberID   string
	NASID          string
	AcctSessionID  string
	Username       string
	FramedIP       string
	Service        string
	Action         CoAAction
	CurrentOp      CoAAction
	Attributes     []vendor.Attr
	Reason         string
	RequestedBy    string
	State          OutboxState
	AttemptCount   int
	LastError      string
	LastOutcome    string
	FallbackUsed   bool
	FallbackAt     *time.Time
	FallbackReason string
	NextAttemptAt  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SentAt         *time.Time
	DoneAt         *time.Time
}

func (r OutboxRow) sessionRef() SessionRef {
	return SessionRef{NASID: r.NASID, AcctSessionID: r.AcctSessionID, Username: r.Username, FramedIP: r.FramedIP, Service: r.Service}
}

// OutboxAttempt is one coa_attempts record — the per-attempt evidence trail
// (why did this fail) the inspect view reads.
type OutboxAttempt struct {
	ID             string
	OutboxID       string
	AttemptNo      int
	Op             CoAAction
	Outcome        string
	Error          string
	WireAttributes []WireAttr
	AttemptedAt    time.Time
}

// OutboxCounters is FR-152's six-counter summary.
type OutboxCounters struct {
	Pending int64 `json:"pending"`
	Sent    int64 `json:"sent"`
	Nack    int64 `json:"nack"`
	Done    int64 `json:"done"`
	Failed  int64 `json:"failed"`
	Total   int64 `json:"total"`
}

// outboxColumns selects every coa_outbox column. NASName is deliberately NOT
// joined in here — an UPDATE...RETURNING (claimBatch) cannot join another
// table, and one shared column list/scan function for every caller (the
// dispatcher's claimBatch/listNackRows included) is worth more than a
// pre-joined name; the panel API layer (coa_outbox_api.go) batch-resolves
// NAS names for the rows it actually renders.
const outboxColumns = `id::text, subscriber_id::text, nas_id::text, acct_session_id, username, framed_ip, service,
	action, current_op, attributes, reason, requested_by::text, state, attempt_count, last_error, last_outcome,
	fallback_used, fallback_at, fallback_reason, next_attempt_at, created_at, updated_at, sent_at, done_at`

func scanOutbox(row pgx.Row) (OutboxRow, error) {
	var r OutboxRow
	var subID, reqBy *string
	var attrsRaw []byte
	var action, currentOp, state string
	err := row.Scan(&r.ID, &subID, &r.NASID, &r.AcctSessionID, &r.Username, &r.FramedIP, &r.Service,
		&action, &currentOp, &attrsRaw, &r.Reason, &reqBy, &state, &r.AttemptCount, &r.LastError, &r.LastOutcome,
		&r.FallbackUsed, &r.FallbackAt, &r.FallbackReason, &r.NextAttemptAt, &r.CreatedAt, &r.UpdatedAt, &r.SentAt, &r.DoneAt)
	if err != nil {
		return OutboxRow{}, err
	}
	if subID != nil {
		r.SubscriberID = *subID
	}
	if reqBy != nil {
		r.RequestedBy = *reqBy
	}
	r.Action = CoAAction(action)
	r.CurrentOp = CoAAction(currentOp)
	r.State = OutboxState(state)
	_ = json.Unmarshal(attrsRaw, &r.Attributes)
	return r, nil
}

// outboxInsert is enqueueCoA's write shape (coa_enqueue.go).
type outboxInsert struct {
	SubscriberID  string
	NASID         string
	AcctSessionID string
	Username      string
	FramedIP      string
	Service       string
	Action        CoAAction
	Attributes    []vendor.Attr
	Reason        string
	RequestedBy   string
}

func insertOutbox(ctx context.Context, db *pgxpool.Pool, in outboxInsert) (string, error) {
	attrs, err := json.Marshal(in.Attributes)
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRow(ctx, `
		INSERT INTO coa_outbox (subscriber_id, nas_id, acct_session_id, username, framed_ip, service, action, current_op, attributes, reason, requested_by)
		VALUES (NULLIF($1,'')::uuid, $2::uuid, $3, $4, $5, $6, $7, $7, $8, $9, NULLIF($10,'')::uuid)
		RETURNING id::text`,
		in.SubscriberID, in.NASID, in.AcctSessionID, in.Username, in.FramedIP, in.Service,
		string(in.Action), attrs, in.Reason, in.RequestedBy,
	).Scan(&id)
	return id, err
}

func getOutbox(ctx context.Context, db *pgxpool.Pool, id string) (OutboxRow, error) {
	return scanOutbox(db.QueryRow(ctx, `SELECT `+outboxColumns+` FROM coa_outbox WHERE id = $1::uuid`, id))
}

// outboxFilter is the panel list/count filter set (FR-152).
type outboxFilter struct {
	State  string // "" = all
	Action string // "" = all
	Search string // matches username or reason, case-insensitive substring
}

func (f outboxFilter) where(startArg int) (string, []any) {
	var clauses []string
	var args []any
	arg := startArg
	add := func(cond string, val any) {
		clauses = append(clauses, fmt.Sprintf(cond, arg))
		args = append(args, val)
		arg++
	}
	if f.State != "" {
		add("state = $%d", f.State)
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.Search != "" {
		// One placeholder, matched against three columns (username, NAS name,
		// reason) — the FR-152 "search by username, NAS, or reason" filter.
		clauses = append(clauses, fmt.Sprintf(
			"(username ILIKE $%d OR reason ILIKE $%d OR nas_id::text IN (SELECT id::text FROM nas WHERE name ILIKE $%d))",
			arg, arg, arg))
		args = append(args, "%"+f.Search+"%")
		arg++
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
}

// listOutbox returns one page ordered newest-first (created_at, id keyset —
// the same pattern billing/tickets_api.go uses), plus the keyset values of
// the last row for the next page's cursor (nil when exhausted); the caller
// (coa_outbox_api.go) is the one that knows about httpapi.EncodeCursor.
func listOutbox(ctx context.Context, db *pgxpool.Pool, f outboxFilter, cursor []string, limit int) ([]OutboxRow, []string, error) {
	where, args := f.where(1)
	if len(cursor) == 2 {
		conj := " WHERE "
		if where != "" {
			conj = " AND "
		}
		where += fmt.Sprintf("%s(created_at, id) < ($%d::timestamptz, $%d::uuid)", conj, len(args)+1, len(args)+2)
		args = append(args, cursor[0], cursor[1])
	}
	q := `SELECT ` + outboxColumns + ` FROM coa_outbox` + where +
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d", limit+1)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var items []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next []string
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		next = []string{last.CreatedAt.Format(time.RFC3339Nano), last.ID}
	}
	return items, next, nil
}

// outboxCounters computes FR-152's six counters in one round trip.
func outboxCounters(ctx context.Context, db *pgxpool.Pool) (OutboxCounters, error) {
	var c OutboxCounters
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'pending'),
			count(*) FILTER (WHERE state = 'sent'),
			count(*) FILTER (WHERE state = 'nack'),
			count(*) FILTER (WHERE state = 'done'),
			count(*) FILTER (WHERE state = 'failed'),
			count(*)
		FROM coa_outbox`).Scan(&c.Pending, &c.Sent, &c.Nack, &c.Done, &c.Failed, &c.Total)
	return c, err
}

// recentFailureCount is the 24h failure count v4-12's dashboard panel reads
// (FR-152, FR-174).
func recentFailureCount(ctx context.Context, db *pgxpool.Pool, since time.Time) (int64, error) {
	var n int64
	err := db.QueryRow(ctx,
		`SELECT count(*) FROM coa_outbox WHERE state = 'failed' AND done_at >= $1`, since).Scan(&n)
	return n, err
}

func listAttempts(ctx context.Context, db *pgxpool.Pool, outboxID string) ([]OutboxAttempt, error) {
	// Ordered chronologically (attempted_at), not by attempt_no: a hybrid
	// fallback resets attempt_count for the disconnect leg's fresh budget, so
	// attempt_no alone can repeat (e.g. update attempt 1-3, then disconnect
	// attempt 1-3) — attempted_at is what keeps the inspect view's timeline
	// actually chronological.
	rows, err := db.Query(ctx, `
		SELECT id::text, outbox_id::text, attempt_no, op, outcome, error, wire_attributes, attempted_at
		  FROM coa_attempts WHERE outbox_id = $1::uuid ORDER BY attempted_at`, outboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxAttempt
	for rows.Next() {
		var a OutboxAttempt
		var op string
		var wireRaw []byte
		if err := rows.Scan(&a.ID, &a.OutboxID, &a.AttemptNo, &op, &a.Outcome, &a.Error, &wireRaw, &a.AttemptedAt); err != nil {
			return nil, err
		}
		a.Op = CoAAction(op)
		_ = json.Unmarshal(wireRaw, &a.WireAttributes)
		out = append(out, a)
	}
	return out, rows.Err()
}

func insertAttempt(ctx context.Context, db *pgxpool.Pool, outboxID string, attemptNo int, op CoAAction, outcome, errText string, wire []WireAttr) error {
	wireJSON, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO coa_attempts (outbox_id, attempt_no, op, outcome, error, wire_attributes)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		outboxID, attemptNo, string(op), outcome, errText, wireJSON)
	return err
}

// cancelOutboxRow cancels an unsent row (gate item 5: "cancel stops an unsent
// row and cannot cancel a sent one"). Modeled as a transition straight to
// failed with a synthetic reason — cancel is not in §4's state list as its
// own state, so it reuses `failed`, the only terminal state that fits "will
// never be attempted again."
func cancelOutboxRow(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	ct, err := db.Exec(ctx, `
		UPDATE coa_outbox SET state = 'failed', last_outcome = 'cancelled', done_at = now()
		 WHERE id = $1::uuid AND state = 'pending'`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// retryOutboxRow re-queues a nack/failed row from scratch: back to the
// original action, attempt budget reset, fallback cleared (gate item 5: "retry
// re-queues"). A pending/sent row is already going to be attempted and a done
// row succeeded, so retry only ever affects nack/failed.
func retryOutboxRow(ctx context.Context, db *pgxpool.Pool, id string) (bool, error) {
	ct, err := db.Exec(ctx, `
		UPDATE coa_outbox
		   SET state = 'pending', current_op = action, attempt_count = 0,
		       fallback_used = false, fallback_at = NULL, fallback_reason = '',
		       last_error = '', last_outcome = '', next_attempt_at = now(),
		       sent_at = NULL, done_at = NULL
		 WHERE id = $1::uuid AND state IN ('nack', 'failed')`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// nasNames batch-resolves NAS display names for the panel API layer (the
// inspect/list views show a router name, not a bare uuid).
func nasNames(ctx context.Context, db *pgxpool.Pool, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT id::text, name FROM nas WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// --- dispatcher-only mutations -----------------------------------------

// claimBatch atomically transitions up to limit due `pending` rows to `sent`
// and returns them, using FOR UPDATE SKIP LOCKED so a second dispatcher
// process (or a concurrent tick) can never claim the same row twice — the
// gate item 10 "no duplicate dispatch" guarantee.
func claimBatch(ctx context.Context, db *pgxpool.Pool, limit int) ([]OutboxRow, error) {
	rows, err := db.Query(ctx, `
		UPDATE coa_outbox
		   SET state = 'sent', sent_at = now()
		 WHERE id IN (
		     SELECT id FROM coa_outbox
		      WHERE state = 'pending' AND next_attempt_at <= now()
		      ORDER BY next_attempt_at
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING `+outboxColumns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// reclaimStuckSent recovers rows a dead process left in `sent` (crash safety,
// gate item 2 "queue survives restart"): the network exchange never being
// certain to have completed, resending is always safe (CoA/Disconnect are
// idempotent operations at the NAS).
func reclaimStuckSent(ctx context.Context, db *pgxpool.Pool, olderThan time.Duration) (int64, error) {
	ct, err := db.Exec(ctx, `
		UPDATE coa_outbox SET state = 'pending', next_attempt_at = now()
		 WHERE state = 'sent' AND sent_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// resolveDone marks a successful attempt's row terminal.
func resolveDone(ctx context.Context, db *pgxpool.Pool, id, outcome string) error {
	_, err := db.Exec(ctx, `
		UPDATE coa_outbox SET state = 'done', done_at = now(), last_outcome = $2, attempt_count = attempt_count + 1
		 WHERE id = $1::uuid`, id, outcome)
	return err
}

// resolveNack records a failed attempt (nack/timeout/error/unsupported) —
// the row sits in `nack` (a real, counted state) until nackSweep decides its
// next move.
func resolveNack(ctx context.Context, db *pgxpool.Pool, id, outcome, errText string) error {
	_, err := db.Exec(ctx, `
		UPDATE coa_outbox
		   SET state = 'nack', attempt_count = attempt_count + 1, last_outcome = $2, last_error = $3
		 WHERE id = $1::uuid`, id, outcome, errText)
	return err
}

// resolveReaped marks a row `done` without ever dispatching, because its
// target session is no longer live (the reaper interaction, gate item 8): a
// disconnect (or update) for a session that has already ended is a no-op,
// never a failure.
func resolveReaped(ctx context.Context, db *pgxpool.Pool, id string) error {
	_, err := db.Exec(ctx, `
		UPDATE coa_outbox SET state = 'done', done_at = now(), last_outcome = 'skipped_reaped'
		 WHERE id = $1::uuid`, id)
	return err
}

// nackDecision is one nack-sweep row's computed next step.
type nackDecision struct {
	ID             string
	Retry          bool // -> pending, current_op unchanged
	Fallback       bool // -> pending, current_op = disconnect, fallback fields set
	Fail           bool // -> failed
	FallbackReason string
}

// applyNackDecision persists one nackSweep decision.
func applyNackDecision(ctx context.Context, db *pgxpool.Pool, d nackDecision) error {
	switch {
	case d.Retry:
		_, err := db.Exec(ctx, `
			UPDATE coa_outbox SET state = 'pending', next_attempt_at = $2 WHERE id = $1::uuid`,
			d.ID, nextAttemptBackoff())
		return err
	case d.Fallback:
		_, err := db.Exec(ctx, `
			UPDATE coa_outbox
			   SET state = 'pending', current_op = 'disconnect', attempt_count = 0,
			       fallback_used = true, fallback_at = now(), fallback_reason = $2,
			       next_attempt_at = now()
			 WHERE id = $1::uuid`, d.ID, d.FallbackReason)
		return err
	case d.Fail:
		_, err := db.Exec(ctx, `
			UPDATE coa_outbox SET state = 'failed', done_at = now() WHERE id = $1::uuid`, d.ID)
		return err
	}
	return nil
}

// listNackRows returns every row currently sitting in `nack`, for the sweep.
func listNackRows(ctx context.Context, db *pgxpool.Pool) ([]OutboxRow, error) {
	rows, err := db.Query(ctx, `SELECT `+outboxColumns+` FROM coa_outbox WHERE state = 'nack'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// nextAttemptBackoff spaces out retries a little rather than hammering an
// unreachable NAS on every tick; deliberately modest since the dispatcher
// tick itself is already only every few seconds.
func nextAttemptBackoff() time.Time {
	return time.Now().Add(3 * time.Second)
}
