package billing

// Five-minute undo window (FR-158). Scoped to the money actions this
// package creates: renewal and top-up (see the gate result's "deliberate
// scope decisions" for why undo does not attempt to cover every mutating
// action in the whole product).
//
// An undo is a REVERSING ENTRY, never a delete (§4's frozen contract): a
// renewal's undo calls refundCore — the EXACT SAME function the panel's
// refund button calls — so "undo" and "refund" can never diverge into two
// different reversal behaviors. A top-up's undo posts its own small
// reversing ledger+journal pair (undoTopupCore) since refundCore is
// subscriber-centric and a top-up has no subscriber to roll back.
//
// "With redo": undoing a row posts a NEW undo_log row (undo_of) wrapping the
// reversal itself, so it is undoable too within its OWN 5-minute window —
// redo is the same mechanism run again, not a second code path.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const undoWindow = 5 * time.Minute

var (
	errUndoNotFound    = errors.New("billing: nothing to undo")
	errUndoAlreadyDone = errors.New("billing: this action has already been undone")
	errUndoExpired     = errors.New("billing: the undo window has closed")
	errUndoRecordGone  = errors.New("billing: the referenced record no longer exists")
	errUndoRecreated   = errors.New("billing: the referenced record was recreated since this action and can no longer be undone this way")
	errUndoChanged     = errors.New("billing: this record changed after the action and can no longer be undone")
)

type undoRenewalSnapshot struct {
	NewExpiresAt time.Time `json:"new_expires_at"`
	// PriorRecorded marks a snapshot that carries PriorExpiresAt (nil there =
	// the subscriber never expired). Undo restores that exact prior expiry;
	// a snapshot written before this field existed falls back to refund's
	// roll-back-by-duration rule.
	PriorRecorded  bool       `json:"prior_recorded,omitempty"`
	PriorExpiresAt *time.Time `json:"prior_expires_at,omitempty"`
}
type undoTopupSnapshot struct {
	ManagerID string `json:"manager_id"`
	Amount    int64  `json:"amount"`
}

// recordUndo inserts an undo_log row for a just-committed action. Run
// AFTER the action's own transaction commits (never inside it) — losing an
// undo_log row to a crash between the two only makes that one action
// non-undoable, never a money-safety issue, so it is deliberately not made
// atomic with the action itself.
func (m *Module) recordUndo(ctx context.Context, kind, entityType, entityID, actorID, ledgerTxID string, snapshot any, undoOf string) (string, error) {
	buf, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	var journalEntryID string
	_ = m.db.QueryRow(ctx,
		`SELECT id::text FROM journal_entries WHERE ledger_tx_id = $1::uuid ORDER BY at DESC LIMIT 1`, ledgerTxID).
		Scan(&journalEntryID)
	var id string
	err = m.db.QueryRow(ctx,
		`INSERT INTO undo_log (kind, entity_type, entity_id, actor_manager_id, ledger_tx_id, journal_entry_id, snapshot, expires_at, undo_of)
		 VALUES ($1, $2, $3::uuid, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, NULLIF($6,'')::uuid, $7, now() + interval '5 minutes', NULLIF($8,'')::uuid)
		 RETURNING id::text`,
		kind, entityType, entityID, actorID, ledgerTxID, journalEntryID, buf, undoOf).Scan(&id)
	return id, err
}

type undoResult struct {
	UndoID             string `json:"undo_id"`
	ReversalLedgerTxID string `json:"reversal_ledger_tx_id"`
	RedoUndoID         string `json:"redo_undo_id"`
}

// performUndo is the ONE undo implementation, called by both undoHandler
// (an unscoped caller) and the "undo" approval kind's executor for a scoped
// one (approvals_gate.go).
func (m *Module) performUndo(ctx context.Context, undoID, actorID string) (undoResult, error) {
	var (
		kind, entityType, entityID, ledgerTxID string
		snapshot                               json.RawMessage
		expiresAt                              time.Time
		undoneAt                               *time.Time
	)
	err := m.db.QueryRow(ctx,
		`SELECT kind, entity_type, entity_id::text, COALESCE(ledger_tx_id::text,''), snapshot, expires_at, undone_at
		   FROM undo_log WHERE id = $1::uuid`, undoID).
		Scan(&kind, &entityType, &entityID, &ledgerTxID, &snapshot, &expiresAt, &undoneAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return undoResult{}, errUndoNotFound
	}
	if err != nil {
		return undoResult{}, err
	}
	if undoneAt != nil {
		return undoResult{}, errUndoAlreadyDone
	}

	var now time.Time
	if err := m.db.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return undoResult{}, err
	}
	now = now.UTC()
	if now.After(expiresAt) {
		return undoResult{}, errUndoExpired
	}

	var reversalLedgerTxID string
	switch kind {
	case "renewal":
		var snap undoRenewalSnapshot
		if err := json.Unmarshal(snapshot, &snap); err != nil {
			return undoResult{}, err
		}
		var curExpires time.Time
		err := m.db.QueryRow(ctx, `SELECT expires_at FROM subscribers WHERE id = $1::uuid`, entityID).Scan(&curExpires)
		if errors.Is(err, pgx.ErrNoRows) {
			return undoResult{}, errUndoRecordGone
		}
		if err != nil {
			return undoResult{}, err
		}
		if !curExpires.UTC().Equal(snap.NewExpiresAt.UTC()) {
			// Either another renewal/edit landed since (a real "changed
			// since" case), or — structurally impossible for a UUID-keyed,
			// append-only-ledger row, but checked for completeness — the
			// row was recreated under the same id.
			return undoResult{}, errUndoChanged
		}
		var restore *expiryRestore
		if snap.PriorRecorded {
			restore = &expiryRestore{At: snap.PriorExpiresAt}
		}
		res, err := m.refundCore(ctx, entityID, ledgerTxID, "undo", "undo", "billing.undo", restore)
		if err != nil {
			return undoResult{}, err
		}
		reversalLedgerTxID = res.RefundLedgerTxID

	case "topup":
		var snap undoTopupSnapshot
		if err := json.Unmarshal(snapshot, &snap); err != nil {
			return undoResult{}, err
		}
		var exists bool
		if err := m.db.QueryRow(ctx, `SELECT true FROM managers WHERE id = $1::uuid`, snap.ManagerID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return undoResult{}, errUndoRecordGone
		} else if err != nil {
			return undoResult{}, err
		}
		id, err := m.undoTopupCore(ctx, ledgerTxID, snap.ManagerID, snap.Amount, actorID)
		if err != nil {
			return undoResult{}, err
		}
		reversalLedgerTxID = id

	default:
		return undoResult{}, errors.New("billing: unknown undo kind " + kind)
	}

	if _, err := m.db.Exec(ctx,
		`UPDATE undo_log SET undone_at = now(), undo_reversal_ledger_tx_id = $2::uuid WHERE id = $1::uuid`,
		undoID, reversalLedgerTxID); err != nil {
		return undoResult{}, err
	}
	redoID, err := m.recordUndo(ctx, "undo_reversal", entityType, entityID, actorID, reversalLedgerTxID,
		map[string]any{"reversal_of": undoID}, undoID)
	if err != nil {
		return undoResult{}, err
	}
	return undoResult{UndoID: undoID, ReversalLedgerTxID: reversalLedgerTxID, RedoUndoID: redoID}, nil
}

// undoTopupCore reverses a top-up: a new ledger row (type=topup, negative
// amount) + its journal entry (signedPair swaps sides automatically for a
// negative amount — see journal.go), in one transaction. Refuses (via the
// unique reverses_id index, same guard refundCore relies on for a renewal)
// if this top-up was already reversed.
func (m *Module) undoTopupCore(ctx context.Context, ledgerTxID, managerID string, amount int64, actorID string) (string, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	at, err := nowIn(ctx, tx)
	if err != nil {
		return "", err
	}
	if _, err := lockBalance(ctx, tx, managerID); err != nil {
		return "", err
	}
	entry := ledgerEntry{
		Type: "topup", Amount: -amount, ActorManagerID: managerID,
		Source: "undo", Reference: ledgerTxID, ReversesID: ledgerTxID, Note: "undo: top-up",
	}
	reversalID, err := insertLedger(ctx, tx, entry)
	if isUniqueViolation(err) {
		return "", errUndoAlreadyDone
	}
	if err != nil {
		return "", err
	}
	if err := recomputeBalance(ctx, tx, managerID); err != nil {
		return "", err
	}
	if err := postLedgerJournal(ctx, tx, at, entry, reversalID, 0, false, "billing.undo", actorID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return reversalID, nil
}
