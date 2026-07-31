# Phase v3-4 — First-Run Setup + NAS Onboarding UX

> Kicked off 2026-07-22 (PRD Decision 50), immediately after v3-3. SOLO, sequential. Owned by **sub-PRD 01** (FR-106) + **sub-PRD 02** (FR-107). Inherits every standing rule from [`../00-v3-execution-plan.md`](../00-v3-execution-plan.md). **No migration.**

## 1. Problem (grounded in the code)

- **#2 Back button** — `SetupWizardPage.tsx`'s `go()` only ever advances + persists the step; no step renders a Back control, so a user can never move backward.
- **#14 Company name not persisted** — the branding step embeds `<BrandingSettings/>` and renders its **own** Next (`go('nas')`), which never triggers `BrandingSettings`' separate Save. No `PUT /api/v1/settings/branding` fires, so `branding.name` (the exact key Settings later reads) is never written → the name must be re-entered.
- **#3 Logo `mkdir … permission denied`** — `HIKRAD_BRANDING_DIR` is unset everywhere (compose/.env/install.sh), so `platform.BrandingDir()` falls back to the relative `data/branding`; the runtime image has no `WORKDIR`, so cwd is `/` and this resolves to `/data/branding`; uid 10001 (non-root) can't `mkdir` at `/`; and no compose volume/chown exists for it (unlike `acct-spill`, which is mounted **and** chowned). So `StoreLogo`'s `os.MkdirAll` returns EACCES → 422 to the UI.
- **#9 NAS detection forces a secondary page** — `ServicesEditor`'s "Detect services" button is gated on a persisted `nasId` + saved API creds; a new NAS has neither, so the operator must create → reopen-in-edit → detect.

## 2. Scope

FR-106 (three first-run bug fixes) + FR-107 (inline NAS detection). No new endpoint, no schema, no router write. Explicitly out of scope: any redesign of the wizard beyond Back + the branding-save wiring; the sibling `HIKRAD_PAYMENT_ATTACHMENTS_DIR` (also unmounted — noted in known-issues but a v2-2 surface, not this phase).

## Frozen contracts

### C1. Wizard Back navigation (FR-106.1)
- A `BACK_TARGET` map offers Back only where safe: `admin→license` (pre-auth), `nas→branding`, `profile→nas`. **Not** on `license` (first), `branding` (its prior step `admin` is a dead-end once the admin exists — re-submitting would 403 via `guardSetupOpen`), or `done`. Rendered as a ghost Back button in the wizard shell (below the step dots), using the existing `ui.back` locale key. Purely client-side step navigation; no data is discarded.

### C2. Branding save on Next (FR-106.2)
- `BrandingSettings` gains an optional imperative handle (`forwardRef` → `{ save(): Promise<void> }`) and an optional `hideSaveButton` prop. In the wizard branding step it is rendered with `hideSaveButton`, and the wizard's Next calls `await brandingRef.current?.save()` **before** `go('nas')`. The save reuses the existing `PUT /api/v1/settings/branding` path (`useSettingsGroup('branding').save`) unchanged — same key, same validation. Outside the wizard, `BrandingSettings` behaves exactly as today (its own Save button shown, no ref required).

### C3. Branding directory plumbing (FR-106.3) — mirrors `acct-spill` exactly
- **`deploy/compose.yml`** (`hikrad-api` service): add env `HIKRAD_BRANDING_DIR: /branding` and volume `- ${HIKRAD_DATA_DIR:-./data}/branding:/branding`.
- **`scripts/install.sh`** (new §3d-style block): `mkdir -p "$HIKRAD_ROOT/data/branding"` + `chown 10001:10001` (the api uid, per `api.Dockerfile`) — idempotent, runs on install and repair.
- **`scripts/hikrad`**: (a) `cmd_update` — before the `compose up -d` that applies the update, `mkdir -p "$(data_dir)/branding"` + `chown 10001:10001` (best-effort, warn if not root), so **existing installs** upgrading via `hikrad update` (which never re-runs `install.sh`) get the dir created+owned before Docker would auto-create it root-owned; (b) `factory-reset` — add `branding` to the bind-mount recreation alongside `acct-spill`/`radius-control`.
- No `BrandingDir()` default change (still `data/branding` when the env is unset — dev/test); production now always sets `HIKRAD_BRANDING_DIR=/branding` to a mounted, chowned path.

### C4. Inline NAS "Save & detect" (FR-107)
- `NasWizardModal` gains local state `createdId: string | null` (reset on open). `save()` resolves the target id as `existing?.id ?? createdId`: an id present → `updateNas(id)`, else `createNas` (so a create-for-detect followed by Save updates the same row — never a duplicate).
- Step 2 (where API creds are entered) gains a **"Save & detect"** button, shown only for a new NAS (`!editing && !createdId`) once `apiUser` + `apiPassword` are filled. It `createNas`, stores the returned id in `createdId`, jumps to step 1, and signals `ServicesEditor` to auto-run `detect()` (a `detectSignal` counter + a `useEffect`), which calls the existing read-only `discoverNasServices(createdId)` and merges results — identical to today's manual detect, just triggered in one flow.
- `ServicesEditor` receives `nasId = existing?.id ?? createdId` and `hasApiCreds = existing?.has_api_creds || Boolean(createdId)`, so its Detect button + auto-detect work for the just-created NAS. No change to the merge logic, the backend, or the router (read-only `discover-services`, Decision 17 intact).

## Integration gate

1. **Back — PASS** when the wizard renders a Back control on admin/nas/profile that returns to the prior step; none on license/branding/done; no admin-recreate dead-end.
2. **Name persistence — PASS** when pressing the branding step's Next fires `PUT /settings/branding` (name written) before advancing; a `SetupWizardPage`/`BrandingSettings` test asserts the save handle is invoked on Next.
3. **Logo dir — PASS** when `deploy/compose.yml` sets `HIKRAD_BRANDING_DIR=/branding` + the mount, `install.sh` and `hikrad` create+chown it (10001), and `cmd_update` preps it pre-`compose up`; verified by `scripts/gen-env.test.sh` still green + a grep/shell assertion that the three files carry the branding dir. (Live fresh-install + upgrade logo upload is the documented-pending human leg — no Docker stack in-session.)
4. **Inline detect — PASS** when a new NAS + creds → "Save & detect" creates exactly one NAS and merges detected services without reopening; a subsequent Save updates (no duplicate); no router write (reuses read-only `discover-services`).
5. **v2-12 CI floors green** (`no-native-controls`, `layoutSmoke`, stylelint, `i18n:check`); any new control uses the FR-94 set; new strings trilingual.
6. **No regressions**: `tsc --noEmit` clean both apps touched; existing NAS wizard + setup tests green; backend untouched (no Go change).

## Owner decisions
All four v3-4 items were confirmed at the v3.x review (Decision 48) and the v3-3 kickoff; the "save-then-detect in one flow" approach for FR-107 (vs a credential-less ad-hoc probe endpoint) is the frozen choice — less backend surface, reuses the existing read-only endpoint.
