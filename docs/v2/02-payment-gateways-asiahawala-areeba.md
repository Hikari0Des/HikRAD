# v2-02 — Payment Gateway Adapters: AsiaHawala (Asiacell) + Areeba

> Deferred to v2 by master PRD Decision 24 (2026-07-11). Unlike v2-01 this needs **zero core changes** — the Phase-4 `PaymentGateway` interface (phase-4 contract C3) was designed exactly for this. It is v2 only because it's blocked on merchant accounts/API documentation, and v1 already ships ZainCash/FastPay/Qi slots plus the mock adapter.

## 1. Scope

Two new adapter packages behind the existing interface, no interface changes:

- **AsiaHawala** — Asiacell's e-wallet ("Asiacell payments" in market terms).
- **Areeba** — card/payment processor operating in Iraq (Mastercard/Visa acceptance); brings card payments, not just wallets.

Each adapter = one package under `backend/internal/billing/gateways/<name>/` implementing:

```go
type PaymentGateway interface {
    Name() string
    CreatePayment(ctx, Intent) (redirectURL string, gatewayRef string, err error)
    VerifyCallback(ctx, *http.Request) (CallbackResult, error) // signature-verified, idempotent
    QueryStatus(ctx, gatewayRef string) (State, error)
}
```

Everything else already exists from Phase 4: intent lifecycle/state machine, idempotent callback processing, reconciliation worker, renewal convergence (source `portal-<gateway>`), settings-driven enable/config (creds encrypted), portal gateway list UI, graceful NFR-7 degradation, FR-55 receipt notifications.

## 2. Per-adapter definition of done

1. Official API spec obtained + merchant/sandbox credentials (the actual blocker — start here).
2. Adapter package: create-payment (amount IQD, redirect/app handoff per gateway), callback signature verification + amount cross-check, status query; unit tests against recorded/fake responses; sandbox notes in `billing/gateways/<name>/README.md` including redirect/callback hosts (input to the walled-garden list, phase-4 C8-adjacent rule from agent-3 task 8).
3. Config: gateway row in `gateway_configs`, settings UI picks it up automatically (Phase-5 settings screen is data-driven per gateway).
4. Gate (mirrors phase-4 gate item 2): full lifecycle in sandbox — create → redirect → callback (replayed 3× = one renewal) → success; stuck-pending reconciled via QueryStatus; disabled/unreachable → portal message + voucher path (NFR-7).
5. Ship disabled-by-default; enabling is an ISP settings action.

## 3. AI kickoff prompt (paste into a fresh Claude Code session at repo root; run once per gateway)

```text
You are working in the HikRAD repo. v1 is complete. Implement a new payment gateway adapter: <AsiaHawala | Areeba>.

Read only: CLAUDE.md, docs/v2/02-payment-gateways-asiahawala-areeba.md, docs/prd/05-billing-payments-vouchers.md FR-23 section, docs/phases/phase-4-portal-payments-pwa/00-phase.md contract C3, backend/internal/billing/gateways/mock/ (the reference implementation) and one shipped live adapter if present (e.g. zaincash).

I will provide the gateway's API documentation and sandbox credentials in this conversation — ask me for them now if not attached; do not invent endpoint shapes or signature schemes from memory.

Then: (1) add a one-line FR-23 note + Decision row to docs/PRD.md recording the new adapter; (2) implement the adapter package under backend/internal/billing/gateways/<name>/ per the interface — no changes outside that package except the gateway registry entry and locale strings for the portal gateway label (en/ar/ku); (3) unit tests with recorded/fake HTTP responses covering signature verification, tampered-amount rejection, callback replay idempotency, and QueryStatus states; (4) README.md documenting sandbox setup + redirect/callback hosts for the walled-garden list; (5) run the mock-gateway gate script legs plus your new tests; (6) report the phase-4-gate-item-2 checklist result against the sandbox, and stop — do not enable the gateway in any config.
```
