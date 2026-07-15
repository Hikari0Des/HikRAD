# Phase 5 — Agent 4 (Frontend Panel) status

Built: reports (revenue/settlement/subscribers/usage, URL-encoded filters, CSV export,
print view via `.print-report`/`PrintHeader`); settings (locale/branding/notifications/
billing/gateways/backups/data-retention/remote-access, generic `useSettingsGroup` hook);
NAS auto-setup UI (preview/conflicts/apply, ROS-matrix gate, API creds added to
`NasWizardModal`); card-payment queue (reveal/approve/reject, nav badge polling);
SAS4 import wizard (upload→map→dry-run→execute→summary, never-dead-ends); first-run
setup wizard (`SetupGate` short-circuits the whole app pre-admin, resumable via
localStorage, license→admin→login→branding→NAS→profile→done); license banner/page +
`LicenseProvider` (`isReadOnly` on `expired_grace`). Resumed `src/pwa/**` ownership
(untouched). 59 vitest tests, build/lint/i18n:check green.

Seams for other agents:
- **Agent A**: `card_payments.{types,reject_cooldown_days}` settings aren't in
  `setupapi/settings_api.go`'s `settingsGroups` map yet — UI at Settings > Billing
  calls group `card_payments` and shows a "pending" notice on 404 until added (one map
  entry). `GET /api/v1/health` has no `tunnel` field yet — Settings > Remote Access
  shows "unknown" status until C7 wires it (client type already has it, optional).
- `common.productName` ("HikRAD") is identical across en/ar/ku — correct (brand name),
  pre-existing, lives in shared `common.json` outside `frontend/panel/**`.

Click-audit (dashboard → done): renew 2 (Renew→confirm), reset-MAC 2 (button→confirm),
find-user 1 (press `/`→pick result), top-up 3 (Managers nav→row Top up→submit) — all ≤3.
Phone-width: existing Hassan screens (balance/renew/dashboard) already used
logical/responsive Tailwind classes pre-Phase-5; spot-checked, no regressions from my changes.
