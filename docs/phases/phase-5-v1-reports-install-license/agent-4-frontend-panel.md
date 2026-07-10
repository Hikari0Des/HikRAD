# Phase 5 — Agent 4 (Frontend Panel): reports, settings, import wizard, first-run wizard, v1 polish

> Owns UI halves of FR-45–47, FR-6, FR-49 (wizard), FR-53 (settings UI); NFR-5 final audit. Depends on contracts in [00-phase.md](00-phase.md) (C2, C3, C4); parallel with Agents 1–3.

## Mission & context
Close out the panel for v1: report screens that read as answers (Omar) and settle collections (Hassan), the full settings module UI, the SAS4 import wizard (the switching-cost killer), the first-run setup wizard that makes the < 30-min install real (M4), the license banner/screens, and the final usability pass — ≤ 3 clicks, empty states, ku strings to zero. You also resume ownership of `src/pwa/` from Agent F (their README documents it). Detail sources: sub-PRDs [08](../../prd/08-reports.md) §5, [01](../../prd/01-platform-install-licensing.md) §5, [04](../../prd/04-subscribers-profiles.md) FR-6.

## File ownership
- **Exclusive:** `frontend/panel/**`. **Read-only:** `frontend/shared/**`. **Forbidden:** portal, backend, deploy.

## Tasks
1. **Reports section** (FR-45–47): each report opens with headline number(s) then the table; filter bars with URL-encoded state (shareable); date-range presets (today/this week/this month); group-by switchers; rows link to user pages (worklists); CSV export buttons (permission-aware); print view (A4, ISP header, generated-at, page numbers, RTL-correct with LTR numeric columns). Settlement report printable as the sign-off document.
2. **Settings UI** (FR-53): grouped screens — locale (TZ/currency/formats/language), branding (logo upload with icon preview, colors), notifications (SMTP/Telegram/WhatsApp — creds, template names, "send test" buttons for each channel), billing defaults (anchor rule, admin-balance bypass), backups (schedule/retention/path + last-backup age with staleness warning), data retention (with floor explanations), gateway enable/config (D's Phase-4 admin API), remote access (FR-57: tunnel toggle, token field write-only after save, live connection status from health). Admin-permission-gated.
2b. **NAS auto-setup UI** (FR-56, B's Phase-4 C6 endpoints): on the NAS page — credentials fields (write-only after save), "Preview setup" rendering the diff (commands to add, conflicts highlighted with plain-language explanations, LTR monospace inside RTL), explicit confirm to apply, per-item results, disabled state with explanation for unvalidated ROS versions (copy-paste tab always available beside it).
2c. **Card-payment verification queue** (FR-59, D's Phase-4 C8 endpoints; amendment 2026-07-11): pending list (subscriber link, profile/price, card type, waiting time — oldest first, badge count in nav), reveal-code action (confirm dialog explaining it's audited; code shown LTR monospace, copy button), approve confirm (shows resulting expiry), reject dialog (reason required, consequence preview: subscriber loses trial access + gets notified); decided items filterable history. Permission `card_payments.verify`-gated.
3. **Import wizard** (FR-6): upload → encoding/delimiter feedback → column mapping (SAS4 preset one-click) → dry-run report (per-row errors/warnings, downloadable) → execute with progress → summary. Must never dead-end: every failure states the fix.
4. **First-run wizard** (FR-49.3): linear stepper on A's setup endpoints — license (fingerprint copyable, file/paste), admin account, branding, optional first NAS (reuses NAS wizard) + first profile; resumable; desktop-primary but phone-tolerable; ends on the dashboard with a "what next" card.
5. **License surfaces** (FR-50): grace banner (persistent, dismissible per-session), license page (state, fingerprint, upload, request-blob download), read-only-mode UX (mutations disabled with explanatory tooltips when `license_expired`).
6. **v1 polish** (NFR-5/6): ≤ 3-click audit for the canonical tasks (renew, reset-MAC, find-user, top-up) — documented with click counts; empty states everywhere meaningful (fresh-install dashboard, empty reports); ku translation to 0 untranslated (with F's Phase-4 gap list); final phone-width pass (Hassan's screens: balance, renew, dashboard).

Edge cases: report tables with thousands of rows (virtualize + server pagination; print view caps with a note); settings save with validation errors mid-group (per-field errors, no partial silent saves); wizard license step offline (self-signed TLS warning explained in plain language); import of a file with BOM/CRLF oddities surfaced gracefully; read-only mode must not break SSE views (reads still live).

## Contracts consumed/exposed
- **Consumes:** C2 reports, C3 import, C4 license/setup, D's gateway admin API (Phase 4), settings endpoints (A) — all frozen.
- **Exposes:** the finished v1 panel; the documented click-count audit and screenshots for the release notes.

## Definition of done
- Gate items 1 (wizard UI leg), 4, 5, 7 (clicks + ku + print views) pass.
- Component tests: report filter/URL state, print-view rendering, import wizard state machine incl. dead-end prevention, setup stepper resume, read-only mode gating.
- `i18n:check`: 0 untranslated across all three locales; Lighthouse pass retained post-changes (PWA shell untouched or re-verified).

## Handoff
v1 complete. Post-v1 panel backlog (card designer, reseller tree UIs, public API console) starts from a finished, localized, audited baseline.
