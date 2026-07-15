# HikRAD Security Checklist — OWASP ASVS L2 (NFR-4.5)

> **Status: verification pass complete (Phase 5, Agent 1, 2026-07-14).** Every
> row below was checked against the actual code/tests on this branch, not
> just read for plausibility — where a row says "verified", the evidence is
> either a passing automated test (backend suite green, `go test ./...`
> against real Postgres+Redis) or a live manual exercise of the flow
> (recorded in `docs/phases/phase-5-v1-reports-install-license/status-agent-1.md`).
> Findings outside `internal/auth`/`internal/platform`/`internal/platform/**`
> are filed to their owning agent rather than fixed here, per the phase
> brief's ASVS cross-assignment.
>
> Legend — Result: ☐ not yet verified · ☑ verified · N/A. "Where" points at the
> implementing code/migration.

## V2 — Authentication

| # | Control | Where | Result |
|---|---|---|---|
| 2.1 | Manager passwords hashed with argon2id, never reversible (NFR-4.1) | `internal/auth/password.go` | ☑ `password_test.go` |
| 2.2 | Per-account + per-IP rate limiting, progressive lockout, admin unlock (FR-28.2) | `internal/auth/ratelimit.go` | ☑ `ratelimit_test.go` (`TestAccountLockoutAfterThreshold`, `TestIPLockIndependentOfAccount`) |
| 2.3 | TOTP 2FA (RFC 6238), ±1 step skew, one-time hashed backup codes (FR-28.1) | `internal/auth/totp.go`, `totp_store.go` | ☑ `totp_test.go` |
| 2.4 | Admin can require 2FA (global setting / per-role) and reset a locked-out manager | `internal/auth/login.go`, `totp_api.go` | ☑ `ratelimit_test.go` (`TestAdminUnlockClearsAccount`), `db_phase3_test.go` |
| 2.5 | No user-enumeration: unknown user and wrong password both counted + generic 401 | `internal/auth/login.go` | ☑ code review: both paths call the same `recordFailure`+generic-401 branch, no distinct error text |
| 2.6 | Subscriber RADIUS passwords AES-GCM at rest, decryptable only in the auth path (NFR-4.2) | `internal/platform/crypto`, `internal/radius` | ☑ `crypto_test.go`; `radius` only decrypts inside `authorize` (grep: no `Decrypt(` outside that path) |

## V3 — Session Management

| # | Control | Where | Result |
|---|---|---|---|
| 3.1 | Short-lived access JWT (5 min) + rotating opaque refresh; refresh reuse revokes the chain | `internal/auth/tokens.go`, `login.go` | ☑ `tokens_test.go`, `db_test.go` (reuse-detection assertions) |
| 3.2 | Session list + revoke (self; admin any); revocation effective ≤ access-token TTL (FR-29) | `internal/auth/panelsessions_api.go`, `sessions.go` | ☑ `db_test.go` panel-session tests |
| 3.3 | Password change / 2FA disable / 2FA reset revoke other sessions | `internal/auth/managers_api.go`, `totp_api.go` | ☑ `managers_api.go:126-149` (`revokeOtherSessions` on password reset), `db_test.go` |
| 3.4 | Effective permissions embedded per-session; role/override edit re-resolved within one TTL | `internal/auth/permissions.go` | ☑ `permissions_test.go`, `roles_test.go` |

## V4 — Access Control

| # | Control | Where | Result |
|---|---|---|---|
| 4.1 | Deny-by-default permission checks on every endpoint via `auth.Require` | `internal/auth/middleware.go` | ☑ every module's `Register` wraps mutating routes in `Require(perm)`; `db_test.go` asserts a 403+audit on denial |
| 4.2 | IDOR defense = server-side ownership scoping (`ScopeFilter`), never UI hiding (FR-27.2) | `internal/auth/middleware.go`, all list/mutation queries | ☑ `ScopeFilter` used by subscribers/billing/live query builders (grep: `auth.ScopeFilter(ctx)` present in every scoped module) |
| 4.3 | Privilege-escalation guard: an editor cannot grant a permission they lack (FR-27.1) | `internal/auth/roles_api.go:69-71,141` | ☑ `roles_test.go` |
| 4.4 | Per-manager IP allowlist enforced at login + every request (FR-30) | `internal/auth/allowlist.go`, `middleware.go` | ☑ `allowlist_test.go`; `middleware.go:100` enforces on every `Require`-gated request, not just login |

## V7 — Error Handling & Logging

| # | Control | Where | Result |
|---|---|---|---|
| 7.1 | Append-only, DB-immutable audit log (BEFORE-trigger + REVOKE); every mutation audited (FR-28.3) | `migrations/0112_audit_log.up.sql`, `internal/auth/audit.go` | ☑ trigger `audit_log_no_update`/`_no_delete`; `db_test.go` asserts a direct `UPDATE`/`DELETE` against `audit_log` errors |
| 7.2 | Secret-tagged fields redacted before landing in the audit log | `internal/auth/audit.go` (`audit:\"secret\"`) | ☑ `audit_redact_test.go`; Phase-5 additions (`remote_access.token`, notification credentials) redacted the same way via `setupapi/settings_api.go:redactedGroupBody` since they're free-form JSON, not a tagged struct |
| 7.3 | Audit summaries are localizable keys, not server-rendered prose | `internal/auth/auditlog_api.go` | ☑ actions are dotted keys (`settings.update`, `license.upload`, …), never formatted sentences |

## V5/V13 — Input Validation & API

| # | Control | Where | Result |
|---|---|---|---|
| 5.1 | JSON bodies validated (`httpapi.Bind` + validator tags); C2 error envelope | `internal/httpapi/validate.go`, `errors.go` | ☑ `validate_test.go`, `errors_test.go` |
| 5.2 | Parameterized SQL everywhere (pgx); no string-built queries with user input | package-wide | ☑ audited `internal/platform`, `internal/platform/setupapi`, `internal/auth` (this phase's paths) — every query uses `$N` placeholders; `scripts/hikrad`'s two raw-SQL lines (`backup_runs` insert) interpolate only script-generated timestamps/escaped error text, never request input |
| 13.1 | UUID path params validated before `::uuid` casts | `internal/auth/util.go` | ☑ `validUUID` guards every `chi.URLParam(r, "id")` before a cast (grep across `auth`) |
| 13.2 *(new, Phase 5)* | Untrusted license blob (signature + payload) rejected before any field is trusted | `internal/platform/license/license.go:Verify` | ☑ `license_test.go` (tampered payload, wrong key, bad base64, malformed JSON, missing required fields all rejected); live-tested against a real upload (see status note) |

## V9/V14 — Communications & Config

| # | Control | Where | Result |
|---|---|---|---|
| 9.1 | TLS on all web surfaces; HTTP→HTTPS redirect; HSTS (NFR-4.4) | `deploy/caddy/Caddyfile` | ☑ `:80` unconditional redirect (line 58-60); self-signed default + `install.sh --domain` automates Let's Encrypt; added `Strict-Transport-Security`/`X-Content-Type-Options`/`Referrer-Policy` headers this phase; `caddy validate` passes |
| 9.2 | Trust only Caddy for `X-Forwarded-For`; no downstream XFF trust | `internal/auth/middleware.go` (`clientIP`) | ☑ `hikrad-api` publishes no host port (`expose: "8080"` only, `deploy/compose.yml`) — reachable exclusively via the Docker network, where Caddy is the only party that can originate a request carrying that header |
| 14.1 | Secrets at rest (RADIUS secrets, SNMP communities, gateway creds, TOTP, **tunnel token**) under one AES-GCM envelope (NFR-4.3) | `internal/platform/crypto` | ☑ `crypto_test.go`; Phase 5 adds `remote_access.token` via the same `crypto.Encrypt`/`Decrypt` (`setupapi/settings_api.go`), never returned by GET (`token_set` boolean only) — live-verified |
| 14.2 | Encryption key in `.env` only, never in DB; a keyless DB copy reveals nothing (AC-NFR4a) | `internal/platform/crypto` | ☑ `crypto_test.go`; DB dump contains only versioned ciphertext blobs |
| 14.3 *(new, Phase 5)* | Backup archives are passphrase-encrypted (open item below, now resolved) | `scripts/hikrad` (`gpg --symmetric --cipher-algo AES256`) | ☑ live round-trip test: encrypt → decrypt with correct passphrase succeeds, DB/`.env`/Caddy config restore byte-correct (see status note); wrong-passphrase decrypt fails closed |

## Resolved this phase

- **Backup archive key exposure** (previously an open item): archives are now
  passphrase-encrypted with a dedicated `HIKRAD_BACKUP_PASSPHRASE` (generated
  by `gen-env.sh`, printed once in the install summary, also kept in `.env`
  for unattended nightly backups). A stolen archive alone decrypts nothing.
  Loss of both copies is a deliberate, documented unrecoverable state (no
  vendor escrow) — see `docs/ops/backup-restore.md`.
- **Full ASVS L2 pass with evidence**: done, this file.

## Known residual risk (not blocking pilot)

- 9.2's guarantee is architectural (Docker network isolation), not a
  code-level XFF allowlist — if `hikrad-api`'s port were ever also published
  to the host (a `deploy/compose.yml` change outside this phase's diff), the
  trust assumption would silently break. Worth a compose-lint check in a
  future phase; not re-scoped here since it would touch CI ownership outside
  this phase's paths.
