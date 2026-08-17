# HikRAD — Phase History (v1–v3)

> Consolidates the per-phase briefs and folder-per-phase execution docs that
> previously lived under `docs/phases/`, `docs/v2/`, and `docs/v3/` (FR-114,
> v4-0 ground clearing). Full frozen contracts, chunk-by-chunk narratives,
> and detailed acceptance evidence live in the linked gate results — this
> file is the index, not a replacement for them. `docs/PRD.md`'s Decisions
> Log remains the authoritative record of *why*; this file records *when*
> and *what shipped*.
>
> Row format: `| phase | date | FRs | migrations | gate | outcome |`.

## v1 — initial build (5 phases, multi-agent, 2026-07-08 → 2026-07-16)

Built by `docs/phases/00-team.md`'s 6-role parallel-agent model (superseded
for all v2+ work by Decision 25 — solo/sequential). Phase briefs and gate
results for phases 3–5 are kept in place under `docs/phases/` (not moved —
only the per-agent task files and status files were removed, see below).

| Phase | Date | FRs | Migrations | Gate | Outcome |
|---|---|---|---|---|---|
| 1 — Foundation | 2026-07-09 | scaffolding (pre-FR numbering) | 0001, 0002, 0010, 0011 | no phase gate-result; verified post-hoc in `docs/verification-phases-1-2.md` | Compose stack (Postgres/TimescaleDB, Redis, FreeRADIUS, Caddy), `hikrad-api`/`hikrad-acct` binaries, module registry, RADIUS auth engine scaffold, panel/portal scaffolding. |
| 2 — AAA pipeline | 2026-07-11 | FR-1…FR-40 range (auth, accounting, RADIUS core) | 0100–0103, 0110–0112, 0120–0122, 0130–0135 | no phase gate-result; verified post-hoc in `docs/verification-phases-1-2.md` | Real JWT auth (argon2id, lockout, permissions-by-string, audit), lossless accounting ingest (ack-after-durable-enqueue, spill, dedup, reaper), live sessions, NAS CRUD/CoA/MNDP, RouterOS auto-setup snippet. |
| 3 — Billing, Security & Monitoring | 2026-07-11 | FR-41…FR-56 range | 0140–0199 range | [docs/history/gates/phase-3.md](history/gates/phase-3.md) — GREEN 8/8 | Billing/ledger (renewals, vouchers, receipts, refunds), roles/2FA/audit, runtime enforcement, NAS/device monitoring + alerts, dashboard. |
| 4 — Portal, Payments & PWA | 2026-07-12 | FR-57…FR-60 range | 0200–0299 range | [docs/history/gates/phase-4.md](history/gates/phase-4.md) — GREEN 10/10 (hardware/merchant-account/Meta-onboarding halves documented-pending) | Subscriber portal, e-wallet + scratch-card payments, PWA packaging (panel+portal), panel web push, subscriber WhatsApp messaging. |
| 5 — v1 Reports, Install & License | 2026-07-15/16 | remaining v1 FR range | 0300–0411 range | [docs/history/gates/phase-5.md](history/gates/phase-5.md) — GREEN 8/8 (full restore round-trip + live Cloudflare edge documented-pending) | Reports reconciling to the ledger, SAS4 CSV migration, offline licensing (grace/expired-grace), backup/restore/update CLI, optional Cloudflare tunnel, ASVS L2 pass, scripted chaos+perf evidence pack. **v1 complete.** |
| v1.1 maintenance | 2026-07-16 | owner's 26-item review | 0412 | rows in `docs/ops/known-issues.md` §"Fixed — v1.1 maintenance" | Voucher code-separator bug, permission-denied route guards, live balance refresh, MNDP UDP 5678 published, NAS RouterOS-version probe, passwordless hotspot subscribers, `hikrad uninstall`, dark/light/system themes, guided-update screen, voucher code length, portal name self-edit. |
| v1.x second pass | 2026-07-17 | pulled forward from v3 backlog | 0505+ | rows in `docs/ops/known-issues.md` | Manager disable + guarded hard delete, `hikrad factory-reset`, themed slim scrollbars, NAS/device status pages titled by name. |

## v2 — solo/sequential phases (12 phases, 2026-07-16 → 2026-07-18)

Established by PRD Decision 25: one Claude Code session per phase, start to
finish, no parallel teams. Execution order and per-phase migration budgets
are recorded in the Decisions Log (24, 28, 29–47); this table is the
outcome record. Phase labels equal their source brief's own number
(Decision 35 correction) — build order (left-to-right below) differs from
label order because of several owner re-orderings (Decisions 37, 38, 39, 40).

| Phase | Date | FRs | Migrations | Gate | Outcome |
|---|---|---|---|---|---|
| v2-1 Hotspot management | 2026-07-16 | FR-61–64 | 0500–0504 | [docs/history/gates/v2-1.md](history/gates/v2-1.md) — GREEN 9/9 | `service_type` (pppoe/hotspot/dual) replacing `allow_hotspot`; multi-service NAS via `nas_services` (`nas.type` retired); subscriber/profile→NAS scoping enforced at auth; service-aware reply pools. |
| v2-3 NAS auto-setup config manager | 2026-07-17 | FR-65–67 | 0520 | [docs/history/gates/v2-3.md](history/gates/v2-3.md) — GREEN 27/27 | Read-only router config inspection, form-driven auto-setup with per-conflict keep/update/abort, `nas_services.management_mode` (router/system) server provisioning. |
| v2-4 Multi-currency billing | 2026-07-17 | FR-68–70 | 0530–0538 | [docs/history/gates/v2-4.md](history/gates/v2-4.md) — GREEN 10/10 | `currencies` catalog, admin-entered `currency_rates`, per-`(manager, currency)` balances, explicit exchange, `formatMoney`. **Reversed by v3-2.** |
| v2-9 Cost/margin/reseller pricing | 2026-07-17 | FR-71–76 | 0540–0543 | [docs/history/gates/v2-9.md](history/gates/v2-9.md) — GREEN 11/11 | Versioned plan cost, margin derived on the ledger, global/per-site overheads, flat 2-level reseller wholesale pricing, reseller-scoping contract. |
| v2-2 Manual payment providers | 2026-07-17 | FR-77–80 | 0580–0589 | [docs/history/gates/v2-2.md](history/gates/v2-2.md) — GREEN 12/12 | Retired the Phase-4 e-wallet gateway surface; named per-manager receiving accounts + unified `payment_tickets` (transfer-proof + scratch card). **e-wallet gateway code fully removed in v4-0.** |
| v2-5 Closed-source distribution | 2026-07-18 | FR-81–83 | none | [docs/history/gates/v2-5.md](history/gates/v2-5.md) — GREEN 7/7 scripted (2 human/hardware legs documented-pending) | Ed25519 release-signing, signed bundle install/update, `hikrad-acct`/`hikrad-monitor` license re-verification. |
| v2-7 One-click update | 2026-07-18 | FR-86–88 | none | [docs/history/gates/v2-7.md](history/gates/v2-7.md) — GREEN 10/10 scripted (2 human/hardware legs documented-pending) | `hikrad-updaterd` host daemon, unix-socket protocol, panel SSE progress, `flock`-guarded single-update-at-a-time. |
| v2-6 Preferences + subscriber email | 2026-07-18 | FR-84–85 | 0589–0590 | [docs/history/gates/v2-6.md](history/gates/v2-6.md) — GREEN 27/27 | `manager_preferences` JSONB doc, self-only `/me/preferences`, subscriber `email` field. |
| v2-10 Custom dashboards | 2026-07-18 | FR-89–90 | none | [docs/history/gates/v2-10.md](history/gates/v2-10.md) — GREEN 26/27 (1 pre-existing unrelated flake) | 11-widget catalog, permission-gated, `dashboard_layout` inside `manager_preferences`. |
| v2-11 Instance branding | 2026-07-18 | FR-91–93 | none | [docs/history/gates/v2-11.md](history/gates/v2-11.md) — GREEN 39/40 (1 pre-existing unrelated flake) | `platform.LoadIdentity` single branding reader, disk-based logo storage, `PoweredByFooter` attribution mark. |
| v2-12 Frontend modernization | 2026-07-18 | FR-94–96 | none | [docs/history/gates/v2-12.md](history/gates/v2-12.md) — GREEN 38/38 | Full Radix-based form control set replacing native controls, `lint-no-native-controls.sh` CI gate, responsive/overflow audit. **v2 complete.** |
| v2.x maintenance | 2026-07-19 | owner's 17-item review, Decisions 45–47 | 0591–0592 | rows in `docs/ops/known-issues.md` | Select menu min-width, roles catalog gap (7 missing permissions), human-readable audit log, quick theme/language toggles, manager contact fields, `RateInput`/unit-prefix parsing, persistent filters, instance default payment accounts, 2 dashboard widgets. |

## v3 — active-then-reordered backlog (6 phases + 2 in-flight, 2026-07-19 → 2026-07-24)

Established by PRD Decision 47; a second owner review (Decision 48/49)
inserted v3-3…v3-8 ahead of the originally-planned v3-1/v3-2 so
backend/feature work landed before the UI restyle. v4 supersedes the
remaining v3-7 (docs/wiki) and v3-8 (dead-code cleanup) slots — folded into
this v4 program instead of running standalone.

| Phase | Date | FRs | Migrations | Gate | Outcome |
|---|---|---|---|---|---|
| v3-3 Licensing hardening + keygen CLI | 2026-07-22 | FR-103–105 | none | [docs/history/gates/v3-3.md](history/gates/v3-3.md) — GREEN 9/9 (1 visual leg documented-pending) | Interactive `license-tool issue`, tier catalog, subscriber-cap enforcement, fingerprint drift diagnostic. |
| v3-4 First-run + NAS onboarding | 2026-07-22 | FR-106–107 | none | [docs/history/gates/v3-4.md](history/gates/v3-4.md) — GREEN 6/6 (2 live legs documented-pending) | Wizard Back button, branding-name persistence fix, logo-upload permission fix, inline NAS "Save & detect". |
| v3-5 RADIUS service config/pools | 2026-07-23 | FR-108–110 | 0593 | [docs/history/gates/v3-5.md](history/gates/v3-5.md) — GREEN 6/6 (2 hardware legs documented-pending) | `address_list` reply intent, `ip_pools.management_mode` (system/router-ownership), router-field discovery-fed dropdowns. |
| v3-6 Profiles management parity | 2026-07-23 | FR-111 | 0594 | [docs/history/gates/v3-6.md](history/gates/v3-6.md) — GREEN 6/6 | Filter bar, bulk actions (`/profiles/bulk`), entity-parameterized CSV import. |
| v3-1 UI/UX modernization (interim) | 2026-07-24 | FR-97–99 | none | [docs/history/gates/v3-1.md](history/gates/v3-1.md) — INTERIM, GREEN for everything shipped | Design-system foundation (tokens, `Card`/`StatCard`/`FilterBar`/`FormSection`), Subscribers chip filter bar, "My account" area, dashboard add-widget gallery. Remaining screen sweep + live-browser matrix deferred — **folded into v4-1 (color/theme) and the per-screen phases that follow it.** |
| v3-2 Currency single-book redesign | 2026-07-24 | FR-100–102 | 0595 | [docs/history/gates/v3-2.md](history/gates/v3-2.md) — GREEN (scripted); migration rehearsal documented-pending | Reverses v2-4 to one IQD ledger book; entry-time conversion stamped on the ledger; wallet exchange endpoint retired. |

## v4 — the active program

See [docs/v4/00-v4-master-plan.md](v4/00-v4-master-plan.md) for the current
plan (v4-0 … v4-12) and [docs/v4/phases/](v4/phases/) for each phase's
brief. This file gets one new row per completed v4 phase going forward,
alongside the phase's own `docs/history/gates/v4-N.md`.

| Phase | Date | FRs | Migrations | Gate | Outcome |
|---|---|---|---|---|---|
| v4-0 Ground clearing | 2026-08-16 | FR-114 | none | [docs/history/gates/v4-0.md](history/gates/v4-0.md) — GREEN, 8/9 items pass | Docs collapsed 140→72 files, `CLAUDE.md` rewritten 131→90 lines, known-issues fully triaged, `formatIQD`/dead e-wallet tests deleted, Field→FormField accessibility migration completed (real gap was 22 instances/5 files once accurately scoped — an earlier same-line grep had overcounted it as 104/34), backend suite (24 packages) + frontend suite both confirmed green with real tooling. Canonical repo flipped private by the owner and confirmed via `gh`; confirmed no GitHub secrets ever existed to rotate. Doc-count target (≤30) explained as a genuine miss (conflicts with "keep every gate-result.md" + "keep docs/v4"). |
| v4-1 Color & theme rebuild | 2026-08-16 | FR-115–117 | none | [docs/history/gates/v4-1.md](history/gates/v4-1.md) — INCOMPLETE, 8/9 items pass | One canonical token set moved to `@hikrad/shared` (panel's `--hik-warn`/portal's `--hik-warning` divergence collapsed, 5 known + 1 newly-found `bg-success` undefined-token bug fixed); border/focus/state/info/disabled/chart-1–8 roles added; 184 `border-surface-sunken` call sites migrated to `border-border`. Two new CI-fatal gates (`lint-tokens.sh`, ground-truthed against real build output, not guessed; `lint-no-raw-palette.sh`) plus a 76-test `contrast.test.ts` computed from the token values. No-flash boot: pre-hydration inline script (explicit preference) + `prefers-color-scheme` fallback block (system preference), values asserted identical by test. **Item 9 (human 360/768/1280 × light/dark × LTR/RTL browser pass) not done — no browser in this session; explicitly left pending rather than marked green.** |
| v4-2 Native install, Docker removal, RouterOS target | 2026-08-17 | FR-118–121 | none | [docs/history/gates/v4-2.md](history/gates/v4-2.md) — INCOMPLETE, 4/14 items run and proven live (1, 6, 7, 8; item 7 was the phase's top priority), items 3/5 strongly evidenced, item 9's precondition set up live but no confirmed real packet, item 10 satisfied in substance, item 11 partially verified | `scripts/install.sh` fully rewritten as a native systemd installer (PostgreSQL 18+TimescaleDB, FreeRADIUS validated pre-enable, nginx additive/port-fallback, six `hikrad-*` units); zero-restart dynamic NAS clients via FreeRADIUS's own discovery mechanism; `scripts/hikrad` rewritten onto `systemctl` plus new `doctor`/`passwd` verbs; `deploy/compose.yml`/`deploy/docker/`/`deploy/caddy/`/`render-release-compose.sh` deleted; `hikrad-updaterd`'s Go code needed zero changes; one RouterOS all-in-one OCI image + documented procedure (`deploy/routeros/`); `hikrad migrate-to-native` for existing Compose installs; a real security fix (hikrad-api/hikrad-acct now bind loopback-only). **Live-tested for real**, not just reviewed: a full native install on a real systemd Debian 13 environment (nine real bugs found and fixed — APT keyring perms, TimescaleDB preload, directory-ownership umask, internal port collision, hardcoded `/etc/raddb` path, `rlm_exec` attribute-naming/environment-isolation, a systemd `RuntimeDirectory` gap, an orphaned-Postgres-role uninstall bug, and a `systemctl` enablement-check bug — see `known-issues.md` "Closed — v4-2"), the RADIUS packet harness (5/5 pass) against that real install, a full reboot-survival test, an uninstall/reinstall cycle, and authorized live testing against a real MikroTik CCR1072 scoped to its test hotspot VLAN (real bidirectional reachability, real scoped/additive router config changes, real NAS registration with zero restart — but no confirmed real RADIUS packet from the router, since RouterOS's captive portal doesn't intercept routed/non-L2-attached traffic the same way, so gate item 9 remains open). Items 2 (PPPoE timing), 12 (Compose migration rehearsal), and 13/14 (RouterOS-hardware install) remain genuinely not run — 2/12/13 are hardware/data legs declared at kickoff, and 13/14 were additionally outside this session's router authorization (scoped to testing only, not deploying HikRAD's own workload onto production infrastructure). |
