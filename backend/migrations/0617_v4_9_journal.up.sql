-- v4-9 (FR-153, §3.1): the journal substrate itself. STAGED — added beside
-- ledger_transactions, not instead of it (0618 backfills history; the
-- ledger is never rewritten). Every money action posts here going forward
-- from journal_hooks.go, inside the SAME transaction as its ledger write, so
-- the two can never diverge even under a crash mid-write.
--
-- Binding invariant (acceptance gate item 1): entries balance OR ARE
-- REFUSED, enforced by a DB constraint, not by convention.

CREATE TABLE journal_entries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    at           timestamptz NOT NULL DEFAULT now(),
    memo         text NOT NULL DEFAULT '',
    source       text NOT NULL DEFAULT '',   -- e.g. billing.renew / billing.topup / billing.invoice
    reference    text NOT NULL DEFAULT '',
    -- Links a live-posted entry back to the ledger row it dual-writes beside
    -- (§3.1's reconciliation join key); NULL for entries with no ledger
    -- counterpart (invoices, expenses, depreciation — new territory the
    -- legacy ledger was never designed to hold, per §4's frozen contract:
    -- the existing ledger/receipts/refunds/tickets gain a journal posting,
    -- nothing else — new money concepts post to the journal only).
    ledger_tx_id uuid REFERENCES ledger_transactions(id),
    reverses_id  uuid REFERENCES journal_entries(id),
    created_by   uuid REFERENCES managers(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX journal_entries_at_idx         ON journal_entries (at DESC);
CREATE INDEX journal_entries_ledger_tx_idx  ON journal_entries (ledger_tx_id);
CREATE UNIQUE INDEX journal_entries_reverses_uniq
    ON journal_entries (reverses_id) WHERE reverses_id IS NOT NULL;

CREATE TABLE journal_lines (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id      uuid NOT NULL REFERENCES journal_entries(id),
    account_id    uuid NOT NULL REFERENCES accounts(id),
    debit         bigint NOT NULL DEFAULT 0 CHECK (debit >= 0),
    credit        bigint NOT NULL DEFAULT 0 CHECK (credit >= 0),
    memo          text NOT NULL DEFAULT '',
    -- Optional subledger dimension: a control account (Agent Prepaid
    -- Balances, Accounts Receivable, Accounts Payable) nets to one figure on
    -- the balance sheet but is really the sum of many per-party balances —
    -- exactly how manager_balances already works per agent. Tagging the
    -- party on the line (never a new column per account, just these two
    -- generic FKs) is what makes "the journal and the ledger agree on every
    -- manager's balance, not just the grand total" reconcilable at all
    -- (gate item 3) without denormalizing party balances onto accounts
    -- itself. NULL on every line that isn't posted against a party-specific
    -- control account (revenue, COGS, cash, ...).
    manager_id    uuid REFERENCES managers(id),
    subscriber_id uuid REFERENCES subscribers(id),
    -- One side per line — standard double-entry discipline; a "0/0" or
    -- "both sides" line is refused outright, before the entry-level balance
    -- check even runs.
    CHECK ( (debit = 0 AND credit > 0) OR (debit > 0 AND credit = 0) )
);
CREATE INDEX journal_lines_entry_idx   ON journal_lines (entry_id);
CREATE INDEX journal_lines_account_idx ON journal_lines (account_id);
CREATE INDEX journal_lines_manager_idx ON journal_lines (manager_id) WHERE manager_id IS NOT NULL;

-- Balanced-or-refused, enforced at the DB level (gate item 1). A DEFERRED
-- constraint trigger lets one entry's lines be inserted across several
-- statements inside a transaction, but the transaction cannot COMMIT (and an
-- explicit `SET CONSTRAINTS journal_lines_balanced IMMEDIATE` cannot
-- succeed) while any entry's debit sum != credit sum.
CREATE OR REPLACE FUNCTION journal_entry_balanced() RETURNS trigger AS $$
DECLARE
    eid uuid;
    d   bigint;
    c   bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        eid := OLD.entry_id;
    ELSE
        eid := NEW.entry_id;
    END IF;
    SELECT COALESCE(sum(debit), 0), COALESCE(sum(credit), 0) INTO d, c
      FROM journal_lines WHERE entry_id = eid;
    IF d <> c THEN
        RAISE EXCEPTION 'journal entry % is unbalanced: debit % <> credit %', eid, d, c
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER journal_lines_balanced
    AFTER INSERT OR UPDATE OR DELETE ON journal_lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION journal_entry_balanced();

-- A "balanced" zero- or one-line entry would trivially pass the sum check
-- above, so it is refused separately: a real double-entry posting always has
-- at least two legs.
CREATE OR REPLACE FUNCTION journal_entry_min_lines() RETURNS trigger AS $$
DECLARE
    n int;
BEGIN
    SELECT count(*) INTO n FROM journal_lines WHERE entry_id = NEW.entry_id;
    IF n < 2 THEN
        RAISE EXCEPTION 'journal entry % must have at least 2 lines', NEW.entry_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER journal_lines_min_two
    AFTER INSERT ON journal_lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION journal_entry_min_lines();

-- Append-only, same discipline as ledger_transactions/payments/audit_log
-- (AC-24a's pattern, applied here per §4: "append-only" is a named frozen
-- contract for this table too). A reversal is a NEW entry (reverses_id),
-- never an edit of the original.
CREATE OR REPLACE FUNCTION journal_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only: % is not permitted', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER journal_entries_no_update BEFORE UPDATE ON journal_entries FOR EACH ROW EXECUTE FUNCTION journal_immutable();
CREATE TRIGGER journal_entries_no_delete BEFORE DELETE ON journal_entries FOR EACH ROW EXECUTE FUNCTION journal_immutable();
REVOKE UPDATE, DELETE ON journal_entries FROM PUBLIC;

CREATE TRIGGER journal_lines_no_update BEFORE UPDATE ON journal_lines FOR EACH ROW EXECUTE FUNCTION journal_immutable();
CREATE TRIGGER journal_lines_no_delete BEFORE DELETE ON journal_lines FOR EACH ROW EXECUTE FUNCTION journal_immutable();
REVOKE UPDATE, DELETE ON journal_lines FROM PUBLIC;
