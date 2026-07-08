# HikRAD — Sub-PRD 06: Managers, Roles & Security

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08. Owns: FR-27, FR-28, FR-29, FR-30 · NFR-4
> Depends on: [01-platform-install-licensing](01-platform-install-licensing.md) (API framework, TLS via Caddy, settings) · Depended on by: **all panel modules** — [02](02-radius-nas-aaa.md), [03](03-lossless-accounting-live-monitoring.md), [04](04-subscribers-profiles.md), [05](05-billing-payments-vouchers.md), [08](08-reports.md) consume permission checks, ownership scoping, and the audit log; [07](07-subscriber-portal-pwa.md) consumes password-storage and rate-limit policy.

## 1. Scope & context

Who can log into the panel and what they may do. This module owns manager accounts (admins, staff like **Sara**, field agents like **Hassan**), the granular permission model with per-manager user-ownership scoping (v1 is deliberately **flat** — the reseller tree is Phase 2), TOTP 2FA, login protection, panel session management, and the platform-wide security posture (NFR-4): password/secret storage rules, immutable audit log, OWASP ASVS L2 for the web layer.

## 2. Owned requirements — elaborated

### FR-27 (M) — Manager accounts with granular permissions & scoping
**Master:** Granular permission sets (per module: view/create/edit/delete; per action: renew, disconnect, top-up, export) and per-manager user-ownership scoping (a manager sees only their own users) — flat structure in v1; tree in Phase 2.

*Elaboration:*
- **FR-27.1** — Permission model: named **roles** (reusable sets) assigned to managers, plus per-manager overrides. Dimensions exactly as mastered: per module (subscribers, profiles, NAS, billing, vouchers, monitoring, reports, settings, managers) × view/create/edit/delete, plus action permissions `renew`, `disconnect`, `top-up`, `export`. Deny by default.
- **FR-27.2** — Ownership scoping: boolean `scoped` per manager; scoped managers see/act only on subscribers whose `owner_manager_id` is them — enforced **server-side in every query and mutation** (subscribers, sessions, ledger, reports), not by UI hiding. Admins are unscoped.
- **FR-27.3** — Built-in roles seeded: Admin (all), Operator (Sara: subscriber view/edit, renew, disconnect, no settings/managers), Agent (Hassan: scoped subscriber view, renew, own collection report). Editable copies, not hardcoded behavior.
- **FR-27.4** — Flat structure is a v1 contract: no manager-under-manager relations in the schema beyond subscriber ownership; Phase-2 tree must be additive (document the migration seam, don't build it).

### FR-28 (M) — 2FA, login protection, audit log
**Master:** TOTP two-factor authentication (optional per account, enforceable by admin); login rate-limiting and lockout; full audit log of every manager action (who/what/when/before-after).

*Elaboration:*
- **FR-28.1** — TOTP (RFC 6238): QR enrolment, backup codes (one-time, hashed), admin toggle "require 2FA" globally or per role; admins can reset a locked-out manager's 2FA (audit-logged).
- **FR-28.2** — Login protection: per-account and per-IP rate limits; progressive lockout (e.g. 5 failures → 15 min) with admin unlock; all auth events (success, failure, lockout) audit-logged. Portal login rate-limiting (NFR-4) uses the same mechanism via [07](07-subscriber-portal-pwa.md).
- **FR-28.3** — Audit log: append-only (same immutability discipline as the ledger, [05](05-billing-payments-vouchers.md) FR-24); entry = actor, action, entity type/id, before/after JSON diff, timestamp, IP, user-agent. **Contract:** every mutating endpoint in every module writes it — this module provides the one write API and the viewer UI (filter by actor/entity/date, permission-gated).

### FR-29 (M) — Panel session management
**Master:** Active sessions list, revoke.

*Elaboration:* short-lived access token + refresh token per [01](01-platform-install-licensing.md) FR-52.2; each login = a session record (device/user-agent, IP, created, last-seen); managers see their own sessions and revoke any; admins can revoke any manager's sessions (e.g. dismissed employee — takes effect within one access-token lifetime, ≤ 5 min). Password change and 2FA reset revoke all other sessions.

### FR-30 (S) — IP allowlist per manager
**Master:** IP allowlist per manager account.

*Elaboration:* optional CIDR list per manager; enforced at login and on every request; empty list = no restriction. UI warns when the editing admin's own current IP would be excluded on self-edit.

### NFR-4 (owned) — Security
**Master:** Passwords hashed (argon2id) — note CHAP/MS-CHAP support for PPPoE requires reversible storage of subscriber RADIUS passwords: store encrypted-at-rest (AES-GCM, key in server config) and document the tradeoff; TLS on all web surfaces (bundled reverse proxy + Let's Encrypt or self-signed); RADIUS secrets encrypted at rest; audit log immutable; OWASP ASVS L2 for the web layer; rate-limited portal login.

*Elaboration (platform-wide policy owned here, implemented where noted):*
- **NFR-4.1** — **Manager/panel passwords:** argon2id, never reversible.
- **NFR-4.2** — **Subscriber RADIUS passwords:** AES-GCM encrypted at rest, key in server config (`.env`, generated at install by [01](01-platform-install-licensing.md) FR-49.1, included in backups); tradeoff documented in ops docs: CHAP/MS-CHAP requires the cleartext at auth time, so DB-only compromise without the key must not reveal passwords. Decryption confined to the auth path ([02](02-radius-nas-aaa.md)) — never returned by any API.
- **NFR-4.3** — **Secrets at rest:** RADIUS secrets, SNMP communities ([02](02-radius-nas-aaa.md) FR-13.3), gateway credentials ([05](05-billing-payments-vouchers.md)) use the same AES-GCM envelope.
- **NFR-4.4** — **TLS everywhere:** Caddy fronts panel/portal/API ([01](01-platform-install-licensing.md) FR-49.5); HTTP→HTTPS redirect; HSTS on.
- **NFR-4.5** — **OWASP ASVS L2** for the web layer: this module owns the checklist (input validation posture, CSRF strategy for cookie flows, security headers, session fixation, IDOR — scoping FR-27.2 is the IDOR defense) and a pre-pilot verification pass; each module implements within its endpoints.
- **NFR-4.6** — Rate-limited portal login (mechanism FR-28.2, applied in [07](07-subscriber-portal-pwa.md)).

## 3. Acceptance criteria

- **AC-27a** — Given Sara's Operator role without `top-up`, when she calls the top-up endpoint directly (API, not UI), then it returns 403 and an audit entry records the denial.
- **AC-27b** — Given scoped agent Hassan, when he lists subscribers, sessions, or ledger entries, then only rows for his owned users are returned — verified at the API level with a second agent's data present.
- **AC-28a** — Given a manager with 2FA enabled, when they log in with correct password but wrong TOTP 3 times, then failures are audit-logged and rate limiting engages per policy.
- **AC-28b** — Given any subscriber edit, then the audit log contains actor, timestamp, and a before/after diff of exactly the changed fields.
- **AC-28c** — Given the app's DB role, when UPDATE/DELETE on an audit row is attempted, then the database refuses it.
- **AC-29a** — Given an admin revokes a manager's sessions, then that manager's next API call after access-token expiry (≤ 5 min) is rejected and they must log in again.
- **AC-30a** — Given an allowlist of `192.168.1.0/24`, when that manager logs in from another network, then login is refused and the attempt is audit-logged.
- **AC-NFR4a** — Given a copy of the database **without** the server config key, then no subscriber password, RADIUS secret, or gateway credential is recoverable from it.

## 4. Data & interfaces

**Owned entities:** `managers` (credentials argon2id, totp_secret_enc, backup_codes, scoped flag, ip_allowlist, balance-related fields read by [05](05-billing-payments-vouchers.md)), `roles` + `permissions` (role ↔ permission map, manager ↔ role), `panel_sessions`, `audit_log` (append-only).

**Exposes (contracts every module consumes):**
- AuthN middleware: token validation, session lookup → request context {manager, permissions, scoped}.
- `Require(permission)` / `ScopeFilter(managerCtx)` helpers — **contract:** every endpoint uses them; no module implements its own checks.
- `Audit(actor, action, entity, before, after)` write API.
- Crypto service: `Encrypt/Decrypt` (AES-GCM envelope, NFR-4.3) for other modules' secrets.
- `GET/POST/PUT /api/v1/managers`, `/api/v1/roles`, `GET/DELETE /api/v1/panel-sessions`, `GET /api/v1/audit-log`.

**Consumes:** balance display/top-up semantics from [05](05-billing-payments-vouchers.md) (manager *money* is theirs; manager *identity* is here); settings from [01](01-platform-install-licensing.md).

## 5. UX notes

Role editor: permission matrix (modules × verbs) with the three seeded roles as starting points — comprehensible to Omar, not just Ali. 2FA enrolment: QR + manual key + backup-codes download, in Arabic/English at minimum. Audit viewer: human-readable sentences ("Sara changed profile of user X from A to B") with expandable raw diff; localized per NFR-6 ([07](07-subscriber-portal-pwa.md)). Session list shows device/location hints so revoking the right one is obvious on a phone (Hassan).

## 6. Out of scope

- Subscriber (portal) authentication → [07](07-subscriber-portal-pwa.md) FR-41 (it reuses this module's rate-limit + storage policy).
- Manager balances/ledger → [05](05-billing-payments-vouchers.md) FR-20.
- TLS/certificate provisioning mechanics → [01](01-platform-install-licensing.md) FR-49.5.
- **Deferred by master:** infinite reseller/sub-manager tree with balance transfer (Phase 2; FR-27.4 records the seam).

## 7. Risks & open questions (owned)

- *(No master risks or open questions are owned here.)*
- **NEW:** the AES-GCM key lives in `.env` and inside backups ([01](01-platform-install-licensing.md) FR-51.1) — a stolen backup therefore contains both data and key. Decide before pilot: exclude `.env` from default backups (restore prompts for it) or encrypt backup archives with a passphrase.
- **NEW:** ASVS L2 verification needs a concrete pre-pilot checklist pass with recorded results — schedule it as a P4 exit criterion.
