# Phase v4-3 — Licensing v2 & security hardening

> **FRs:** FR-122 … FR-127 · **Migrations:** 0596 · **Depends on:** v4-0 (repo privacy), v4-2 (release pipeline)
> **Owner sub-PRD:** 01 (platform) · **Decisions committed at kickoff:** 61, 62

## 1. Intent

Two halves of one problem. The licensing procedure works but is operationally brittle: activation is a hand-transcription of a fingerprint, which produces the recurring "re-issued key still lands in grace" failure; and the payload carries a tier and a cap but no *entitlements*, so white-labelling has nowhere to live. The security half closes the closed-source premise properly — v4-0 made the repo private; this phase makes the *artifacts* hold that line and adds the CI that keeps them holding it.

## 2. Scope

**In:** licence document v2, file-based offline activation, entitlement enforcement, licence UX, closed-source distribution guarantees, the security hardening pass.
**Not in:** DRM, obfuscation, or anti-debug of any kind — an explicit, standing non-goal. On-prem binaries can be reverse-engineered; the protection is contractual and practical.

## 3. Work

### 3.1 FR-122 — Licence document v2
Versioned payload, Ed25519-signed as today:
```
version, licensee, fingerprint, tier, max_subscribers,
entitlements[], issued_at, expires_at, support_until, serial
```
`entitlements[]` is a string set checked by name — `white_label`, `self_service`, `api_access`, `gateways`, and future ones — never by tier name, so adding a tier never touches enforcement code (the same "check by permission string, never role name" discipline the panel already uses).

**v1 blobs must still verify.** A customer holding a v1 licence keeps working, mapped to a default entitlement set derived from their tier. No customer is stranded by this change; that is a gate item.

### 3.2 FR-123 — Activation as a file exchange
This is the real fix for the "reactivation still lands in 14-day grace" trap, and it removes transcription entirely.

1. The server exports a signed **activation request** file containing its **current** fingerprint, component breakdown, version, and current licence serial.
2. The operator sends that file to the vendor by any channel.
3. The vendor's interactive tool consumes the request, shows the licensee and fingerprint it is about to sign for, prompts for tier and entitlements from the catalogue, and emits a licence file.
4. The operator imports the file. The server verifies the fingerprint in the licence against its own **live** fingerprint before accepting.

Because the vendor signs against a fingerprint the server itself produced, signing against a stale value becomes structurally impossible. Manual entry stays available as a fallback, and the flag-driven CLI form is retained for CI.

### 3.3 FR-124 — Enforcement in one path
One verified evaluation used by every consumer: subscriber cap (hard wall at `max_subscribers + max(10, 5%)`, per FR-105, enforced at create and CSV import), entitlement checks, expiry, and grace state. The evaluation result is derived from the **signed** payload every time — never cached into a settings row, a config file, or a database column an operator could edit. **Migration 0596** adds the licence-state audit table recording every evaluation transition (valid → grace → expired_grace and back) with the fingerprint at the time.

**Grace semantics are unchanged and non-negotiable:** RADIUS authentication and accounting never stop, in any state. Only panel writes gate. No code in this phase may introduce a new blocking point; a test asserts it.

### 3.4 FR-125 — Licence UX
The licence page and first-run wizard show: current fingerprint with a copy action, per-component drift diagnostic naming *which* component changed, tier, entitlements, cap and current usage against it, expiry, support window, and state. Export-request and import-licence are buttons, not documentation. All of it themed against v4-1's tokens — the old grace banner was invisible because it styled an undefined color, and this page is where that bug lived.

### 3.5 FR-126 — Closed-source distribution
- Private canonical repo (done in v4-0) + public source-free mirror as the only customer-facing repository.
- Shipped artifacts: **stripped** binaries, production frontend build with **no source maps**, migrations, config templates, installer. No source file of any extension, no `.git`, no internal docs, no test fixtures, no vendor tooling.
- **`lint-release-manifest.sh`** — a CI job that inspects the assembled bundle and **fails the build** on any source extension, `.map`, `.git`, internal doc path, or vendor tool. Written and proven to fail before the first v4 release exists, not after the first leak.
- Release-signing key and licence-signing key remain **separate**; both live only in the CI secret store and an offline backup.
- `license-tool/` and `release-signing/` move outside the shipped tree.
- A written **support-without-source procedure**: log bundle, health export, `hikrad doctor` output — so no support case ever creates pressure to hand over source or grant repo access.

### 3.6 FR-127 — Security hardening pass
- **Delete the checked-in FreeRADIUS private keys** (`deploy/freeradius/certs/ca.key`, `server.key`, `client.key`, `*.p12`). Stock bootstrap certs, but a known private key on an EAP surface is indefensible. Certificates are generated at install time, per install. *(Known-issues row opened in v4-0.)*
- **Secret scanning** in CI, plus a pre-commit hook.
- Confirm the encryption-at-rest inventory is complete: subscriber RADIUS passwords, RADIUS shared secrets, RouterOS API credentials, SNMP communities, SMTP/Telegram/SMS/WhatsApp tokens, tunnel tokens. Anything missing gets added here.
- Re-verify the content-type middleware handles multipart correctly (the v2-11 bug where a global JSON-only middleware silently 415'd every upload) — and add a test that exercises it **through the real router**, not around it.
- State plainly whether backups are encrypted and how the key is handled. Backups contain every secret above.
- ASVS L2 spot-check on the surfaces this phase touches.

## 4. Frozen contracts

- Licence payload field set as in §3.1; adding a field is additive and must keep v1 and v2 verification working.
- Entitlement names are checked as strings; the catalogue is data, not code.
- Activation request/response file format and their signatures.
- `lint-release-manifest.sh`'s deny-list is a contract — loosening it requires an amendment, never a local exception.
- Grace semantics (FR-50.3) restated as frozen: RADIUS and accounting never gate.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | v2 licence issues and activates | End-to-end via the file exchange, on a clean install |
| 2 | v1 licence still verifies | An existing v1 blob activates and maps to a sane entitlement set |
| 3 | Fingerprint trap closed | Change a fingerprint component, export request, re-issue, import — lands **valid**, not grace |
| 4 | Drift diagnostic names the component | Alter one component only; the page identifies which |
| 5 | Cap enforced at the wall | Create subscribers past cap + buffer; refused with `license_subscriber_cap`; CSV import refused identically |
| 6 | Entitlement gates white-label | Attribution mark present without the entitlement, absent with it; **no UI path to hide it either way** |
| 7 | Entitlement not forgeable | Edit the entitlement in the database and in config; both have no effect |
| 8 | Grace never blocks RADIUS | Force expired_grace; packet harness still authenticates and accounts |
| 9 | Release manifest audit works | Plant a `.go` file and a `.map` in the bundle; CI fails; remove; CI passes |
| 10 | No source maps shipped | `find` over the built bundle returns zero `.map` |
| 11 | FreeRADIUS keys gone and generated | Repo has no `.key`/`.p12`; a fresh install produces unique certs; two installs differ |
| 12 | Secret scanning live | Plant a fake key; CI fails |
| 13 | Multipart works through the real router | Upload test hits the actual middleware chain |
| 14 | Security review | Written review of auth, uploads, licence path, and updater recorded in the gate result |

## 6. Commit plan

1. `feat(license): v2 payload with entitlements, v1 verification retained (FR-122)`
2. `feat(license): file-based offline activation request/response (FR-123)`
3. `feat(license): single verified enforcement path; migration 0596 licence-state audit (FR-124)`
4. `test(license): grace never gates RADIUS or accounting (FR-124)`
5. `feat(panel): licence page — fingerprint, drift, entitlements, export/import (FR-125)`
6. `feat(license): white-label as a signed entitlement, no UI path to disable (FR-122, Decision 62)`
7. `ci(release): release-manifest source-leak audit (FR-126)`
8. `build: strip binaries, disable frontend source maps (FR-126)`
9. `chore(vendor): move license-tool and release-signing outside the shipped tree (FR-126)`
10. `security(radius): remove checked-in private keys, generate certs at install (FR-127)`
11. `ci(security): secret scanning and pre-commit hook (FR-127)`
12. `docs(ops): security-and-source.md, support-without-source procedure (FR-126, FR-127)`
13. `docs: v4-3 gate result`

## 7. Kickoff prompt

```
Execute phase v4-3 (Licensing v2 & security hardening) of the HikRAD v4 program, solo and
sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md               (Decisions 61, 62)
  - docs/v4/phases/phase-v4-3-licensing-security/00-phase.md   (this brief)
  - docs/prd/01-platform-install-licensing.md  (FR-50, FR-81-83, FR-103-105 — existing licensing)
  - backend/internal/platform/license/ and scripts/license-tool/
  - scripts/build-release-bundle.sh, scripts/verify-bundle.sh
  - docs/ops/known-issues.md (the FreeRADIUS private-key row opened by v4-0)

Deliver FR-122 through FR-127. Migration 0596 is the only one this phase may consume.

The single most valuable change here is FR-123: activation becomes a FILE EXCHANGE instead
of fingerprint transcription. The server exports a signed activation request carrying its
own current fingerprint; the vendor signs against that file; the operator imports the
result. This makes "signed against a stale fingerprint, lands straight back in grace"
structurally impossible rather than merely diagnosed. Build it that way round — do not
just add a better error message to the existing flow.

Non-negotiable invariants, each with a test:
  - Grace NEVER stops RADIUS auth or accounting. Only panel writes gate. No new blocking
    point may be introduced by anything in this phase.
  - Entitlements and the cap are derived from the SIGNED payload on every evaluation.
    Never cached into a settings row, config file, or column an operator can edit.
    Gate item 7 proves this by trying to forge it.
  - White-label is removed by a signed entitlement. The product exposes NO UI to hide the
    attribution mark, entitled or not.
  - v1 licences keep verifying. No existing customer is stranded.

Explicit non-goal, standing: NO DRM, obfuscation, packing, or anti-debug. On-prem binaries
can be reverse-engineered; protection here is no-source + signing + contract + support
cut-off. Do not add any of it, and do not propose it.

Prove the CI gates by making them FAIL first — plant a .go file and a .map in the bundle
(item 9) and a fake key (item 12), watch CI reject, then remove. A gate that has never
failed has not been tested.

Finish with docs/history/gates/v4-3.md.
```
