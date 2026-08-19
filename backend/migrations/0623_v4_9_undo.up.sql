-- v4-9 (FR-158): the five-minute undo window. Scoped to the money actions
-- this phase's own billing package creates/touches (renewal, top-up,
-- invoice void, expense void) — sub-PRD 05's domain, and the domain the
-- binding invariant ("undo writes a reversing entry, never a delete") is
-- actually about. Undoing an arbitrary non-money state change elsewhere in
-- the product (a profile edit, a NAS rename) is a materially different,
-- much larger cross-cutting effort outside this phase's scope; see the gate
-- result's "deliberate scope decisions" for the full reasoning.
--
-- "With redo": undoing a row here posts a NEW undo_log row (undo_of) for the
-- reversal itself, so it is undoable too, within ITS OWN 5-minute window —
-- redo falls out of the same mechanism recursively, with no special case.

CREATE TABLE undo_log (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             text NOT NULL,          -- 'renewal' | 'topup' | 'invoice_void' | 'expense_void' | 'undo_reversal'
    entity_type      text NOT NULL,          -- 'subscriber' | 'manager' | 'invoice' | 'expense'
    entity_id        uuid NOT NULL,
    actor_manager_id uuid REFERENCES managers(id),
    ledger_tx_id     uuid REFERENCES ledger_transactions(id),
    journal_entry_id uuid REFERENCES journal_entries(id),
    -- snapshot carries whatever the four refusal checks need: the record's
    -- state right after the action, so "it changed since" is a plain
    -- comparison against current state, not a re-derivation.
    snapshot         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    undone_at        timestamptz,
    undo_reversal_ledger_tx_id  uuid REFERENCES ledger_transactions(id),
    undo_reversal_journal_entry_id uuid REFERENCES journal_entries(id),
    -- Set when THIS row is itself the undo of an earlier row — the "redo"
    -- chain.
    undo_of          uuid REFERENCES undo_log(id)
);
CREATE INDEX undo_log_entity_idx ON undo_log (entity_type, entity_id);
CREATE INDEX undo_log_actor_idx  ON undo_log (actor_manager_id, created_at DESC);
CREATE INDEX undo_log_expires_idx ON undo_log (expires_at) WHERE undone_at IS NULL;
