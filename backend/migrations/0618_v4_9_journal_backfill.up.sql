-- v4-9 (FR-153, §3.1): backfill. Projects every EXISTING ledger_transactions
-- row into a balanced journal entry, using the identical mapping rules
-- journal.go's postLedgerJournal implements for every NEW row going forward
-- (re-expressed here in PL/pgSQL, since a migration cannot call Go) — kept
-- in lockstep by the dual-write reconciliation test
-- (journal_reconciliation_db_test.go), which is what actually proves the two
-- never drifted apart, not this comment.
--
-- The existing ledger is NEVER rewritten here — every statement below is an
-- INSERT into the new journal_entries/journal_lines tables; not one UPDATE
-- or DELETE touches ledger_transactions, payments, or manager_balances.
--
-- Idempotent by construction (NOT EXISTS on ledger_tx_id) so a partial
-- re-run — or running this on a database where some rows already backfilled
-- some other way — never double-posts.

CREATE TABLE journal_backfill_report (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_at                 timestamptz NOT NULL DEFAULT now(),
    ledger_rows_seen       int NOT NULL,
    entries_created        int NOT NULL,
    rows_skipped_no_event  int NOT NULL,
    total_ledger_iqd       bigint NOT NULL, -- sum(ledger_transactions.amount) across every IQD row
    total_journal_wallet_net bigint NOT NULL -- sum(credit-debit) on the 2100 Agent Prepaid Balances account
);

DO $$
DECLARE
    l               RECORD;
    p_amount        bigint;
    entry_id        uuid;
    acct_cash       uuid;
    acct_wallet     uuid;
    acct_cogs       uuid;
    acct_suspense   uuid;
    acct_revenue    uuid;
    acct_capital    uuid;
    n_seen          int := 0;
    n_entries       int := 0;
    n_skipped       int := 0;
    v_ledger_total  bigint;
    v_wallet_net    bigint;
BEGIN
    SELECT id INTO acct_cash     FROM accounts WHERE code = '1000';
    SELECT id INTO acct_wallet   FROM accounts WHERE code = '2100';
    SELECT id INTO acct_cogs     FROM accounts WHERE code = '5000';
    SELECT id INTO acct_suspense FROM accounts WHERE code = '2900';
    SELECT id INTO acct_revenue  FROM accounts WHERE code = '4000';
    SELECT id INTO acct_capital  FROM accounts WHERE code = '3000';

    FOR l IN
        SELECT id, at, type, amount, actor_manager_id, cost_at_sale, reference, note
          FROM ledger_transactions
         WHERE currency = 'IQD'
           AND NOT EXISTS (SELECT 1 FROM journal_entries je WHERE je.ledger_tx_id = ledger_transactions.id)
         ORDER BY at ASC, id ASC
    LOOP
        n_seen := n_seen + 1;
        entry_id := NULL;
        SELECT amount INTO p_amount FROM payments WHERE ledger_tx_id = l.id LIMIT 1;

        IF l.type = 'topup' THEN
            IF l.amount <> 0 THEN
                INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id)
                VALUES (l.at, COALESCE(NULLIF(l.note, ''), l.type), 'billing.backfill', l.reference, l.id)
                RETURNING id INTO entry_id;
                INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                    VALUES (entry_id, acct_cash, l.amount, 0, 'topup');
                INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id)
                    VALUES (entry_id, acct_wallet, 0, l.amount, 'topup', l.actor_manager_id);
            END IF;

        ELSIF l.type = 'exchange' THEN
            -- Historical only (the exchange endpoint was retired in v3-2):
            -- the IQD leg of a currency revaluation, no real revenue/cash
            -- behind it. Dr/Cr Owner Capital vs Agent Prepaid Balances.
            IF l.amount <> 0 THEN
                INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id)
                VALUES (l.at, 'exchange (v3-2 legacy)', 'billing.backfill', l.reference, l.id)
                RETURNING id INTO entry_id;
                IF l.amount >= 0 THEN
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_capital, l.amount, 0, 'exchange');
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id)
                        VALUES (entry_id, acct_wallet, 0, l.amount, 'exchange', l.actor_manager_id);
                ELSE
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id)
                        VALUES (entry_id, acct_wallet, -l.amount, 0, 'exchange', l.actor_manager_id);
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_capital, 0, -l.amount, 'exchange');
                END IF;
            END IF;

        ELSIF l.type IN ('renewal', 'voucher_redeem', 'adjustment', 'refund', 'manual_payment', 'discount') THEN
            -- A wallet was actually moved (agent/reseller spent or was
            -- refunded prepaid credit): Dr/Cr Agent Prepaid Balances vs
            -- Subscription Revenue, signed by amount. The retail payment
            -- row, if any, is the agent's own collection from the
            -- subscriber in real life — never the owner's cash — so it is
            -- deliberately not booked here.
            IF l.actor_manager_id IS NOT NULL AND l.amount <> 0 THEN
                INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id)
                VALUES (l.at, COALESCE(NULLIF(l.note, ''), l.type), 'billing.backfill', l.reference, l.id)
                RETURNING id INTO entry_id;
                IF l.amount >= 0 THEN
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_revenue, l.amount, 0, l.type);
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id)
                        VALUES (entry_id, acct_wallet, 0, l.amount, l.type, l.actor_manager_id);
                ELSE
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id)
                        VALUES (entry_id, acct_wallet, -l.amount, 0, l.type, l.actor_manager_id);
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_revenue, 0, -l.amount, l.type);
                END IF;
            -- No wallet movement, but a real payment row exists: a direct
            -- sale/refund the owner's own till collected.
            ELSIF p_amount IS NOT NULL AND p_amount <> 0 THEN
                INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id)
                VALUES (l.at, COALESCE(NULLIF(l.note, ''), l.type), 'billing.backfill', l.reference, l.id)
                RETURNING id INTO entry_id;
                IF p_amount >= 0 THEN
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_cash, p_amount, 0, l.type);
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_revenue, 0, p_amount, l.type);
                ELSE
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_revenue, -p_amount, 0, l.type);
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_cash, 0, -p_amount, l.type);
                END IF;
            END IF;
            -- Neither leg applied: a true zero-value event (e.g. a free
            -- trial grant) — nothing to post, entry_id stays NULL.

            -- Cost-of-service allocation, matched in the same entry as
            -- whichever revenue leg (if any) just posted, or its own entry
            -- when the sale itself was a true zero-value event but somehow
            -- still carries a cost stamp (defensive; never actually
            -- happens given how cost_at_sale is only ever set alongside a
            -- real charge).
            IF l.cost_at_sale IS NOT NULL AND l.cost_at_sale <> 0 THEN
                IF entry_id IS NULL THEN
                    INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id)
                    VALUES (l.at, COALESCE(NULLIF(l.note, ''), l.type), 'billing.backfill', l.reference, l.id)
                    RETURNING id INTO entry_id;
                END IF;
                IF l.cost_at_sale >= 0 THEN
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_cogs, l.cost_at_sale, 0, 'cost of service');
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_suspense, 0, l.cost_at_sale, 'cost of service');
                ELSE
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_suspense, -l.cost_at_sale, 0, 'cost of service');
                    INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo)
                        VALUES (entry_id, acct_cogs, 0, -l.cost_at_sale, 'cost of service');
                END IF;
            END IF;
        END IF;

        IF entry_id IS NOT NULL THEN
            n_entries := n_entries + 1;
        ELSE
            n_skipped := n_skipped + 1;
        END IF;
    END LOOP;

    -- Reconciliation figures (the report gate item 2 asks for): the grand
    -- total across every manager's ledger-derived balance vs the journal's
    -- own net on the same control account. Scoped to actor_manager_id IS NOT
    -- NULL on the ledger side to match exactly what postLedgerJournal ever
    -- posts to the wallet account (a NULL-actor row is a direct cash sale
    -- that never touches 2100 on either side, by design — see the mapping
    -- comment above). A per-manager breakdown is the job of the Go-level
    -- reconciliation test (finer-grained and re-run on demand); this
    -- migration records the headline figures that must hold for the
    -- finer-grained check to even be possible.
    SELECT COALESCE(sum(amount), 0) INTO v_ledger_total
      FROM ledger_transactions WHERE currency = 'IQD' AND actor_manager_id IS NOT NULL;
    SELECT COALESCE(sum(credit - debit), 0) INTO v_wallet_net FROM journal_lines WHERE account_id = acct_wallet;

    INSERT INTO journal_backfill_report
        (ledger_rows_seen, entries_created, rows_skipped_no_event, total_ledger_iqd, total_journal_wallet_net)
    VALUES (n_seen, n_entries, n_skipped, v_ledger_total, v_wallet_net);

    RAISE NOTICE 'v4-9 backfill: % ledger rows seen, % journal entries created, % skipped (no economic event), wallet net % (ledger total %)',
        n_seen, n_entries, n_skipped, v_wallet_net, v_ledger_total;
END $$;
