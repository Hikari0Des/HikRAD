# Phase 3 — Agent 5 (Frontend Panel): dashboard, renew flow, billing UIs, security UIs, alerts UI

> Owns UI halves of FR-19–22/24/25, FR-27–30, FR-32/34/35/36, FR-39. Depends on contracts in [00-phase.md](00-phase.md) (C2, C3, C5, C6, C7); parallel with Agents 1–4.

## Mission & context
Turn the MVP backends into the ≤ 3-click operator experience (NFR-5): the Renew dialog on the user page (key flow 2), Omar's dashboard, Hassan's balance-aware agent view, voucher management, the roles/2FA/security screens, alerts and health pages. Personas: Sara (speed), Omar (glanceability, phone), Hassan (phone-first, balance always visible), Ali (health/debug depth). Detail sources: sub-PRDs [05](../../prd/05-billing-payments-vouchers.md) §5, [06](../../prd/06-managers-roles-security.md) §5, [03](../../prd/03-lossless-accounting-live-monitoring.md) §5.

## File ownership
- **Exclusive:** `frontend/panel/**`. **Read-only:** `frontend/shared/**`. **Forbidden:** portal, backend, deploy.

## Tasks
1. **Renew dialog** (FR-19, key flow 2): activate the Phase-2 slot — opens pre-selecting current profile + resolved price, optional profile switch, note; confirm → success state shows new expiry + CoA result (restored / had-to-disconnect / failed with retry) + receipt actions (print view in ar/en, share link). Idempotency key on submit. Whole flow ≤ 3 clicks post-search; measure and record. [NFR-5]
2. **Balances & top-up**: agent's own balance in the header (Hassan); manager list with balances; top-up dialog (permission-gated); low-balance badge. Insufficient-balance renewal error rendered helpfully.
3. **Ledger view** (FR-24): filter chips (manager/user/date/type/source), running-balance column when filtered to one manager, CSV export, reversing-entry linking (refund ↔ original visual pairing). **Refund flow** (FR-25) from a ledger row or payment history: reason required, consequences preview (expiry rollback).
4. **Vouchers** (FR-22): batch creation wizard (profile, count, prefix, expiry) → CSV download; batch list with used/unused/expired counts + drill-down; void-batch with confirm; operator redeem action on the user page.
5. **Dashboard** (FR-32): tile grid per C5 payload — online now + sparkline, subscriber tiles (each links to pre-filtered user list), revenue today (→ ledger), NAS cards (→ NAS status page), RADIUS rps, pipeline status; single-column phone layout; auto-refresh cadence per C5.
6. **Monitoring UIs**: per-NAS status page (probe history charts — LTR inside RTL, downtime log); **devices section** (FR-60, amendment 2026-07-11): monitored-device CRUD screen (name/IP/type/SNMP/location) + device status cards beside the NAS cards + per-device status page reusing the NAS status-page components — visually distinct from NASes (no RADIUS affordances); admin health page (FR-35: services, queue depth, **counter invariant badge**, disk); alerts: rules CRUD forms (type-specific threshold fields, channel routing incl. the `whatsapp` channel with per-rule recipient numbers, quiet hours), events list showing per-channel delivery results, in-app notification center fed by the notifications SSE + banner for critical (NAS down).
7. **Security UIs** (FR-27–30): manager CRUD with role assignment + scoped flag; roles matrix editor (modules × verbs grid + action perms — comprehensible to Omar); TOTP enrolment (QR, manual key, backup-codes download) + login TOTP step; sessions list with revoke; IP allowlist editor with self-lockout warning; audit-log viewer (human-readable rows from A's summary keys, expandable diff, filters, export).
8. **Debug tool UI** (FR-39): live tail view with username/NAS filter, reason badges localized, pause/scroll-lock.

Edge cases: renew dialog when the profile was archived mid-open (server 422 → refresh state); dashboard SSE/poll degradation offline; roles matrix on phone width (horizontal scroll container); TOTP QR must also show the secret for manual entry; notification center must not lose events across route changes.

## Contracts consumed/exposed
- **Consumes:** C2 renewal, C3 balances/vouchers/receipts, C5 dashboard/health/alerts, C6 debug SSE, C7 security endpoints — all frozen.
- **Exposes:** the complete MVP panel; Phase 5 (same role) adds reports/settings/wizard screens.

## Definition of done
- Gate items 1–3, 5–7 UI halves pass; the ≤ 3-click renew measurement documented.
- Component tests: renew dialog states (success/CoA-fallback/insufficient-balance/archived-profile), ledger pairing render, roles matrix editing, TOTP step, notification reducer.
- `i18n:check` green; en+ar complete, ku keys present; phone-width pass for dashboard + renew + balance screens (screenshots in PR).

## Handoff
Phase 5 (same role) receives a complete operational panel; only reports/settings/import/wizard UIs remain.
