# Phase 5 — Reports, Install & License (v1 / pilot-ready)

> Goal: master P6 — sellable v1: offline-licensed, installable by a network tech in < 30 min (M4), backup/restore/update safe, reports that reconcile with the ledger to the dinar, SAS4 CSV migration, and hard evidence for the flagship claims (zero loss M2, NFR-1 performance, ASVS L2). Ends with the pilot-ISP go-live checklist. Requires Phase 4 gate green.

## Agent roster & path ownership (verified disjoint)

| Agent | Task file | Exclusive paths this phase |
|---|---|---|
| A — Platform & Security | [agent-1-platform-security.md](agent-1-platform-security.md) | `deploy/**` (exc. freeradius), `scripts/**`, `backend/internal/platform/**`, `backend/internal/auth/**` (ASVS fixes), `docs/ops/**` (exc. ros-matrix.md), migrations `0410–0419` |
| C — Accounting & Monitoring | [agent-2-accounting-monitoring.md](agent-2-accounting-monitoring.md) | `backend/test/chaos/**`, `backend/test/perf/**`, `backend/internal/accounting/**`, `backend/internal/monitorsvc/**`, `docs/evidence/**` |
| D — Backend Business | [agent-3-backend-business.md](agent-3-backend-business.md) | `backend/internal/reports/**`, `backend/internal/importer/**`, migrations `0400–0409` |
| E — Frontend Panel | [agent-4-frontend-panel.md](agent-4-frontend-panel.md) | `frontend/panel/**` (incl. resuming `src/pwa/` from F with its README) |

## Frozen contracts

### C1. Schema (D 0400–0409; A 0410–0419)
- **D:** `import_batches` + `import_rows` (state, errors jsonb), `report_schedules` (only if FR-48 stretch lands).
- **A:** `license` (key_id, payload jsonb, signature, fingerprint, state `valid|grace|expired_grace`, grace_started_at), backup metadata table.

### C2. Reports API (D) — read-only over existing data, consumed by E
- `GET /api/v1/reports/revenue?from&to&group_by=day|month|manager|profile|method` → `{total_iqd, rows:[{key, amount_iqd, count}]}` — signed sums over ledger; refunds negative; totals MUST equal ledger sums exactly.
- `GET /api/v1/reports/settlement?manager_id&from&to` → `{opening, topups, renewals:{count,amount}, refunds, closing}` (Hassan's settlement — closing ≡ live balance when `to=now`).
- `GET /api/v1/reports/subscribers?view=new|expired|expiring|by_profile|inactive&n=` (expiring uses the same query as the FR-36 digest — one definition, C's rule calls D's query); rows carry subscriber ids (worklist links).
- `GET /api/v1/reports/usage?view=top_consumers|per_nas&from&to&limit=` (over `usage_daily` rollups only).
- Every report: `&format=csv` (permission `export`), print-view flag. All scoped via `ScopeFilter`.

### C3. Import API (D)
`POST /api/v1/import/subscribers` (multipart CSV) → batch id; `POST /api/v1/import/{batch}/map {column_map, preset?}` (SAS4 preset ships); `POST /api/v1/import/{batch}/dry-run` → per-row report `{row, errors[], warnings[], action:create|skip}`; `POST /api/v1/import/{batch}/execute` (idempotent re-run skips imported usernames). Encodings: UTF-8 + CP1256.

### C4. License & lifecycle (A) — sub-PRD 01 FR-50
Ed25519-signed JSON key; fingerprint = hash(machine-id + primary MAC) shown in wizard; offline verify; mismatch → 14-day grace (banner via a `GET /api/v1/license` state E polls; alert event) → after grace: panel read-only (middleware rejects mutations with `license_expired` code) but **RADIUS auth/acct unaffected**. `POST /api/v1/license` (upload), `POST /api/v1/license/request-blob`. First-run wizard backend: `GET/POST /api/v1/setup/{status,license,admin,branding}` — active only while no admin exists; wizard NAS/profile steps reuse existing APIs.

### C5. Backup/update CLI (A)
`hikrad backup now|list`, `hikrad restore <archive>` (stop-restore-migrate-verify, prints subscriber/ledger/acct summary), `hikrad update` (pre-backup, pull/load images, forward-only migrations, rollback images on failure). Nightly schedule + retention from settings. Restore refuses newer-schema archives.

### C7. Cloudflare tunnel (A) — FR-57 (amendment 2026-07-09)
`cloudflared` in compose behind profile `tunnel`, **off by default**; `hikrad tunnel enable|disable` (starts/stops only that profile); token in settings group `remote_access` (encrypted); tunnel state (disabled/connected/disconnected) in `GET /api/v1/health` and alertable. Exposure boundary: fronts Caddy only — RADIUS/CoA UDP never tunneled. No service may depend on it (NFR-7). Cloudflare-dashboard setup steps in `admin-guide.md`.

### C6. Evidence pack (C) → `docs/evidence/`
Scripted, reproducible: chaos suite results (kill-DB/kill-acct/dup-storm/unclean-host → invariant holds), perf run (NFR-1 numbers at 5k/2k scale: auth p99, ingest depth, SSE latency), 200 GB sizing check. This is the M2 proof shipped with v1.

## Cross-assignments (deliberate)
FR-45–47 backend D, UI E. FR-49 wizard backend A, UI E. FR-53 settings backend existed (A, Phase 1) — full settings UI E now. FR-6 backend D, UI E. ASVS: checklist A, fixes in each owner's code via A-filed issues (A patches only `auth`/`platform`; others get listed findings — in practice this phase's other agents fix trivia in their own paths).

## Integration gate (v1 cut)
1. **M4 rehearsal:** clean Ubuntu VM → `install.sh` → first-run wizard (license, admin, branding, NAS, profile) → real PPPoE Accept, timed < 30 min by someone other than the implementer, following `docs/ops/install-guide.md` only.
2. License: offline activation; clone-to-new-VM → grace banner + alert; grace expiry → panel read-only, harness proves auth/acct continue; re-issue clears.
3. `hikrad backup` → restore on a second VM → counts match, auth works; `hikrad update` with an injected failing migration rolls back cleanly.
4. Reports: revenue totals ≡ ledger sums (property test over seeded random data); settlement closing ≡ live balance; expiring report ≡ digest list; CSV + Arabic print views correct; scoped-manager isolation verified.
5. SAS4-shaped CSV (Arabic names, CP1256) → dry-run catches planted errors, zero writes; execute imports valid rows; re-execute skips.
6. Evidence pack generated on the release build: invariant green through all chaos scenarios; NFR-1 numbers met; attached to the release.
7. ASVS L2 checklist pass recorded; ku untranslated count = 0 (`i18n:check`); ≤ 3-click audit for renew/reset-MAC/find-user documented. Pilot go-live checklist (`docs/ops/pilot-checklist.md`) complete.
8. Tunnel (C7): disabled by default on a fresh install and everything works offline on the LAN; enable with a valid token → panel reachable via the Cloudflare hostname + health shows connected; disable stops only `cloudflared`; RADIUS/CoA unreachable through the tunnel (negative check).

---
*Amended 2026-07-09 (pre-start, Decisions 16–18): C7 tunnel contract + gate item 8 (A); settings UI gains the WhatsApp notification group, remote-access group, and the NAS auto-setup panel slot (E). See master PRD Decisions Log.*
