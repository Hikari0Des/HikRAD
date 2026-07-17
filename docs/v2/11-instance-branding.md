# v2-11 — Instance branding (company logo + name)

> Owner request 2026-07-17 (ex-v3.3, merged into v2 by PRD Decision 32): "per-company logo and instance name configuration." Each licensed installation belongs to one ISP; that ISP's identity should appear everywhere a customer or operator looks, not "HikRAD".

## 1. Problem

FR-43 promised portal ISP branding (logo, name, colors) via admin settings, and the first-run wizard already collects "branding" — but the identity is not threaded everywhere: panel chrome, PWA manifests/icons, printed receipts/vouchers and report headers carry product identity or defaults. There is no single owned "instance identity" contract.

## 2. Requirements (draft — renumber at kickoff)

### FR-A — Identity settings
- Settings > Branding (`branding.manage` permission): **instance name** (trilingual-capable), **logo upload** (PNG/SVG size-limited, stored under the data dir — never fetched from the internet, NFR-7), optional accent color. Served via a small public endpoint so login pages can show it pre-auth.

### FR-B — Threaded everywhere
- Panel + portal login screens, app header/sidebar, browser titles.
- PWA: `manifest` name/short_name + generated icon set for both apps (regenerate on logo change; installed-app icon updates on next SW update).
- Print surfaces: report headers (FR-49's ISP header), receipts, voucher/card print templates.
- Sensible fallback when unset: current defaults, so a fresh install is never broken-looking.

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `platform.Settings` + first-run wizard | Phases 1/5 (A/D) | branding keys + upload handling + public read endpoint |
| Panel + portal shells, login pages | Phases 2–5 (E/F) | consume identity (name/logo/color) everywhere |
| PWA manifests/SW | Phase 4 (F) | dynamic manifest + icon generation |
| Reports/receipts print views | Phases 3/5 (D/E) | header identity from settings |

## 4. Acceptance sketch

- Admin uploads a logo and sets "Nur Net" → both login screens, headers, the installed PWA name/icon, a printed receipt and a report header all show Nur Net; nothing anywhere fetches the logo from outside the server.
- Removing the logo falls back to defaults cleanly; oversized/wrong-type uploads 422 with a field error; the change is audit-logged.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. We are starting v2 phase 11: instance branding (PRD Decision 32). You work SOLO — no parallel agents; execute sequentially (settings + upload → shells/logins → PWA manifests → print surfaces), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/11-instance-branding.md, docs/prd/01-platform-security.md (settings), docs/prd/07-subscriber-portal-pwa.md (FR-43, PWA), the first-run wizard code, and one print surface (report header).

Step 1 — Amend the docs (single commit): FR rows + Decisions Log row in docs/PRD.md, update owning sub-PRDs (01/07/08), docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-11-instance-branding/00-phase.md with frozen contracts (branding settings keys, upload constraints, public identity endpoint shape, manifest generation approach) and the integration gate (pre-auth login shows identity, PWA manifest reflects it, receipt/report header test, offline test — no external fetch; migrations linear, next free number). Scriptable items → scripts/gate-v2-phase-11.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: assets live on local disk only (NFR-7); the identity endpoint is public but read-only and serves nothing user-uploaded except the vetted logo; strings trilingual; update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
