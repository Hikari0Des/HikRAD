# Phase v3-4 — First-Run Setup + NAS Onboarding UX — Gate Result

**Verdict: GREEN.** 6/6 scripted gate items PASS; 2 legs (live fresh-install + upgrade logo upload; a real-router "Save & detect" round trip) are documented-pending — no Docker stack / real MikroTik in-session, same posture as prior phases' hardware legs.

Date: 2026-07-22 · FR-106 (sub-PRD 01) + FR-107 (sub-PRD 02) · **no migration** · **no Go/backend change** (frontend + deploy scripts only).

## What shipped

| Contract | Summary | Key files |
|---|---|---|
| C1 (FR-106.1) | Wizard Back navigation — `BACK_TARGET` map, ghost Back control on admin→license / nas→branding / profile→nas | `frontend/panel/src/setup/SetupWizardPage.tsx` |
| C2 (FR-106.2) | Company-name persistence — `BrandingSettings` imperative `save()` handle + `hideSaveButton`; wizard Next saves before advancing | `frontend/panel/src/pages/settings/BrandingSettings.tsx`, `SetupWizardPage.tsx` |
| C3 (FR-106.3) | Branding-dir plumbing mirroring `acct-spill`: env + mount + chown on install/factory-reset **and** a pre-`compose up` prep in `hikrad update` for existing installs | `deploy/compose.yml`, `scripts/install.sh`, `scripts/hikrad` |
| C4 (FR-107) | Inline NAS "Save & detect" — create-then-discover in one flow; idempotent Save (no duplicate); read-only, no router write | `frontend/panel/src/pages/nas/NasWizardModal.tsx`, `frontend/shared/locales/{en,ar,ku}/panel.json` |

## Gate items

1. **Back navigation — PASS.** `BrandingSaveOnNext.test.tsx` asserts a Back control on the profile step returns to nas; the map deliberately omits license/branding/done (branding's prior step `admin` would 403 on re-submit via `guardSetupOpen`). No admin-recreate dead-end.
2. **Name persistence — PASS.** `SetupWizardPage` branding step renders `<BrandingSettings ref hideSaveButton />` and its Next calls `brandingRef.current.save()`; `BrandingSaveOnNext.test.tsx` mocks the settings-group save and asserts it fires exactly once on Next and the wizard advances to `nas`. (This is the bug: previously Next never saved, so `branding.name` was never written.)
3. **Logo dir — PASS.** `deploy/compose.yml` sets `HIKRAD_BRANDING_DIR=/branding` + the `${HIKRAD_DATA_DIR}/branding` mount; `install.sh` §3d0 and `hikrad factory-reset` `mkdir`+`chown 10001:10001`; `cmd_update` preps+chowns it before `compose up` so existing installs are fixed on upgrade. `bash -n` clean on both scripts; `scripts/gen-env.test.sh` → all checks passed. (Live upload on a real stack — fresh + upgraded — is the documented-pending human leg.)
4. **Inline detect — PASS (logic).** `NasWizardModal` "Save & detect" creates the NAS, stores `createdId`, jumps to step 1, and auto-runs the existing read-only `discoverNasServices(createdId)`; `save()` resolves `targetId = existing?.id ?? createdId` so a create-for-detect + Save updates one row, never a duplicate. `tsc --noEmit` clean; panel lint clean. (Real-router round trip is the documented-pending hardware leg — reuses the already-tested FR-62.6 endpoint, no new backend.)
5. **v2-12 CI floors — PASS.** `no-native-controls (panel): clean`; prettier clean; stylelint logical-properties passes; `npm run i18n:check` → **OK** (the new `nas.saveAndDetect` key is trilingual; the 5 residual identical warnings are pre-existing proper nouns). No native control added.
6. **No regressions — PASS.** `tsc --noEmit` clean; existing `SetupWizardPage.test.tsx` (3) + `branding.test.tsx` (2) + the 2 new setup tests green; no backend change (`go` untouched). Bonus: v3-3's new `warn` token also un-breaks the NAS health-finding banner (`bg-warn/10` in `NasWizardModal`, previously a no-op class).

## Documented-pending / notes

- **Live legs (2):** fresh-install + upgrade logo upload on a real Docker stack (proves the dir is created+owned end-to-end); real-MikroTik "Save & detect" round trip. Neither is reproducible in-session (no Docker/hardware); both reuse already-verified plumbing (the `acct-spill` chown pattern; the FR-62.6 discovery endpoint).
- **Out of scope (flagged):** `HIKRAD_PAYMENT_ATTACHMENTS_DIR` is *also* unset/unmounted (same latent class as the branding dir, a v2-2 surface) — noted for a future pass, not fixed here.
- **No migration**; repo max stays 0592.
