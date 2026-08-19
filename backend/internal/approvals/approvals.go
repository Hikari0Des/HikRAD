// Package approvals is the generic agent-approval-gate substrate (FR-159).
// A "kind" of money-returning action (undo, invoice void, subscriber
// delete) registers exactly ONE executor function; RequestOrExecute either
// runs it immediately (an unscoped caller) or parks a pending
// approval_requests row (a scoped agent), and Approve later calls the SAME
// registered executor with the stored payload. There is deliberately no
// second "approved variant" anywhere in this package — that is the whole
// point of the frozen contract (FR-159 §4, gate item 10): a bespoke
// approved-path implementation drifts from the agent's own path and the
// divergence is a money bug.
//
// Mirrors the module registry's existing seam discipline
// (radius.SetVoucherAuthenticator, subscribers.SetInvoicer): each owning
// package calls Register from its own init()/wireOnce so this package never
// imports billing/subscribers/etc — only they import this.
package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor performs the real work for one approvable kind, given the
// payload RequestOrExecute captured and the manager who is ultimately
// causing it to run (the original requester for a direct/unscoped call, the
// approver for a replayed one). It manages its own transaction(s) against
// db — the same way m.renew/refundHandler/etc already do — RequestOrExecute
// and Approve never wrap it in a shared transaction of their own (see the
// package doc's crash-safety note on Approve).
type Executor func(ctx context.Context, db *pgxpool.Pool, payload json.RawMessage, actorManagerID string) (result any, err error)

var (
	mu       sync.RWMutex
	registry = map[string]Executor{}
)

// Register installs the executor for a kind. Registering the same kind
// twice panics — a programming error (two packages fighting over one kind
// name), not a runtime condition to handle gracefully.
func Register(kind string, exec Executor) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[kind]; exists {
		panic("approvals: kind " + kind + " already registered")
	}
	registry[kind] = exec
}

func executorFor(kind string) (Executor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := registry[kind]
	return e, ok
}

var (
	ErrUnknownKind = errors.New("approvals: no executor registered for this kind")
	ErrNotFound    = errors.New("approvals: request not found")
	ErrNotPending  = errors.New("approvals: request is not pending")
)

// Request is the API read shape for one approval_requests row.
type Request struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	RequestedBy  string          `json:"requested_by"`
	Description  string          `json:"description"`
	State        string          `json:"state"`
	DecidedBy    *string         `json:"decided_by"`
	DecidedAt    *string         `json:"decided_at"`
	RejectReason *string         `json:"reject_reason"`
	Result       json.RawMessage `json:"result"`
	CreatedAt    string          `json:"created_at"`
}

// RequestOrExecute is the FR-159 split: a scoped (agent) caller performing a
// money-returning action gets it parked as a pending request instead of
// executed; an unscoped (admin/operator) caller's action runs immediately
// through the exact same executor Approve calls later. pending=true means
// requestID is populated and result is nil; pending=false means the action
// already ran and result holds its return value.
func RequestOrExecute(ctx context.Context, db *pgxpool.Pool, kind string, payload any, mgr *auth.Manager, description string) (pending bool, requestID string, result any, err error) {
	exec, ok := executorFor(kind)
	if !ok {
		return false, "", nil, ErrUnknownKind
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return false, "", nil, err
	}
	actorID := ""
	if mgr != nil {
		actorID = mgr.ID
	}
	if mgr != nil && mgr.Scoped {
		var id string
		err := db.QueryRow(ctx,
			`INSERT INTO approval_requests (kind, payload, requested_by, description)
			 VALUES ($1, $2, $3::uuid, $4) RETURNING id::text`,
			kind, buf, actorID, description).Scan(&id)
		if err != nil {
			return false, "", nil, err
		}
		return true, id, nil, nil
	}
	result, err = exec(ctx, db, buf, actorID)
	return false, "", result, err
}

// Approve replays the ORIGINAL executor with the request's stored payload
// (gate item 10 — one implementation, never a second "approved" variant).
//
// The decision is claimed FIRST, via an atomic pending->approved UPDATE, and
// only then is the executor run — the reverse order (run first, mark
// approved after) would let a crash between the two leave a completed money
// action re-approvable, causing a double-execution. Marking the decision
// before running the action instead means the failure mode of a crash mid-
// execution is "approved but the money action itself needs a manual
// re-check" (recorded via the stored error below), the same operational
// posture this codebase already accepts for renew.go's post-commit CoA
// enqueue: a completed decision is never rolled back by a later step
// failing.
func Approve(ctx context.Context, db *pgxpool.Pool, id, approverID string) (any, error) {
	var kind string
	var payload json.RawMessage
	err := db.QueryRow(ctx,
		`UPDATE approval_requests SET state = 'approved', decided_by = $2::uuid, decided_at = now(), updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'
		 RETURNING kind, payload`, id, approverID).
		Scan(&kind, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the row doesn't exist, or it exists but wasn't pending —
		// disambiguate for the caller's HTTP mapping.
		var exists bool
		_ = db.QueryRow(ctx, `SELECT true FROM approval_requests WHERE id = $1::uuid`, id).Scan(&exists)
		if exists {
			return nil, ErrNotPending
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	exec, ok := executorFor(kind)
	if !ok {
		return nil, ErrUnknownKind
	}
	result, execErr := exec(ctx, db, payload, approverID)
	resBuf, _ := json.Marshal(map[string]any{"ok": execErr == nil, "value": result, "error": errString(execErr)})
	_, _ = db.Exec(ctx, `UPDATE approval_requests SET result = $2, updated_at = now() WHERE id = $1::uuid`, id, resBuf)
	if execErr != nil {
		return nil, execErr
	}
	return result, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Reject marks a pending request rejected without ever calling its
// executor.
func Reject(ctx context.Context, db *pgxpool.Pool, id, approverID, reason string) error {
	ct, err := db.Exec(ctx,
		`UPDATE approval_requests SET state = 'rejected', decided_by = $2::uuid, decided_at = now(),
		        reject_reason = $3, updated_at = now()
		  WHERE id = $1::uuid AND state = 'pending'`, id, approverID, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		var exists bool
		_ = db.QueryRow(ctx, `SELECT true FROM approval_requests WHERE id = $1::uuid`, id).Scan(&exists)
		if exists {
			return ErrNotPending
		}
		return ErrNotFound
	}
	return nil
}

// Withdraw lets the original requester cancel their own still-pending
// request.
func Withdraw(ctx context.Context, db *pgxpool.Pool, id, requesterID string) error {
	ct, err := db.Exec(ctx,
		`UPDATE approval_requests SET state = 'withdrawn', updated_at = now()
		  WHERE id = $1::uuid AND requested_by = $2::uuid AND state = 'pending'`, id, requesterID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotPending
	}
	return nil
}

const requestColumns = `id::text, kind, payload, requested_by::text, description, state,
	decided_by::text, to_char(decided_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	reject_reason, result, to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')`

func scanRequest(row pgx.Row) (Request, error) {
	var v Request
	var decidedAt, decidedBy *string
	err := row.Scan(&v.ID, &v.Kind, &v.Payload, &v.RequestedBy, &v.Description, &v.State,
		&decidedBy, &decidedAt, &v.RejectReason, &v.Result, &v.CreatedAt)
	v.DecidedBy, v.DecidedAt = decidedBy, decidedAt
	return v, err
}

// Get returns one request by id.
func Get(ctx context.Context, db *pgxpool.Pool, id string) (Request, error) {
	v, err := scanRequest(db.QueryRow(ctx, `SELECT `+requestColumns+` FROM approval_requests WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return v, err
}

// List returns requests filtered by state ("" = any) and, when mine != "",
// pinned to that requester (an agent's own pending list, FR-159's "agents
// see and may withdraw their own pending requests").
func List(ctx context.Context, db *pgxpool.Pool, state, mine string, limit int) ([]Request, error) {
	q := `SELECT ` + requestColumns + ` FROM approval_requests WHERE ($1 = '' OR state = $1) AND ($2 = '' OR requested_by::text = $2)
	       ORDER BY created_at DESC LIMIT $3`
	rows, err := db.Query(ctx, q, state, mine, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		v, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
