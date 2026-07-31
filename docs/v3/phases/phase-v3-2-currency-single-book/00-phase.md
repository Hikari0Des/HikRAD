# Phase v3-2 — Single-Book IQD Currency Redesign

> Kicked off 2026-07-23 (PRD Decision 54). SOLO, sequential. Owned by **sub-PRD 05** (money core). FR-100/101/102. **Migration 0595.** Reverses v2-4's per-`(manager, currency)` wallet model and **supersedes named v2-4 clauses** (listed below). This phase performs an **irreversible money migration** — the algorithm here is the reviewable, owner-approved plan.

## Owner decisions (locked at kickoff, 2026-07-23)
1. **Plans are IQD-only** — `profiles.currency` retired; existing non-IQD plans convert once at upgrade.
2. **Rounding = round-half-up to whole IQD** on every conversion (IQD has no minor units).
3. **Portal shows IQD only** — no foreign equivalents.
4. Upgrade = **auto-convert** (Decision 47b), refusing on any missing rate.

## v2-4 clauses explicitly superseded
- **FR-69.2** (per-`(manager,currency)` balances) → one IQD balance (FR-100.1).
- **FR-69.3** (`manager.exchange`) → retired (FR-100.3).
- **FR-69.1** (per-currency renewal math) → IQD-only renewals (FR-101.3).
- **FR-70.2/70.3** (per-currency reports + default-display-currency) → single IQD book (FR-101.5).
- Not superseded, retained: `currencies`/`currency_rates` catalog + rate CRUD (FR-68.1/68.3/68.4), `formatMoney` (FR-70.1), the ledger immutability core (FR-24), refund-reverses-original (FR-69.5, preserved in IQD).

## Migration 0595 — exact algorithm (append-only, refuse-on-missing-rate)

Runs in one transaction (golang-migrate wraps each file); any `RAISE EXCEPTION` rolls the whole file back and stops the upgrade.

```
-- 0. Schema: the conversion stamp.
ALTER TABLE ledger_transactions
  ADD COLUMN entered_amount   bigint,
  ADD COLUMN entered_currency text REFERENCES currencies(code);

-- 1. REFUSAL GATE (before any write): every currency that appears in a row we
--    must convert MUST have a from->IQD rate. Abort naming the first missing one.
--    Sources checked: manager_balances, manager_low_balance_thresholds,
--    profiles, profile_cost_history — each WHERE currency <> 'IQD'.
--    (A helper picks the latest currency_rates row by effective_from per pair.)
FOR each distinct foreign currency C in (those tables) LOOP
  IF NOT EXISTS (currency_rates WHERE from_currency=C AND to_currency='IQD')
     THEN RAISE EXCEPTION 'v3-2: no IQD rate for %, cannot convert — enter a rate first', C;
END LOOP;

-- 2. manager_balances -> IQD (append-only conversion pair per non-zero foreign row).
FOR each (manager_id, currency C <> 'IQD', balance B) LOOP
  rate := latest currency_rates(C -> IQD);
  iqd  := round( (B::numeric / 10^digits(C)) * rate );           -- round-half-up
  IF B <> 0 THEN
    INSERT ledger (type='exchange', amount=-B, currency=C,   actor=manager_id,
                   source='migration', reference='UPGRADE-'||manager_id,
                   currency_rate_id=rate.id, note='v3-2 single-book conversion');
    INSERT ledger (type='exchange', amount=+iqd, currency='IQD', actor=manager_id,
                   source='migration', reference='UPGRADE-'||manager_id,
                   currency_rate_id=rate.id,
                   entered_amount=B, entered_currency=C,
                   note='v3-2 single-book conversion');
  END IF;
  INSERT audit_log (action='manager.balance_converted_v3_2', entity='manager',
                    entity_id=manager_id, after=jsonb{from:C, amount:B, iqd:iqd, rate_id:rate.id});
END LOOP;
-- Rebuild IQD cache = sum(ledger.amount WHERE currency='IQD') per manager;
-- delete every manager_balances row WHERE currency <> 'IQD'.

-- 3. thresholds -> IQD (value conversion, no ledger — thresholds aren't money moved).
UPDATE manager_low_balance_thresholds SET threshold = round(threshold_whole * rate), currency='IQD'
  WHERE currency <> 'IQD';   -- then collapse: one IQD row per manager (0534 made it per-currency).

-- 4. profiles -> IQD (owner: IQD-only plans).
UPDATE profiles SET price = round((price::numeric/10^digits(currency))*rate), currency='IQD'
  WHERE currency <> 'IQD';

-- 5. profile_cost_history -> IQD (future margin stamps in IQD; historical cost_at_sale on
--    past ledger rows is left as-is — that is history).
UPDATE profile_cost_history SET cost = round((cost::numeric/10^digits(currency))*rate), currency='IQD'
  WHERE currency <> 'IQD';
```

Notes:
- **`digits(C)`** = `currencies.minor_unit_digits` (IQD 0, USD/EUR 2). IQD target has 0 digits, so the IQD result is whole-currency already.
- **Rounding**: `round(x)` in Postgres is round-half-away-from-zero for `numeric`; amounts are non-negative, so that IS round-half-up. Documented.
- **History untouched**: existing `ledger_transactions`/`payments`/`voucher_batches` rows keep their currency; only *new* writes are IQD (code-enforced).
- **Reconciliation (gate)**: after the migration, for each manager `new_IQD_balance == Σ_C round((old_balance_C/10^digits(C))×rate_C)`; every deleted foreign balance row has a matching `UPGRADE-` pair. Proven on a restored realistic dataset.

## Frozen contracts (code)

### C1. Ledger + balances (FR-100)
- `internal/billing/ledger.go`: `insertLedger` forces `Currency='IQD'` for new rows and accepts optional `EnteredAmount *int64` / `EnteredCurrency string` (the stamp). `lockBalance`/`recomputeBalance` drop the currency parameter (single IQD row). Balance readers query the IQD row only.
- Retire the exchange path (`balance_api.go` `exchangeHandler`/`exchange`, the route, `manager.exchange` audit). Keep `'exchange'` in the type CHECK (history + migration).

### C2. Entry-time conversion (FR-101)
- A single `convertToIQD(ctx, tx, amount, fromCurrency) (iqd int64, rateID string, err error)` helper in billing: looks up the latest `from→IQD` rate, applies `round-half-up`, returns the IQD amount + rate id; `fromCurrency=='IQD'` is a no-op (rateID=""). Used by top-up and any foreign-accepting money entry.
- Top-up (`balance_api.go`): accepts `currency` + optional; converts to IQD, stamps `entered_*`. Renewals/vouchers charge IQD (plans are IQD now). Refund (`refund.go`): unchanged in shape — it already reverses the original row's stored `amount` (now IQD); receipt reads the stamp for the original figure.

### C3. Plans IQD-only (FR-101.3)
- `internal/profiles` + `billing` renewal: `profiles.currency` no longer written (defaults IQD); the profile form drops the currency selector; cost history entry is IQD.

### C4. Reports / monitor / frontend (FR-101.5)
- `reports/settlement.go`, `monitorsvc/dashboard_api.go` + `conditions.go` (low-balance alert): drop per-currency grouping → IQD.
- Panel: `/currency-rates` keeps the **rate table** (entry) but the **exchange form is removed**; balance/ledger/topup/receipt render IQD; profile form loses the currency picker; receipts show the original foreign figure + rate when the row carries a stamp. Portal: IQD only.
- `formatMoney` stays; all money call sites pass `'IQD'`; `formatIQD` byte-identical (regression test intact).

## Integration gate
1. **Migration rehearsal (HARD gate, execution-plan rule 4)** — apply 0595 to a copied realistic dataset with USD/EUR balances; written reconciliation (per-manager IQD == Σ converted); refusal path proven (drop a rate → migration aborts naming the currency, nothing written).
2. **Ledger/balance** — cache≡ledger property test updated to single IQD book; new entries are IQD; `insertLedger` rejects/normalizes a non-IQD new write; the exchange route is gone (404).
3. **Entry-time conversion** — a USD top-up lands one IQD ledger row carrying `entered_amount/entered_currency/currency_rate_id`; its receipt shows both; a refund of it returns exactly its IQD amount regardless of any later rate change, and the receipt shows the original USD figure.
4. **Plans IQD** — a renewal charges IQD; the profile form has no currency selector; margin stamps are IQD.
5. **Reports/monitor** — settlement + dashboard balance + low-balance alert are single-currency IQD; no per-currency blending.
6. **No-regression** — v2-4/v2-9 gate tests updated **deliberately** (a reviewed diff per changed assertion), never silently deleted; `formatIQD` regression test green; backend build/vet/test green; frontend lint/build/test/i18n:check + v2-12 CI floors green.

## Documented-pending
The migration rehearsal + reconciliation requires a realistic dataset + a Postgres (self-skipping DB tests in-session); the write-up is produced when a pilot backup is available, per the never-probe-production rule. Standing unrelated: `cmd/hikrad-updaterd`'s two rollback tests (pre-existing).
