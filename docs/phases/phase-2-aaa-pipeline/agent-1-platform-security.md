# Phase 2 — Agent 1 (Platform & Security): real manager auth, permissions middleware, audit log, crypto service

> Owns FR-28 (core: login protection, audit write), FR-29 (sessions core), NFR-4.1–4.3; depends on contracts in [00-phase.md](00-phase.md) (C1-A, C2, C3); parallel with Agents 2–5.

## Mission & context
Phase 1 shipped a dev-only auth stub. You replace it with real manager authentication (argon2id, JWT access + rotating refresh, panel session records), the permission/scoping middleware every API module adopts this phase, the append-only audit log write API, and the AES-GCM crypto service other agents use for secrets at rest. The full security module (2FA, roles editor, IP allowlist, audit viewer) is Phase 3 — build the seams now. Detail sources: sub-PRDs [06-managers-roles-security](../../prd/06-managers-roles-security.md), [01](../../prd/01-platform-install-licensing.md) §FR-52.2.

## File ownership
- **Exclusive:** `backend/internal/auth/**`, `backend/internal/platform/crypto/**`, `backend/migrations/0110_*.sql`–`0119_*.sql`.
- **Read-only:** `backend/internal/httpapi` (use the middleware-injection seam D left). **Forbidden:** `backend/internal/{radius,subscribers,profiles,accounting,live}`, `frontend/**`, `deploy/**`.

## Tasks
1. Migrations 0110–0119 per phase C1-A: manager TOTP fields (unused until Phase 3), `scoped` flag, `panel_sessions`, `audit_log` with `REVOKE UPDATE, DELETE` from the app role. [FR-28.3, FR-29]
2. `platform/crypto`: AES-GCM envelope service per C3 (key from env, random nonce, versioned prefix for future rotation). Unit-test against fixed vectors. [NFR-4.2/4.3]
3. Real auth: `POST /api/v1/auth/login` (argon2id verify; per-account + per-IP rate limiting; progressive lockout 5 fails → 15 min, audit-logged), `/auth/refresh` (rotating refresh tokens, hash stored on `panel_sessions`, reuse detection revokes the session), `/auth/logout`. Response shapes identical to Phase-1 C7 so Agent E's client needs no change. Remove the dev stub (coordinate: D marked it; you own `internal/auth`, the stub lived behind the injection seam). [FR-28.2, FR-29]
4. Permission middleware per C2: permission-string checks (`subscribers.view`, `nas.edit`, action perms `renew|disconnect|topup|export`) with this phase's hardcoded role→permission sets (admin=all; operator per sub-PRD 06 FR-27.3; agent=scoped minimal); `ScopeFilter` helper returning the manager's scope for query filtering. Deny by default; 403s audited. [FR-27 core, FR-27.2]
5. `auth.Audit(...)` write API per C2 + minimal `GET /api/v1/audit-log?entity_type&entity_id` (permission-gated, paginated) so E's user page can show a change trail (full viewer UI is Phase 3). [FR-28.3]
6. Manager CRUD minimal: `GET/POST/PUT /api/v1/managers` (create operator/agent accounts, set role + scoped flag, reset password) — enough for Phase-2/3 testing; full roles editor is Phase 3.
7. Session management endpoints: `GET /api/v1/panel-sessions` (own), `DELETE /api/v1/panel-sessions/{id}` (own; admins any). Password change revokes other sessions. [FR-29]

Edge cases: lockout must not let an attacker lock out an admin permanently (admin unlock endpoint + per-IP vs per-account separation); refresh-token reuse (stolen token) revokes the whole session chain and audits it; audit `before/after` diffs must redact secret fields (password_enc, secrets) by struct tag.

## Contracts consumed/exposed
- **Consumes:** httpapi framework + middleware seam (D, Phase 1), platform Settings/DB (own role, Phase 1).
- **Exposes (frozen in C2/C3):** `auth.Require`, `auth.ScopeFilter`, `auth.Audit`, `crypto.Encrypt/Decrypt`, real login/refresh endpoints. Every other backend agent adopts these **this phase**; E consumes real login transparently.

## Definition of done
- Gate items 5 (login) and 6 (audit rows) pass; all Phase-2 modules compile against `Require`/`ScopeFilter`/`Audit` (verify with a grep-level check that no module ships a mutation without an Audit call — add a lint script).
- Tests: argon2id verify + upgrade path, lockout timing, refresh rotation + reuse detection, permission matrix per role, scope filtering (agent sees only owned subscriber via a D-fixture), audit immutability (UPDATE refused at DB level), crypto round-trip + tamper detection.
- Auth p99 overhead < 5 ms (it sits inside B's 100 ms budget).

## Handoff
Phase 3 (same role) builds the full FR-27–30 module on these tables/middleware. Other agents get working authn/authz/audit/crypto they never re-implement.
