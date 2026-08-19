package billing

// FR-159: this package's money-returning actions (invoice void, undo) route
// through internal/approvals' generic registry instead of executing
// directly. requestOrExecute is a thin wrapper so every call site here reads
// the same as every other billing handler (m.db, not a bare pool argument).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hikrad/hikrad/internal/approvals"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nowIn returns the database's own now() (UTC), the same "resolve time from
// the DB, not the app server's clock" discipline every money transaction in
// this package already follows (renew.go, refund.go, balance_api.go).
func nowIn(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var t time.Time
	err := tx.QueryRow(ctx, `SELECT now()`).Scan(&t)
	return t.UTC(), err
}

func (m *Module) requestOrExecute(ctx context.Context, kind string, payload any, mgr *auth.Manager, description string) (pending bool, requestID string, result any, err error) {
	return approvals.RequestOrExecute(ctx, m.db, kind, payload, mgr, description)
}

const approvalKindInvoiceVoid = "invoice_void"

type voidInvoicePayload struct {
	InvoiceID string `json:"invoice_id"`
	Reason    string `json:"reason"`
}

func init() {
	approvals.Register(approvalKindInvoiceVoid, executeVoidInvoice)
}

func executeVoidInvoice(ctx context.Context, db *pgxpool.Pool, raw json.RawMessage, actorID string) (any, error) {
	var p voidInvoicePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	at, err := nowIn(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := voidInvoice(ctx, tx, at, p.InvoiceID, p.Reason, actorID, "billing.invoice_void"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"invoice_id": p.InvoiceID, "status": "void"}, nil
}

// --- undo (FR-158 x FR-159): a scoped agent's undo is itself a money-
// returning action and parks for approval the same as invoice void. The
// executor delegates to performUndo (undo.go), the ONE undo implementation.

const approvalKindUndo = "undo"

type undoPayload struct {
	UndoID string `json:"undo_id"`
}

func init() {
	approvals.Register(approvalKindUndo, executeUndo)
}

func executeUndo(ctx context.Context, _ *pgxpool.Pool, raw json.RawMessage, actorID string) (any, error) {
	var p undoPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return singleton.performUndo(ctx, p.UndoID, actorID)
}
