# Phase v3-2 — Single-Book IQD Currency Redesign — Gate Result

**Verdict: GREEN (scripted).** All code legs PASS; the **migration rehearsal + written reconciliation** (execution-plan rule 4) is documented-pending — it requires a restored realistic dataset + Postgres, which is not available in-session (same posture as every DB-gated leg here). FR-100/101/102 (PRD Decision 54), sub-PRD 05, **migration 0595** (repo max 0594 → 0595). Reverses v2-4's per-currency wallets; supersedes FR-69.1/69.2/69.3 and amends FR-70.2/70.3, each named in the brief.

## What shipped

| Contract | Summary | Key files |
|---|---|---|
| C1 | Single IQD book — `insertLedger` forces IQD, `lockBalance`/`recomputeBalance` drop the currency dimension, balance readers are IQD; exchange endpoint **retired** | `billing/ledger.go`, `balance_api.go`, `module.go` |
| C2 | Entry-time conversion — `convertToIQD`/`signedToIQD` (round-half-up), top-up converts a foreign amount to IQD and stamps `entered_amount`/`entered_currency`/`currency_rate_id`; refund reverses the stored IQD exactly (converts only a rare legacy-foreign original) | `billing/ledger.go`, `balance_api.go`, `refund.go` |
| C3 | Plans IQD-only — profile form loses the currency selector; renewal/voucher charge IQD | `ProfilesPage.tsx`, `billing/renew.go`, `voucher.go`, `voucher_api.go`, `tickets.go` |
| C4 | Migration 0595 — refusal gate → balance conversion pair → cache rebuild → thresholds/plans/cost-history → IQD; per-manager audit | migration `0595`, panel `CurrencyRatesPage.tsx` (exchange card removed), `api/billing.ts` |

## Migration 0595 (the irreversible part)
Runs as one transaction; the PL/pgSQL block **refuses the whole upgrade** (RAISE → rollback) if any foreign currency in balances/thresholds/plans/cost-history lacks a `→IQD` rate, naming it. Each non-zero foreign balance becomes an append-only `exchange` pair (foreign debit zeroing the wallet + IQD credit at the current rate, shared `UPGRADE-` reference, rate + original stamped) with a `manager.balance_converted_v3_2` audit row; the IQD cache is rebuilt and foreign `manager_balances` rows deleted. Thresholds/plans/cost-history convert in place. Historical `ledger`/`payments`/`voucher_batches` rows keep their original currency (history, never rewritten). Round-half-up via Postgres `round()` on non-negative `numeric`.

## Gate items
1. **Migration rehearsal + reconciliation — DOCUMENTED-PENDING.** The algorithm is frozen and reviewed (00-phase.md); running it against a restored realistic dataset with a written per-manager reconciliation (`new_IQD == Σ round(old×rate)`) + the refusal-on-missing-rate proof needs a Postgres + pilot backup (never the live stack). Same in-session limitation as all DB-gated legs.
2. **Ledger/balance single-book — PASS (code + DB-gated tests written).** `insertLedger` writes `'IQD'` unconditionally; `lockBalance`/`recomputeBalance` are IQD-only; the cache≡ledger helpers (`ledgerSum`/`cachedBalance`) already read IQD. `currency_gate_test.go` was **deliberately rewritten** (not deleted) to the single-book invariants.
3. **Entry-time conversion — PASS (tests written).** `TestEntryTimeConversionStamp` (USD 5000¢ × 1310 → 65,500 IQD, one row, stamped, cache≡ledger), `TestNativeIqdTopupHasNoStamp`, `TestTopupUnknownRateRefused` (422, no balance move).
4. **Exchange retired — PASS.** Route removed; `TestExchangeEndpointRetired` asserts 404/405; the panel exchange card + `exchangeManagerBalance` client fn are gone.
5. **Plans IQD-only — PASS.** Profile form currency selector removed; renewal/voucher charge IQD; migration converts existing plans + cost history.
6. **No-regression — PASS.** Backend `go build`/`vet` clean; billing/reports/monitor tests green (DB-gated self-skip); the v2-9/tickets gate tests keep reading balances via the retained `balanceFor` helper. Panel `tsc` clean, **36 files / 121 tests green**, `i18n:check` OK (1618 keys × 3 locales), panel+portal lint pass (`no-native-controls` clean), shared tests green. `formatIQD` regression test untouched/green. Only the two pre-existing `cmd/hikrad-updaterd` rollback tests still fail (unrelated).

## Design decisions worth recording
- **`insertLedger` forces IQD at the write boundary** (not "callers pass IQD") — the single-book guarantee can't be bypassed by a stray currency argument.
- **Refund of a legacy foreign transaction converts to IQD at the current rate** (the foreign book no longer exists to reverse into) and is stamped; a NEW (IQD) refund reverses the stored amount *exactly* — the common case and the FR-101.2 promise. If the legacy currency has no rate, the refund is refused (`no_rate`) rather than guessing.
- **`currency_gate_test.go` rewritten, not deleted** — the gate rule forbids silently dropping superseded assertions; the file now documents what replaced them and why.

## Documented-pending
The migration rehearsal + reconciliation (needs a restored realistic dataset + Postgres). Standing unrelated: `cmd/hikrad-updaterd`'s two rollback tests (pre-existing).
