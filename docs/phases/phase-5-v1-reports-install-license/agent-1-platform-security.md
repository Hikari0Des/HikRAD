# Phase 5 — Agent 1 (Platform & Security): license, backup/restore/update, installer & wizard, ASVS pass, docs

> Owns FR-49 (complete), FR-50, FR-51, FR-53 (backend complete), NFR-4.5 verification, NFR-3/7 final checks. Depends on contracts in [00-phase.md](00-phase.md) (C1-A, C4, C5); parallel with Agents 2–4.

## Mission & context
Make HikRAD a sellable on-prem product: the offline license system with generous grace behavior (never cut off an ISP's subscribers over licensing), one-command backup/restore, a data-preserving update mechanism, the polished installer + first-run wizard backend (metric M4: < 30 min to first PPPoE user), the recorded ASVS L2 pass, and the ops documentation a pilot needs. Detail source: sub-PRD [01-platform-install-licensing](../../prd/01-platform-install-licensing.md).

## File ownership
- **Exclusive:** `deploy/**` (exc. `deploy/freeradius/`), `scripts/**`, `backend/internal/platform/**`, `backend/internal/auth/**`, `backend/migrations/0410_*.sql`–`0419_*.sql`, `docs/ops/**` (exc. `ros-matrix.md`).
- **Read-only:** everything else (ASVS findings outside your paths are filed to their owners). **Forbidden:** `internal/{billing,reports,importer,radius,accounting}` code changes, `frontend/**`.

## Tasks
1. **License** per C4 (FR-50): Ed25519 verify (pubkey embedded), fingerprint (machine-id + primary MAC hash; tolerate single-component change before mismatch — document VM-clone behavior per sub-PRD 01's open question), states valid/grace(14d)/expired_grace; expired_grace middleware: mutations 403 `license_expired`, reads + RADIUS paths untouched; upload + request-blob endpoints; license state on `GET /api/v1/license` + health page + alert event on grace entry. Vendor-side keygen tool under `scripts/license-tool/` (kept out of shipped images).
2. **Backup/restore** per C5 (FR-51): nightly job (settings schedule/retention/path), archive = pg_dump custom + `.env` + Caddy config + branding assets, `hikrad backup/restore` CLI with post-restore verification summary; decision from sub-PRD 06's open question implemented: **archives are passphrase-encrypted** (age/GPG; passphrase set at install, stored only in operator's head + printed install summary) so a stolen backup ≠ data+key. Restore refuses newer schemas.
3. **Update** (FR-51.4/51.5): `hikrad update` per C5 with automatic pre-backup, image pinning, forward-only migrations, rollback-on-failure; offline bundle path (`hikrad update --bundle <tar>`) for internet-poor sites (NFR-7).
4. **Installer & wizard** (FR-49): `install.sh` final (re-run → update/repair menu; NFR-3 checks with override), first-run wizard backend per C4 (setup endpoints active only pre-admin; license → admin → branding → optional NAS/profile via existing APIs); TLS finalization (Let's Encrypt when domain+internet, self-signed otherwise, documented cert-replacement path).
5. **Settings completion** (FR-53): remaining groups wired (backup schedule, data-retention floors enforcement check with C, billing defaults, notification config validation endpoints — "send test Telegram/email").
6. **ASVS L2 verification** (NFR-4.5): run the Phase-3 checklist against the release candidate; fix findings in your paths; file others to their owners (this phase's agents patch their own trivia); record results in `docs/ops/security-checklist.md`.
7. **Docs**: `install-guide.md` (the M4 document), `admin-guide.md` skeleton, `pilot-checklist.md`, backup/restore/update runbooks, the one-page operator guide source (NFR-5's training artifact).

Edge cases: license grace during restore-to-same-hardware must not trigger (fingerprint match); backup running during update blocked; wizard abandoned mid-way → resumable, setup endpoints stay gated; passphrase loss = documented unrecoverable (deliberate); disk-full during backup alerts rather than corrupting rotation.

## Contracts consumed/exposed
- **Consumes:** existing settings/alert plumbing (C), all module APIs (wizard reuse).
- **Exposes:** C4 license/setup APIs (E's wizard + banner UIs), C5 CLI (docs + pilot), evidence hooks (C's pack references your restore verification).

## Definition of done
- Gate items 1, 2, 3, 7 (ASVS + docs parts) pass as written — including the timed independent install rehearsal.
- Tests: signature verify incl. tampered keys, fingerprint tolerance matrix, grace state machine, read-only middleware coverage map, backup round-trip integrity + encryption, update rollback on injected failure, wizard gating.

## Handoff
v1 ships. Post-v1 backlog inherits: TWA wrapper, more gateways/vendors, reseller tree, card designer (master P7) — none of it blocked by anything you built.
