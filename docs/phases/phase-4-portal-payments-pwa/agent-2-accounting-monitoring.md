# Phase 4 — Agent 2 (Accounting & Monitoring): Web Push channel, expiring digest, usage API polish

> Owns FR-54.4 (backend), FR-36.3 completion. Depends on contracts in [00-phase.md](00-phase.md) (C1-C, C4); parallel with Agents 1, 3, 4.

## Mission & context
The PWAs need a push backend: standard Web Push (VAPID, no third-party service — NFR-7-compatible: pushes fail gracefully offline) as a fourth alert channel for the panel surface, plus the subscriber-facing expiring-soon reminder. You also polish the usage APIs the portal consumes (per-subscriber scoping and monthly granularity edge cases). Detail sources: sub-PRDs [07](../../prd/07-subscriber-portal-pwa.md) FR-54.4, [03](../../prd/03-lossless-accounting-live-monitoring.md) FR-36.3.

## File ownership
- **Exclusive:** `backend/internal/push/**`, `backend/internal/monitorsvc/**`, `backend/internal/live/**`, `backend/migrations/0330_*.sql`–`0339_*.sql`.
- **Read-only:** settings (VAPID storage via A's platform), subscriber expiry data (D's, via existing stats/queries). **Forbidden:** `internal/{portalapi,billing,radius}`, `frontend/**`.

## Tasks
1. Migration 0330: `push_subscriptions` per phase C1-C.
2. `internal/push`: VAPID keypair generated into settings on first boot; subscribe/unsubscribe endpoints per C4 (panel: manager token; portal: subscriber token — auth context distinguishes surface); web-push sender with TTL, per-endpoint failure handling (410 Gone → prune subscription), payload shape `{title_key, body_key, params, url}` per C4 (client localizes). [FR-54.4]
3. Alerts engine: add `push` as a routable channel for panel-surface rules (Phase-3 routing schema already has channels jsonb — extend the enum); delivery isolation like other channels (a dead push endpoint never delays Telegram). [FR-36 extension]
4. Expiring-soon subscriber reminder: extend the `expiring_digest` rule type with per-subscriber portal-push targeting (subscriber's own expiry, N days, respecting their language for the notification key params). If gateway/scale complexity exceeds the phase, deliver panel-surface push only and record portal reminders as deferred (frozen fallback per C4 — decide by mid-phase, tell F either way).
4b. **Subscriber WhatsApp messaging** (FR-55, phase C7): consume D's `billing.renewed` event → send the `payment_receipt` template (Phase-3 WhatsApp sender reused; subscriber language; requires `whatsapp_opt_in` + valid phone); add WhatsApp as a target of the per-subscriber expiring reminder (same targeting as task 4's portal push). Delivery isolation as ever (a WhatsApp failure never touches push/Telegram/in-app); every send recorded. If Meta template approval is pending, prove the path against a request-capture fake and record it in the merge notes (gate item 9's fallback).
5. Usage API polish for portal: monthly granularity boundary correctness (Asia/Baghdad month edges), empty-history responses, per-subscriber scope guard (defense in depth behind D's token scoping), response-size caps.

Edge cases: push payload size limit (4 KB) — keys+params only, never rendered text; VAPID key rotation story documented (regenerating invalidates subscriptions — settings warning); duplicate subscriptions per endpoint deduped; iOS endpoints behave differently (feature-detect on client; backend treats uniformly).

## Contracts consumed/exposed
- **Consumes:** C4 shapes, Phase-3 alert engine (own), A's settings, auth contexts (A/D token middlewares).
- **Exposes:** subscribe endpoints + push channel (F's PWA work), pruned-subscription semantics, usage endpoints (F's graphs).

## Definition of done
- Gate items 5 and 9 pass (NAS-down → panel PWA push on Android; WhatsApp receipt + reminder delivered or fake-proven per C7); portal expiring reminder demonstrated or its deferral recorded in the phase merge notes.
- Tests: subscription lifecycle incl. 410 pruning, channel isolation, payload key/param encoding, month-boundary usage math, VAPID bootstrap idempotence.

## Handoff
Phase 5 (same role) focuses on chaos/perf evidence; push is feature-complete. F receives working push + usage APIs; support docs get the VAPID rotation note.
