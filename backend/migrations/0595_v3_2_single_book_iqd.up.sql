-- 0595_v3_2_single_book_iqd — Phase v3-2 (FR-100/101/102, PRD Decision 54),
-- sub-PRD 05. Reverses v2-4's per-(manager, currency) wallet model to a SINGLE
-- IQD book. This is the irreversible money migration; the exact algorithm is
-- frozen in docs/v3/phases/phase-v3-2-currency-single-book/00-phase.md.
--
-- Runs as one transaction (golang-migrate wraps each file), so any RAISE
-- EXCEPTION below rolls the WHOLE upgrade back — never a partial conversion.
--
-- Owner decisions (locked 2026-07-23): IQD-only plans; round-half-up to whole
-- IQD; portal IQD-only; auto-convert existing balances (refuse on missing rate).

-- 0. The conversion stamp: a converted row remembers its original figure so
--    receipts/audit can always show both (FR-101.1).
ALTER TABLE ledger_transactions
    ADD COLUMN entered_amount   bigint,
    ADD COLUMN entered_currency text REFERENCES currencies(code);

-- 1-3. Refusal gate + balance conversion (append-only pair) + IQD cache rebuild.
DO $$
DECLARE
    missing   text;
    b         RECORD;
    v_rate    numeric;
    v_rate_id uuid;
    v_digits  int;
    v_iqd     bigint;
BEGIN
    -- Refusal gate (FR-102.3): every foreign currency present in a table we must
    -- convert needs a from->IQD rate. Abort the whole upgrade naming the first
    -- missing one — no silent 1:1, no partial conversion.
    SELECT c INTO missing
    FROM (
        SELECT DISTINCT currency AS c FROM manager_balances               WHERE currency <> 'IQD'
        UNION SELECT DISTINCT currency FROM manager_low_balance_thresholds WHERE currency <> 'IQD'
        UNION SELECT DISTINCT currency FROM profiles                       WHERE currency <> 'IQD'
        UNION SELECT DISTINCT currency FROM profile_cost_history           WHERE currency <> 'IQD'
    ) t
    WHERE NOT EXISTS (
        SELECT 1 FROM currency_rates WHERE from_currency = t.c AND to_currency = 'IQD'
    )
    LIMIT 1;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'v3-2 single-book upgrade: no IQD exchange rate defined for %, enter one before upgrading', missing;
    END IF;

    -- Balances -> IQD via an append-only conversion pair (FR-102.2): a foreign
    -- debit that zeroes the wallet + an IQD credit at the current rate, sharing
    -- one UPGRADE- reference, both stamping the rate, the credit also stamping
    -- the original amount/currency. History (the original credits) is netted to
    -- zero, never edited.
    FOR b IN SELECT manager_id, currency, balance FROM manager_balances WHERE currency <> 'IQD' LOOP
        SELECT rate, id INTO v_rate, v_rate_id
        FROM currency_rates
        WHERE from_currency = b.currency AND to_currency = 'IQD'
        ORDER BY effective_from DESC
        LIMIT 1;

        SELECT minor_unit_digits INTO v_digits FROM currencies WHERE code = b.currency;
        -- round() on numeric is round-half-away-from-zero; balances are >= 0, so
        -- that IS round-half-up (owner decision). IQD has 0 minor digits.
        v_iqd := round((b.balance::numeric / power(10, v_digits)) * v_rate);

        IF b.balance <> 0 THEN
            INSERT INTO ledger_transactions
                (type, amount, currency, actor_manager_id, source, reference, currency_rate_id, note)
            VALUES
                ('exchange', -b.balance, b.currency, b.manager_id, 'migration',
                 'UPGRADE-' || b.manager_id, v_rate_id, 'v3-2 single-book conversion');
            INSERT INTO ledger_transactions
                (type, amount, currency, actor_manager_id, source, reference, currency_rate_id, entered_amount, entered_currency, note)
            VALUES
                ('exchange', v_iqd, 'IQD', b.manager_id, 'migration',
                 'UPGRADE-' || b.manager_id, v_rate_id, b.balance, b.currency, 'v3-2 single-book conversion');
            INSERT INTO audit_log (action, entity_type, entity_id, after)
            VALUES ('manager.balance_converted_v3_2', 'manager', b.manager_id::text,
                    jsonb_build_object('from_currency', b.currency, 'from_amount', b.balance,
                                       'iqd_amount', v_iqd, 'currency_rate_id', v_rate_id));
        END IF;
    END LOOP;

    -- Rebuild the IQD cache for every manager touched (sum of their IQD ledger,
    -- now including the new credits), then drop the emptied foreign rows.
    INSERT INTO manager_balances (manager_id, currency, balance)
        SELECT actor_manager_id, 'IQD', COALESCE(sum(amount), 0)
        FROM ledger_transactions
        WHERE currency = 'IQD'
          AND actor_manager_id IN (SELECT DISTINCT manager_id FROM manager_balances WHERE currency <> 'IQD')
        GROUP BY actor_manager_id
    ON CONFLICT (manager_id, currency) DO UPDATE
        SET balance = EXCLUDED.balance, updated_at = now();

    DELETE FROM manager_balances WHERE currency <> 'IQD';
END $$;

-- 4. Low-balance thresholds -> IQD. Adopt the converted foreign threshold only
--    if the manager has no explicit IQD threshold already (DO NOTHING), then
--    drop the foreign rows.
INSERT INTO manager_low_balance_thresholds (manager_id, currency, threshold)
SELECT t.manager_id, 'IQD',
       round((t.threshold::numeric / power(10, c.minor_unit_digits)) * r.rate)
FROM manager_low_balance_thresholds t
JOIN currencies c ON c.code = t.currency
JOIN LATERAL (
    SELECT rate FROM currency_rates
    WHERE from_currency = t.currency AND to_currency = 'IQD'
    ORDER BY effective_from DESC LIMIT 1
) r ON true
WHERE t.currency <> 'IQD'
ON CONFLICT (manager_id, currency) DO NOTHING;

DELETE FROM manager_low_balance_thresholds WHERE currency <> 'IQD';

-- Latest from->IQD rate per source currency. Factored out as a plain derived
-- table (not a LATERAL correlated to the UPDATE target) because Postgres forbids
-- a LATERAL item in an UPDATE's FROM clause from referencing the target table
-- (SQLSTATE 42P10 — a plan-time error, so it fires even with zero foreign rows).
-- DISTINCT ON (from_currency) ... ORDER BY effective_from DESC reproduces the
-- old per-row "latest rate" LIMIT 1 exactly.

-- 5. Plans -> IQD (owner: IQD-only plans, FR-101.3).
UPDATE profiles p
SET price = round((p.price::numeric / power(10, c.minor_unit_digits)) * r.rate),
    currency = 'IQD'
FROM currencies c
JOIN (
    SELECT DISTINCT ON (from_currency) from_currency, rate
    FROM currency_rates
    WHERE to_currency = 'IQD'
    ORDER BY from_currency, effective_from DESC
) r ON r.from_currency = c.code
WHERE p.currency <> 'IQD' AND c.code = p.currency;

-- 6. Plan cost history -> IQD (future margin stamps in IQD; historical
--    cost_at_sale stamps on past ledger rows are left as-is — that is history).
UPDATE profile_cost_history h
SET cost = round((h.cost::numeric / power(10, c.minor_unit_digits)) * r.rate),
    currency = 'IQD'
FROM currencies c
JOIN (
    SELECT DISTINCT ON (from_currency) from_currency, rate
    FROM currency_rates
    WHERE to_currency = 'IQD'
    ORDER BY from_currency, effective_from DESC
) r ON r.from_currency = c.code
WHERE h.currency <> 'IQD' AND c.code = h.currency;
