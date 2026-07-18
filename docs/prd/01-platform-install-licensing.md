# HikRAD — Sub-PRD 01: Platform, Install & Licensing

> Derived from [docs/PRD.md](../PRD.md) v1.1 on 2026-07-08 (updated 2026-07-09: FR-57 added — Decision 18; FR-53 gains WhatsApp credentials — Decision 16; updated 2026-07-18 for master Decision 38 — v2 phase 5: FR-81–83, closed-source distribution & licensing hardening). Owns: FR-49, FR-50, FR-51, FR-52, FR-53, FR-57, FR-81, FR-82, FR-83 · NFR-3, NFR-7, NFR-8 · Risks: solo-dev scope creep, license cracking · Open question 3 (price point)
> Depends on: none (this is the foundation every other module builds on) · Depended on by: **all** sub-PRDs (Compose skeleton, `/api/v1` framework, migrations, settings service), especially [02-radius-nas-aaa](02-radius-nas-aaa.md) (service wiring) and [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (disk-backed queue volumes, backup of hypertables).

## 1. Scope & context

HikRAD is a commercial RADIUS AAA + billing platform for Iraqi ISPs, sold as a **one-time license per server** and installed **on-premise** via Docker. This module owns everything that makes HikRAD a deployable, updatable, licensable product rather than a pile of services: the Docker Compose installer and first-run wizard, the offline license system, backup/restore and versioned updates, the versioned internal REST API skeleton, and the global settings module. Target operator for install is **Ali** (network engineer, MikroTik expert, Linux basics); success metric **M4** is fresh install → first authenticated PPPoE user in under 30 minutes.

Stack (fixed by master §8): Go backend (`hikrad-api`, `hikrad-acct`, `hikrad-monitor`), FreeRADIUS 3.2, PostgreSQL 16 + TimescaleDB, Redis, React 18 + TypeScript (panel + portal), Caddy reverse proxy — all under one Docker Compose project on Ubuntu 22.04/24.04 LTS.

## 2. Owned requirements — elaborated

### FR-49 (M) — Docker Compose–based installer
**Master:** Single script on Ubuntu 22.04/24.04 LTS provisions all services; guided first-run wizard (admin account, ISP branding, first NAS, first profile).

*Elaboration:*
- **FR-49.1** — `install.sh` (curl-able or shipped on media): checks OS version, CPU/RAM/disk against NFR-3 minimums, installs Docker Engine + Compose plugin if absent, creates `/opt/hikrad/` layout (`compose.yml`, `.env`, `data/`, `backups/`, `licenses/`), generates all secrets (DB password, Redis password, API signing keys, RADIUS-internal shared secret) into `.env`, then `docker compose up -d`.
- **FR-49.2** — The Compose file defines: `postgres` (with TimescaleDB extension), `redis` (AOF persistence on), `freeradius`, `hikrad-api`, `hikrad-acct`, `hikrad-monitor`, `caddy`. UDP 1812/1813/3799 published; web only via Caddy (80/443). All state in named volumes/bind mounts under `data/`.
- **FR-49.3** — On first boot with an empty DB, `hikrad-api` runs migrations then serves the **first-run wizard** at the panel URL: (1) license key entry (FR-50), (2) admin account creation, (3) ISP branding (name, logo, colors — stored via FR-53 settings), (4) first NAS (delegates to the wizard owned by [02](02-radius-nas-aaa.md) FR-14), (5) first profile (form owned by [04](04-subscribers-profiles.md) FR-8). Steps 4–5 skippable.
- **FR-49.4** — Idempotency: re-running `install.sh` on an installed server detects the existing deployment and offers update (FR-51.4) instead of reinstall; it must never wipe `data/`.
- **FR-49.5** — TLS at install time per NFR-4: Caddy config offers Let's Encrypt (if a domain + internet) or generates a self-signed cert (offline default).

### FR-50 (M) — One-time license
**Master:** Signed license key bound to a server fingerprint, validated offline (no internet dependency for daily operation); grace behavior and re-issue flow for hardware changes.

*Elaboration:*
- **FR-50.1** — License file = JSON payload (licensee name, issue date, max-subscriber tier, entitled major version) + Ed25519 signature from HikRAD's private key; public key embedded in the binaries. Validation is purely offline (NFR-7).
- **FR-50.2** — Server fingerprint derived from stable machine identifiers (e.g. `/etc/machine-id` + primary MAC), hashed; the wizard displays the fingerprint so the buyer can request a key, and accepts pasted key text or file upload.
- **FR-50.3** — Grace behavior: on fingerprint mismatch (hardware change/restore to new server), the system enters a **14-day grace mode** — fully functional, persistent banner in the panel, alert event raised — during which a re-issued key can be installed. After grace expiry: panel becomes read-only *but RADIUS auth and accounting keep running* (never cut off an ISP's subscribers over licensing; consistent with NFR-2).
- **FR-50.4** — Re-issue flow is manual/off-line: admin exports a "license request" blob (old key ID + new fingerprint); vendor returns a new key. No phone-home.
- **FR-50.5** — License state (valid / grace / expired-grace, tier, version entitlement) exposed at `GET /api/v1/license` and shown on the health page (owned by [03](03-lossless-accounting-live-monitoring.md) FR-35).

### FR-51 (M) — Backup/restore & updates
**Master:** Scheduled DB + config dumps to local path; one-command restore; update mechanism preserving data (versioned migrations).

*Elaboration:*
- **FR-51.1** — Nightly (schedule configurable via FR-53) job produces one self-contained archive: `pg_dump` (custom format, includes Timescale hypertables), `.env` + Compose overrides, Caddy config, uploaded branding assets. Retention count configurable (default 14). Path default `/opt/hikrad/backups`, admin-configurable to any mounted path.
- **FR-51.2** — `hikrad backup now` and `hikrad restore <archive>` CLI (wrapper script installed by FR-49.1): restore stops app services, restores DB + config, restarts, and prints a verification summary (subscriber count, last ledger entry time, last accounting record time).
- **FR-51.3** — Restore must be safe against version skew: archives embed the schema version; restoring into a newer install runs forward migrations after load; restoring into an older install is refused with a clear message.
- **FR-51.4** — Update = `hikrad update` (or re-run installer): pulls pinned new image tags, takes an automatic pre-update backup, runs migrations (forward-only, transactional where possible), restarts. A failed migration aborts the update and restores the pre-update images. Unclean shutdown at any point must not corrupt data (NFR-2 — relies on Postgres WAL + Redis AOF).
- **FR-51.5** — Updates are delivered as versioned offline bundles (image tarballs + manifest) as well as registry pulls, because target servers may lack reliable internet (NFR-7).

### FR-52 (M) — Internal REST API, versioned
**Master:** Internal REST API used by all frontends (panel, portal), versioned from day one (`/api/v1`) so Phase-2 mobile apps and eventual public exposure need no rework.

*Elaboration:*
- **FR-52.1** — Single Go service `hikrad-api` exposes `/api/v1/**`; all panel and portal functionality goes through it — no side channels to the DB from frontends. Breaking changes require `/api/v2`; additive changes are allowed within v1.
- **FR-52.2** — Conventions this module fixes for everyone: JSON bodies; cursor pagination (`?cursor=&limit=`); consistent error envelope `{error: {code, message, field_errors[]}}`; times in RFC 3339 UTC with client-side rendering in the ISP timezone (FR-53); auth via short-lived access token + refresh (panel/portal sessions are owned by [06](06-managers-roles-security.md) FR-29 and [07](07-subscriber-portal-pwa.md) FR-41).
- **FR-52.3** — Machine-readable route inventory (OpenAPI spec generated from code) maintained internally from day one — *not published* (public API docs are an explicit non-goal for v1) but used by the frontends and tests.
- **FR-52.4** — WebSocket/SSE endpoints for live data (`/api/v1/live/…`) follow the same versioning and auth rules (consumed per [03](03-lossless-accounting-live-monitoring.md) FR-31).

### FR-53 (S) — Settings module
**Master:** Timezone (default Asia/Baghdad), currency (IQD default, display formatting), date formats, SMTP, Telegram bot token, WhatsApp Business API credentials, expiry/quota behavior defaults.

*Elaboration:*
- **FR-53.1** — Key-value `settings` table with typed, schema-validated entries; one settings service in `hikrad-api` with cache + invalidation; audit-logged changes (audit log owned by [06](06-managers-roles-security.md) FR-28).
- **FR-53.2** — v1 setting groups: **Locale** (timezone default `Asia/Baghdad`, currency `IQD`, number/date formats, default UI language), **Branding** (ISP name, logo, colors — consumed by portal/PWA [07](07-subscriber-portal-pwa.md)), **Notifications** (SMTP host/port/creds, Telegram bot token + chat IDs, WhatsApp Business Cloud API access token + phone-number ID + template names/languages — consumed by [03](03-lossless-accounting-live-monitoring.md) FR-36/FR-55), **Billing defaults** (renewal anchor rule for FR-19, expiry/quota behavior defaults consumed by [04](04-subscribers-profiles.md)), **Backups** (schedule, path, retention), **Data retention** (raw sessions ≥ 12 months, rollups ≥ 3 years — enforced by [03](03-lossless-accounting-live-monitoring.md) FR-33), **Remote access** (FR-57 tunnel enable + token, encrypted at rest).
- **FR-53.3** — Settings screen is admin-permission-gated (permission model owned by [06](06-managers-roles-security.md) FR-27).

### FR-57 (S) — Optional Cloudflare Zero Trust tunnel
**Master:** Optional built-in Cloudflare Zero Trust tunnel for remote panel/portal access: a bundled `cloudflared` container behind a Compose profile, **off by default**, with the tunnel token configured in settings; connection status shown on the health page. Strictly a convenience feature — LAN access and every daily operation keep working with the tunnel disabled or the internet down (NFR-7); only Caddy's web surface is ever tunneled, never RADIUS/CoA.

*Elaboration:*
- **FR-57.1** — `cloudflared` ships in the Compose file behind the `tunnel` profile (not started by default). Enabling = settings toggle + tunnel token (encrypted at rest per NFR-4) + `hikrad tunnel enable|disable` in the CLI wrapper (starts/stops the profile); no other service is restarted by either operation.
- **FR-57.2** — Tunnel state (disabled / connected / disconnected) surfaces on the health page ([03](03-lossless-accounting-live-monitoring.md) FR-35) and is alertable via FR-36. No service may depend on `cloudflared`: tunnel down or internet down must be invisible to LAN operation (NFR-7).
- **FR-57.3** — HikRAD only consumes the token; creating the tunnel and Zero Trust access policies happens in the Cloudflare dashboard and is documented step-by-step in the admin guide (including the strong recommendation to put an Access policy in front of the panel hostname).
- **FR-57.4** — Exposure boundary: the tunnel fronts Caddy (panel `/`, portal `/portal`, `/api`) only. RADIUS (1812/1813) and CoA (3799) UDP are never tunneled or reachable through it.

**Acceptance:**
- **AC-57a** — Given the tunnel disabled and the server offline, then every daily flow works on the LAN unchanged; given it enabled with a valid token, then the panel is reachable via the Cloudflare hostname, health shows "connected", and disabling it stops only the `cloudflared` container.

### FR-81 (M) — v2: Binary release pipeline & signed offline bundles
**Master (Decision 38):** the delivery model changes from source checkout to compiled, signed image/bundle distribution.

*Elaboration:*
- **FR-81.1** — A CI release job (triggered on a version tag) builds `hikrad-api`, `hikrad-acct`, `hikrad-monitor` and a prebuilt-frontend Caddy image, tags each `vX.Y.Z` (from the repo-root `VERSION` file, already wired per v1.1), and pushes them to a private registry.
- **FR-81.2** — The same job produces an offline bundle `hikrad-vX.Y.Z.tar`: every image the compose stack needs (HikRAD's own four plus pinned third-party images — Postgres/TimescaleDB, Redis, FreeRADIUS; `cloudflared` excluded, it is optional and already internet-dependent per FR-57), a `compose.yml` with `image:` tags (`build:` removed from the shipped copy), `scripts/`, `backend/migrations/` (checksummed, though also baked into the `hikrad-api` image per existing practice — the bundle copy lets the installer verify migrations independent of Docker layer trust), and a checksum manifest.
- **FR-81.3** — The manifest carries a detached signature verified against a public key embedded in `install.sh`/`hikrad` (the installer scripts, which must exist and be trustworthy before any HikRAD binary runs). `install.sh` and `hikrad update` verify the signature **before** extracting or loading anything from the bundle; a missing, invalid, or tampered signature is refused with no partial effect (no images loaded, no files written beyond the rejected download).
- **FR-81.4** — `install.sh`/`hikrad update` gain a bundle mode (`--bundle <path>`, already partially wired for `hikrad update` in v1) and a registry mode (pull by tag from the private registry using the FR-82.3 credential); both leave the on-server footprint at compose file + scripts + `.env` + `data/` — no source tree, no Go toolchain. The existing source-build path (`git clone` + `docker compose build`, today's only mode) remains available behind an explicit flag/env var for development only; `make up` from a checkout is unaffected (constraint: dev workflow regression-free).
- **FR-81.5** — Registry naming and bundle layout are frozen at kickoff in `docs/v2/phases/phase-v2-5-closed-source/00-phase.md`, not here — this sub-PRD fixes the requirement, the phase doc fixes the exact contract (same split as every other v2 phase's FR vs. `00-phase.md`).

### FR-82 (M) — v2: Licensing hardening — boot verification in every binary, grace unchanged
**Master (Decision 38):** `hikrad-acct`/`hikrad-monitor` gain the same license verification `hikrad-api` runs; FR-50.3 grace semantics are explicitly unchanged; no DRM/obfuscation/anti-debug.

*Elaboration:*
- **FR-82.1** — `hikrad-acct` and `hikrad-monitor` call the existing `platform.RefreshLicenseCache` (boot + the same 10-minute ticker `hikrad-api`/`setupapi.Module` already runs) so all three binaries independently load, evaluate against the live machine fingerprint (FR-50.2, unchanged), and log the license state. This reuses FR-50's existing pure grace-evaluation logic (`internal/platform/license`) and DB-backed cache (`internal/platform/license_store.go`) verbatim — no new schema, no new evaluation rule.
- **FR-82.2** — **Hard boundary:** this verification is informational/defense-in-depth only. Neither binary may refuse to start, stop processing, or degrade its core function (accounting ingest, ICMP/SNMP probing, the alerts engine) on account of license state, in any state including `expired_grace`. The only enforcement point in the whole system remains `hikrad-api`'s existing `licenseGate` HTTP middleware, unchanged in scope (panel/portal mutating calls only; never `/internal/*`, never a GET). This is not a new requirement so much as a guardrail restated because it is easy to get wrong by analogy to "verify at boot" meaning "gate at boot" elsewhere in the industry.
- **FR-82.3** — **Resolved at kickoff (registry scope, phase-v2-5 00-phase.md):** the private registry (GHCR) is a vendor/dev-side convenience and the FR-81 offline bundle is the mandatory, always-available customer path (it alone already satisfies NFR-7). No per-customer registry-pull credential is issued or bound to the license in v1 — "registry mode" in `install.sh`/`hikrad update` (FR-81.4) is documented as an unsupported/internal path for now, not a customer-facing delivery channel. Revisit only if a future need for direct `docker pull` at customer sites outweighs the ops cost of per-customer credential provisioning.
- **FR-82.4** — Explicitly out of scope: code obfuscation, anti-debugging, runtime tamper-detection beyond the license check itself, or any other DRM technique. Compiled Go with no shipped source raises the bar; it is not, and is not being made to be, tamper-proof. Enforcement beyond the license/fingerprint check is contractual and practical (no source access, signed updates, support cut-off) — consistent with this sub-PRD's existing license-cracking risk mitigation (§7).

### FR-83 (S) — v2: Repo/business hygiene for closed distribution
**Master (Decision 38):** the canonical repo stays private; only FR-81's artifacts reach customers.

*Elaboration:*
- **FR-83.1** — The GitHub repository remains private for the indefinite future; customers and resellers are never granted git access of any kind. Public-facing material (marketing pages, documentation excerpts) is maintained separately from this repository, not by loosening its visibility.
- **FR-83.2** — `docs/ops/release-checklist.md` gains a signing/tagging/registry-push section: confirm the release tag's images are signed, the bundle's signature verifies, and the registry push succeeded, before the checklist's existing sign-off step.

**Acceptance:**
- **AC-81a** — Given a clean Ubuntu VM with no Go toolchain and no HikRAD source tree, when `install.sh` runs in bundle mode against a signed `hikrad-vX.Y.Z.tar`, then the stack comes up healthy with no build step.
- **AC-81b** — Given a bundle with one bit flipped anywhere in its contents, when `install.sh`/`hikrad update` attempt to use it, then they refuse before extracting/loading anything, and no partial state is left behind.
- **AC-82a** — Given a license in `expired_grace`, when `hikrad-acct`/`hikrad-monitor` boot or hit their re-verify ticker, then they log the state but accounting ingest and monitoring probes are observably unaffected (a real Accounting-Request is still accepted and durably enqueued; a probe still runs).
- **AC-82b** — Given the dev workflow (`make up` from a checkout, no license installed at all), then nothing in this feature blocks it — the zero-license "valid" default (existing FR-50 behavior) is unchanged.

### NFR-3 (owned) — Hardware footprint
Runs fully on one modest server: **4 vCPU / 8 GB RAM / 200 GB SSD** for the 5k-subscriber tier. *Elaboration:* the installer enforces minimums with an override flag; Compose sets per-container memory limits so one component cannot OOM the box; image sizes and retention defaults must fit 200 GB with 3 years of rollups (sizing math verified in [03](03-lossless-accounting-live-monitoring.md) FR-33).

### NFR-7 (owned) — Offline resilience
No feature required for daily operation may depend on internet access. *Elaboration:* license validation offline (FR-50); updates installable from offline bundles (FR-51.5); self-signed TLS path (FR-49.5); the only online-dependent features are e-wallet payments ([05](05-billing-payments-vouchers.md)), outbound Telegram/SMTP/WhatsApp alerts and subscriber messages ([03](03-lossless-accounting-live-monitoring.md) FR-36/FR-55), and the optional Cloudflare tunnel (FR-57) — all must fail gracefully and queue/skip without affecting anything else.

### NFR-8 (owned) — Maintainability
Solo-dev-friendly: monorepo, one backend service + workers, automated migrations, seeded demo data, CI running unit + integration tests including a **RADIUS packet-level test harness simulating a MikroTik NAS**. *Elaboration:* this module owns the monorepo layout, migration tooling, `make seed-demo` (demo NAS, profiles, subscribers, sessions), and the CI skeleton; the packet-level harness content is specified with [02](02-radius-nas-aaa.md) and exercised for the pipeline in [03](03-lossless-accounting-live-monitoring.md).

## 3. Acceptance criteria

- **AC-49a** — Given a clean Ubuntu 24.04 server meeting NFR-3, when Ali runs `install.sh` and completes the wizard following the docs, then all containers are healthy and a test PPPoE Access-Request receives Access-Accept in **< 30 minutes total** (M4).
- **AC-49b** — Given an existing install, when `install.sh` is re-run, then it offers update/repair and no data under `data/` is modified without explicit confirmation.
- **AC-50a** — Given a valid license file for this server's fingerprint, when uploaded in the wizard, then the system activates with **no outbound network traffic** (verifiable with the server offline).
- **AC-50b** — Given a restored backup on new hardware, when the system boots, then it runs in grace mode with a banner, RADIUS keeps authenticating, and installing a re-issued key clears the banner. After 14 days without a new key, panel writes are blocked but auth/accounting continue.
- **AC-51a** — Given a nightly backup exists, when `hikrad restore` is run on a fresh server, then subscribers, ledger, settings, and usage history match the source (spot-checked counts) and RADIUS auth works immediately after.
- **AC-51b** — Given an update whose migration fails midway, when the updater exits, then the system is left running the previous version against uncorrupted data.
- **AC-52a** — Given any panel or portal feature, when its network traffic is inspected, then every call is under `/api/v1/` with the standard error envelope on failures.
- **AC-53a** — Given the timezone set to Asia/Baghdad and currency IQD, when any date or amount renders in panel/portal, then it uses those settings without per-page overrides.

## 4. Data & interfaces

**Owned entities:** `settings` (key, type, value JSONB, updated_by, updated_at), `license` (key_id, payload, signature, fingerprint, state, grace_started_at), `schema_migrations`. FR-81/82 add no new table — the registry-pull credential and bundle/signature material are file-based artifacts (installer-verified, not DB rows); exact shape frozen in the phase doc.

**Exposes:**
- `GET/PUT /api/v1/settings/{group}` (admin-gated)
- `GET /api/v1/license`, `POST /api/v1/license` (upload), `POST /api/v1/license/request-blob`
- `GET /api/v1/system/version` (app version, schema version, update channel)
- Compose service names & internal DNS (`postgres`, `redis`, …) — the contract other modules' services rely on.
- `install.sh --bundle <path> | --registry`, `hikrad update --bundle <path>` (FR-81.4) — installer/CLI surface, not HTTP.

**Consumes:** health signals from [03](03-lossless-accounting-live-monitoring.md) FR-35 for install-time smoke checks; audit-log write API from [06](06-managers-roles-security.md).

## 5. UX notes

First-run wizard: linear stepper, one decision per screen, mobile-usable but desktop-primary (Ali installs from a laptop). All wizard strings localized (Arabic/English at minimum — full localization rules owned by [07](07-subscriber-portal-pwa.md) NFR-6). License screens must show the fingerprint as copyable text. Update/backup screens live under Settings → System with last-backup age prominently displayed (and an alert rule if stale, via [03](03-lossless-accounting-live-monitoring.md) FR-36).

## 6. Out of scope

- NAS wizard content and RouterOS snippets → [02](02-radius-nas-aaa.md) FR-14.
- Health page content and alert engine → [03](03-lossless-accounting-live-monitoring.md) FR-35/36.
- Panel/portal auth, permissions, audit log → [06](06-managers-roles-security.md).
- PWA packaging, branding consumption → [07](07-subscriber-portal-pwa.md).
- **Deferred by master:** cloud/SaaS multi-tenant hosting (non-goal); public API docs & stability guarantees (post-v1); paid-major-update commercial flow beyond version entitlement in the key (Decision 2 — enforcement detail post-v1).
- **Explicitly out of scope (FR-82.4):** DRM, code obfuscation, anti-debugging, or any runtime tamper-detection beyond the license/fingerprint check itself. The one-click updater's panel-triggered update UX → v2-7 (`docs/v2/07-one-click-updater.md`), which builds on this phase's registry/bundle plumbing rather than the reverse.

## 7. Risks & open questions (owned)

- **Risk (master): Solo dev + "ASAP" → scope creep from SAS4 parity pressure.** Likelihood High / Impact High. Mitigation: MoSCoW is contract — Musts only until pilot; parity items live in the P7 backlog. This module is the enforcement point: phase gates in the milestone plan, and no non-Must work merged before pilot.
- **Risk (master): License cracking of on-prem one-time licenses.** Likelihood Medium / Impact Medium. Mitigation: signed keys + fingerprint; accept residual risk — support and updates are the real paid value. *Elaboration:* never let anti-crack measures degrade legitimate operation (grace mode is generous by design). **v2 phase 5 (FR-81–83) mitigation extension:** compiled-only distribution (no source tree ships) raises the bar further without changing this posture — residual risk is still accepted by design (FR-82.4), not chased with DRM.
- **Open question 3 (master): Price point** of the one-time license — market decision, not needed before P6. Blocks nothing in this module except final packaging copy.
- **NEW:** exact fingerprint inputs must tolerate common virtualization (Proxmox/ESXi clones change MACs) — decide fingerprint composition during P1 and document what changes trigger re-issue.
- **NEW:** offline update bundles need a distribution channel (USB? download portal?) — decide before P6. **Resolved direction, v2 phase 5:** the bundle itself (FR-81.2) is the distribution unit; the channel (download portal vs. USB) is an operational/business decision frozen in the phase doc, not this sub-PRD.
