# Phase 3 — Agent 5 (Frontend Panel) — status

Done. Panel is the full MVP operator experience over the frozen C2/C3/C5/C6/C7 contracts.

**Built (tasks 1–8):** Renew dialog (FR-19, hero flow) with CoA-outcome + receipt print (ar/en) + idempotency key; balances header widget + Managers list with per-row balance + top-up + IP-allowlist (self-lockout warning); Ledger (FR-24) with filters, running balance, CSV export, reversing-entry pairing + Refund flow (FR-25, expiry-rollback preview); Vouchers (FR-22) batch wizard→CSV, list/void, operator redeem on user page; Dashboard (FR-32) tiles+sparkline, auto-refresh, phone single-column; Monitoring — NAS/device status (shared probe view, charts LTR), Devices CRUD (FR-60), Health (counter-invariant badge, queue, disk), Alerts rules+events (whatsapp channel, quiet hours); Security — Roles matrix editor, Managers CRUD, TOTP enrol + login 2FA step + backup codes, sessions revoke, audit-log viewer with diff; RADIUS debug tail (FR-39, pause/scroll-lock).

**Permissions:** panel now gates on the JWT `perms` claim (DB-backed roles, wildcard for admin), with the builtin-role matrix as fallback for legacy tokens.

**≤3-click renew (NFR-5):** search result → click user (1) → **Renew** (2) → **Confirm** (3). Dialog opens pre-selecting the current profile + resolved price, so no click is required to reach Confirm; profile switch/note are optional. Measured = **3 clicks**.

**Gate/quality:** `npx vitest run` 48/48 green (incl. RenewModal states, notificationReducer, ledgerPairing, RoleMatrix, LoginPage TOTP step); `npm run build` OK; `npm run lint` OK; `npm run i18n:check` OK (698 keys × en/ar/ku, en+ar translated, ku keys present). Appended lint/build/test/i18n legs to `scripts/gate-phase-3.sh`.

**Seams to verify (flagged, not blocking):**
- Renew **share-link** (key flow 2 step 5) not built — C2 renew response omits `share_token`; only print (ar/en) implemented. Ask D to add `share_token` to the renew/redeem response.
- Alert **threshold/recipients/quiet_hours** sent as `{value}` / `{whatsapp,telegram,email}` / `{start,end}` — backend stores raw JSON so it accepts them; confirm shapes with C3.
- **TOTP QR bitmap** not rendered (no bundled QR lib, CSP/offline); manual setup key + otpauth URI shown instead (enrolment fully works offline).
