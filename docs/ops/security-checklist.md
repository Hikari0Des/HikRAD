# HikRAD Security Checklist — OWASP ASVS L2 (NFR-4.5)

> **Status: skeleton (Phase 3).** This maps ASVS L2 to concrete HikRAD surfaces
> and records where each control is implemented. The **verification pass**
> (filling the Result column with evidence) is a **Phase 5 / P4-exit** task —
> see sub-PRD [06 §7](../prd/06-managers-roles-security.md). Owner: Agent 1
> (Platform & Security).
>
> Legend — Result: ☐ not yet verified · ☑ verified · N/A. "Where" points at the
> implementing code/migration.

## V2 — Authentication

| # | Control | Where | Result |
|---|---|---|---|
| 2.1 | Manager passwords hashed with argon2id, never reversible (NFR-4.1) | `internal/auth/password.go` | ☐ |
| 2.2 | Per-account + per-IP rate limiting, progressive lockout, admin unlock (FR-28.2) | `internal/auth/ratelimit.go` | ☐ |
| 2.3 | TOTP 2FA (RFC 6238), ±1 step skew, one-time hashed backup codes (FR-28.1) | `internal/auth/totp.go`, `totp_store.go` | ☐ |
| 2.4 | Admin can require 2FA (global setting / per-role) and reset a locked-out manager | `internal/auth/login.go`, `totp_api.go` | ☐ |
| 2.5 | No user-enumeration: unknown user and wrong password both counted + generic 401 | `internal/auth/login.go` | ☐ |
| 2.6 | Subscriber RADIUS passwords AES-GCM at rest, decryptable only in the auth path (NFR-4.2) | `internal/platform/crypto`, `internal/radius` | ☐ |

## V3 — Session Management

| # | Control | Where | Result |
|---|---|---|---|
| 3.1 | Short-lived access JWT (5 min) + rotating opaque refresh; refresh reuse revokes the chain | `internal/auth/tokens.go`, `login.go` | ☐ |
| 3.2 | Session list + revoke (self; admin any); revocation effective ≤ access-token TTL (FR-29) | `internal/auth/panelsessions_api.go`, `sessions.go` | ☐ |
| 3.3 | Password change / 2FA disable / 2FA reset revoke other sessions | `internal/auth/managers_api.go`, `totp_api.go` | ☐ |
| 3.4 | Effective permissions embedded per-session; role/override edit re-resolved within one TTL | `internal/auth/permissions.go` | ☐ |

## V4 — Access Control

| # | Control | Where | Result |
|---|---|---|---|
| 4.1 | Deny-by-default permission checks on every endpoint via `auth.Require` | `internal/auth/middleware.go` | ☐ |
| 4.2 | IDOR defense = server-side ownership scoping (`ScopeFilter`), never UI hiding (FR-27.2) | `internal/auth/middleware.go`, all list/mutation queries | ☐ |
| 4.3 | Privilege-escalation guard: an editor cannot grant a permission they lack (FR-27.1) | `internal/auth/roles_api.go` | ☐ |
| 4.4 | Per-manager IP allowlist enforced at login + every request (FR-30) | `internal/auth/allowlist.go`, `middleware.go` | ☐ |

## V7 — Error Handling & Logging

| # | Control | Where | Result |
|---|---|---|---|
| 7.1 | Append-only, DB-immutable audit log (BEFORE-trigger + REVOKE); every mutation audited (FR-28.3) | `migrations/0112_audit_log.up.sql`, `internal/auth/audit.go` | ☐ |
| 7.2 | Secret-tagged fields redacted before landing in the audit log | `internal/auth/audit.go` (`audit:"secret"`) | ☐ |
| 7.3 | Audit summaries are localizable keys, not server-rendered prose | `internal/auth/auditlog_api.go` | ☐ |

## V5/V13 — Input Validation & API

| # | Control | Where | Result |
|---|---|---|---|
| 5.1 | JSON bodies validated (`httpapi.Bind` + validator tags); C2 error envelope | `internal/httpapi/validate.go`, `errors.go` | ☐ |
| 5.2 | Parameterized SQL everywhere (pgx); no string-built queries with user input | package-wide | ☐ |
| 13.1 | UUID path params validated before `::uuid` casts | `internal/auth/util.go` | ☐ |

## V9/V14 — Communications & Config

| # | Control | Where | Result |
|---|---|---|---|
| 9.1 | TLS on all web surfaces; HTTP→HTTPS redirect; HSTS (NFR-4.4) | `deploy/` (Caddy) — Agent A platform | ☐ |
| 9.2 | Trust only Caddy for `X-Forwarded-For`; no downstream XFF trust | `internal/auth/middleware.go` (`clientIP`) | ☐ |
| 14.1 | Secrets at rest (RADIUS secrets, SNMP communities, gateway creds, TOTP) under one AES-GCM envelope (NFR-4.3) | `internal/platform/crypto` | ☐ |
| 14.2 | Encryption key in `.env` only, never in DB; a keyless DB copy reveals nothing (AC-NFR4a) | `internal/platform/crypto` | ☐ |

## Open items to decide before pilot

- **Backup archive contains the key**: `.env` (with `HIKRAD_ENCRYPTION_KEY`)
  lives inside default backups — a stolen backup holds both data and key. Decide:
  exclude `.env` from default backups (restore prompts for it) **or** encrypt the
  backup archive with a passphrase. (sub-PRD 06 §7.)
- Run the full ASVS L2 pass and attach evidence per row (Phase 5).
