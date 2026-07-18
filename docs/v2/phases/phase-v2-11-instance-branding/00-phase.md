# Phase v2-11 — Instance Branding

Source brief: [docs/v2/11-instance-branding.md](../../11-instance-branding.md). Requirements
FR-91 (identity settings + disk-backed logo upload + public identity endpoint),
FR-92 (threading that identity through every surface), and FR-93 (a fixed,
non-configurable "Powered by HikRAD" attribution — added by Decision 43 while
this brief was still awaiting confirmation, before any feature code was
written), committed by PRD Decisions 42 and 43. FR-91 owned by sub-PRD
[01-platform-install-licensing.md](../../../prd/01-platform-install-licensing.md)
(extends FR-53's existing Settings module); FR-92/FR-93 owned by sub-PRD
[07-subscriber-portal-pwa.md](../../../prd/07-subscriber-portal-pwa.md)
(extends FR-43/FR-54 — FR-93 joins FR-92 under the same owner rather than
getting a domain of its own, since it is the fixed floor under the same
identity-threading surface, not a new area); sub-PRD 08's report-header text
now names FR-91's endpoint as its source, no new FR (same "consumer, not
owner" pattern 08 already follows for every other domain's data).

## 1. Problem (restated from the source brief, sharpened by kickoff research)

FR-43 promised portal ISP branding, and Settings > Branding + the first-run
wizard's branding step already exist — an admin can type a name, pick colors,
and upload a logo today. **But almost none of it actually reaches anywhere**,
and not just because nobody wired the last mile: reading every existing
branding consumer before freezing this file turned up three independent,
pre-existing bugs (full detail in `docs/ops/known-issues.md`, added this
session):

1. The public `GET /api/v1/branding` endpoint (`internal/portalapi/branding.go`)
   and the Hotspot captive-portal page (`internal/radius/hotspot.go`) both call
   `platform.Get[brandingSettings](ctx, settings, "branding")` — a single
   settings key `"branding"` that **has never existed**. `platform.Settings`
   stores one row per fully-qualified key (`branding.name`, `branding.logo_url`,
   …); both callers silently swallow the resulting `ErrSettingNotFound` and
   fall back to their hardcoded defaults, every time, on every install.
2. Even fixing that, both readers' struct field names (`color_primary`,
   `logo_data_uri`) don't match the real stored field names (`primary_color`,
   `logo_url`) — a second, independent mismatch.
3. The receipt header (`internal/billing/receipt.go`) reads
   `billing.receipt_branding` expecting a JSON **object**
   (`{name, address, phone}`), but the only UI that ever writes that key
   (`BillingSettings.tsx`) sends a plain **boolean** (a show/hide toggle) —
   the unmarshal fails, the error is swallowed, and every receipt has always
   shown the literal fallback `"HikRAD"`.
4. Separately, the logo itself is stored as a base64 **data: URI inside the
   settings JSONB row**, encoded client-side with only a client-side 512 KB
   check and *no server-side size or content-type validation at all* — not
   the "stored under the data dir" contract FR-A always intended (NFR-7 is
   not actually violated — nothing is fetched from outside the server either
   way — but an unvalidated arbitrary blob landing in a `settings` row via a
   plain `PUT` is still the wrong shape, and it is what stands between today
   and FR-91's disk-backed upload).
5. Neither the panel's sidebar/header mark, its browser `<title>`, nor its
   own login screen (`frontend/panel/src/pages/LoginPage.tsx`) read branding
   at all — both hardcode `t('common.productName')` ("HikRAD"). There has
   never been an FR covering this; FR-92 closes the gap.

This phase's job is therefore **not** "wire up branding" from scratch — most
of the wiring (portal login/shell, both PWA manifest swaps, the report print
header) already exists and is already correct on the *consuming* side. The
job is: fix the one broken settings-read pattern at its source (once, in
`internal/platform`), give logo storage a real disk-backed home with real
validation, and add the two consumers (panel chrome, receipts) that were
never wired in the first place.

**Added by Decision 43, while this brief was still awaiting confirmation:**
the owner clarified that full customer rebranding is intentional and should
ship exactly as FR-91/92 describe — a customer may rename and re-color
*everything* — but a small, fixed "Powered by HikRAD" mark must remain
somewhere the customer's own product access can never remove or edit (C10).
The two requirements are not in tension: FR-91/92 govern the *configurable*
identity layer (name/logo/color, entirely the customer's), FR-93 is a single
fixed line underneath it that never reads from that layer at all.

## 2. Scope for this implementation pass

1. **Settings + storage** — `internal/platform` gains the corrected branding
   read helpers and disk-backed logo storage (C1/C2); `internal/portalapi`'s
   `GET /api/v1/branding` and `internal/radius/hotspot.go`'s `loadBranding`
   both switch to them (C3/C4); `internal/billing`'s receipt header switches
   its `receipt_branding` read from a broken object-shaped key to the
   boolean it always actually was (C5).
2. **Panel** — new `useBranding` hook/context (mirrors the portal's existing
   `branding.tsx`), wired into `Sidebar`, `TopBar`/browser title, and
   `LoginPage.tsx` (C6); `BrandingSettings.tsx`'s logo control switches from
   client-side data-URI encoding to a real multipart upload against the new
   endpoint.
3. **Portal + PWA + reports** — no code changes expected (C7/C8): these
   already call the now-fixed endpoint correctly. This pass re-verifies them
   rather than reimplementing them, and only touches them if the fix
   surfaces a real regression.
4. **Fixed attribution** — a hardcoded `<PoweredByHikRAD>`-style footer
   component, shared (`@hikrad/shared` or duplicated once per app if a
   shared component would need its own new export surface — implementer's
   call), mounted in both apps' shells and login pages (C10).
5. **Gate** — DB-gated tests proving every one of the three original bugs is
   fixed, upload validation, panel threading, the fixed attribution's
   presence and non-configurability, and an NFR-7 offline/no-external-fetch
   check; `scripts/gate-v2-phase-11.sh`; `gate-result.md`.

Commit in reviewable chunks, in the order the kickoff prompt specified:
**settings + upload (C1/C2/C3) → Hotspot + receipts fixed on the same
underlying read (C4/C5) → panel shells/logins (C6) → PWA manifests
re-verified (C7) → print surfaces re-verified (C8) → fixed attribution
footer (C10)**.

## Migration budget

**None used.** `settings` and its `branding.*` keys already exist (migration
0010); this phase corrects how they're read and adds a new upload/serve
path, neither of which is a schema change. The execution plan's original
budget for this phase (0600–0609) is unused and stale — the repo's actual
current maximum is **0590**, so if implementation surfaces a genuine need for
a migration, **0591** is the next free number per the standing linear rule,
not anything in the stale 06xx range.

## Frozen contracts

### C1. Branding storage model — one corrected source of truth

No new settings keys. FR-53.2's existing `branding` group is the **only**
source: `branding.name` (string), `branding.logo_url` (string | absent —
now server-managed, see C2), `branding.primary_color`, `branding.secondary_color`
(both `#rrggbb` strings). `billing.receipt_branding` (bool, default `true`)
stays a separate key — it already correctly means "show the instance
identity on printed receipts," not a second identity record; FR-91/92 do not
add an address/phone field to any receipt (see C5's note on the two dead
`brandingConfig` fields this removes).

`internal/platform` gains the single reader every consumer calls instead of
each hand-rolling its own `platform.Get[...]("branding")` (the root cause of
bug 1/2 above):

```go
// internal/platform/branding.go
type Identity struct {
    Name            string  // "" if unset — every consumer applies its own generic fallback
    LogoURL         *string // nil if no logo uploaded; a served path, e.g. "/api/v1/branding/logo?v=<hash>" — never a data: URI
    ThemeColor      *string // from branding.primary_color
    BackgroundColor *string // from branding.secondary_color — was NEVER populated pre-phase (brandingResponse.BackgroundColor had no setter anywhere); this is a fourth, smaller bug this phase closes as a side effect of consolidation
}

func LoadIdentity(ctx context.Context, s Settings) Identity
```

`LoadIdentity` reads `branding.name` / `branding.logo_url` /
`branding.primary_color` / `branding.secondary_color` as four independent
keys (matching how they are actually stored), tolerating any subset being
absent — never a single `"branding"` key, never a struct-tag mismatch.
`internal/portalapi`, `internal/radius/hotspot.go`, and
`internal/platform/setupapi` all call this one function; none of them
declare their own `brandingSettings`-shaped struct anymore.

### C2. Logo storage — disk-backed, validated, single current asset

Follows the exact precedent `internal/billing/ticket_attachments.go`
(v2-2, FR-78.2) already established for disk-backed uploads — same
env-var-with-default resolution, same magic-byte sniffing discipline, same
"never trust the client's declared type" posture:

```go
// internal/platform/branding.go (continued)
// HIKRAD_BRANDING_DIR, default "data/branding" — same pattern as
// HIKRAD_PAYMENT_ATTACHMENTS_DIR / HIKRAD_ACCT_SPILL_DIR.
func BrandingDir() string

// StoreLogo validates data (size, sniffed type), writes it to
// <BrandingDir>/logo.<ext> (single file — a logo is a singleton asset, not
// a per-record list like payment attachments — written via a temp file +
// rename so a crash mid-upload never leaves a half-written file being
// served), and returns the servedPath to store in branding.logo_url.
func StoreLogo(data []byte) (servedPath, contentType string, err error)

// DeleteLogo removes the stored file, if any; not an error if none exists.
func DeleteLogo() error

// ReadLogoBytes reads the current logo straight off disk (used by the
// Hotspot package builder, C4, which must inline the raw bytes into a
// self-contained zip — never a fetch of servedPath, which would require the
// router to reach hikrad-api at build time, breaking the "always available"
// contract the hotspot login page needs even if hikrad-api is briefly down).
func ReadLogoBytes() (data []byte, contentType string, ok bool)
```

Validation (`StoreLogo`, `422` on any failure, nothing written):
- **Size ceiling: 1 MiB** (`1 << 20` bytes) — the panel's pre-existing
  client-side 512 KB constant was never enforced server-side; this phase
  both enforces a real ceiling server-side and relaxes the client constant
  to match, since 512 KB was an arbitrary client-only number with no
  contract behind it.
- **Type, sniffed from bytes, never the client's `Content-Type` header or
  filename**: PNG/JPEG via `http.DetectContentType` (matches
  `ticket_attachments.go`'s existing sniffing helper — reused, not
  reimplemented); SVG via a dedicated check (Go's stdlib sniffer does not
  recognize SVG, since it's text/XML) — after trimming a UTF-8 BOM and
  leading whitespace/`<?xml …?>` prolog, the content must contain a `<svg`
  tag within its first 1 KB, and must **not** contain the literal substring
  `<script` (case-insensitive) anywhere — a defense-in-depth XSS guard; an
  `<img src=…>`-rendered SVG does not execute scripts in any current
  browser, but the check costs nothing and closes the class outright.
- **Dimensions (raster only):** decoded width/height ≤ 2048×2048 px, checked
  via Go's `image` package (`image.DecodeConfig`, no full decode needed) —
  guards against a valid-but-huge PNG bloating every manifest/hotspot-zip
  that embeds it. No dimension check for SVG (vector, no intrinsic pixel
  size).

`servedPath` embeds a cache-busting version so every consumer that already
just re-fetches `GET /api/v1/branding` to get the current `logo_url` value
(none of them hardcode it) gets a fresh URL the instant the logo changes,
with zero extra cache-invalidation logic anywhere:
`/api/v1/branding/logo?v=<sha256(data)[:12]>`.

### C3. Endpoints (fixes contract C5's pre-phase shape; response field names unchanged)

```
POST   /api/v1/settings/branding/logo   (settings.edit, multipart/form-data, field "logo")
  200  {"logo_url": "/api/v1/branding/logo?v=<hash>", ...rest of the branding group, matching every other settings-group PUT's response shape}
  422  {"error":{"code":"validation_failed","field_errors":[{"field":"logo","message":"…"}]}}

DELETE /api/v1/settings/branding/logo   (settings.edit)
  200  {"logo_url": null, ...rest of the branding group}

GET    /api/v1/branding/logo            (public, unauthenticated)
  200  raw bytes, Content-Type = the sniffed type, Cache-Control: public, max-age=31536000, immutable (safe: the URL's ?v= changes whenever the content does)
  404  no logo currently set

GET    /api/v1/branding                 (public, unauthenticated — pre-existing endpoint, contract fixed, response shape UNCHANGED)
  200  {"name": "HikRAD", "logo_url": null | "/api/v1/branding/logo?v=…", "theme_color": null | "#…", "background_color": null | "#…"}
```

`PUT /api/v1/settings/branding` (the existing generic settings-group
endpoint) **rejects `logo_url` in its body with a `422`** naming the field
("managed via POST/DELETE .../branding/logo, not this endpoint") — the same
special-casing pattern `setupapi/settings_api.go` already applies to
`remote_access.token`, closing the "arbitrary unvalidated blob via a plain
PUT" half of bug 4. `name`/`primary_color`/`secondary_color` remain plain
fields on the generic group endpoint, unchanged.

Both new endpoints live in `internal/platform/setupapi` (co-located with
every other settings-group handler); `GET /api/v1/branding` and
`GET /api/v1/branding/logo` stay in `internal/portalapi` (the existing public
route owner) but now call `platform.LoadIdentity`/`platform.ReadLogoBytes`
rather than reimplementing the read.

### C4. Hotspot captive-portal package (fixes bug 1/2 for this consumer too)

`internal/radius/hotspot.go`'s `loadBranding` is deleted; `buildHotspotPackage`
now takes a `platform.Identity` (C1) directly. The logo embedded in the
generated `login.html` is read via `platform.ReadLogoBytes` and inlined as a
base64 `data:` URI **at zip-generation time** — never `servedPath` — so the
generated Hotspot page stays fully self-contained on the router with zero
runtime dependency on `hikrad-api` being reachable (NFR-7, unchanged
requirement, now actually honored since bug 1/2 previously made this whole
code path serve nothing but the hardcoded default anyway).

### C5. Receipts — reinterpret the boolean correctly, drop the two dead fields

`internal/billing/receipt.go`'s `brandingConfig{Name, Address, Phone}` and
its `keyReceiptBrand = "billing.receipt_branding"` read are replaced:
`Address`/`Phone` have never had any settable source anywhere in the
codebase (no UI, no settings-group field ever declared them) — they are
retired outright rather than wired to a new field this phase didn't
otherwise need (out of scope per the source brief's FR-A: name, logo, accent
color only). The receipt header now calls `platform.LoadIdentity` for the
name and reads `billing.receipt_branding` as the **boolean** it has always
actually been on the writing side: `true` (default) shows
`identity.Name` (falling back to the existing hardcoded `"HikRAD"` literal
only when the instance name itself is unset, unchanged fallback posture);
`false` shows the generic `"HikRAD"` literal regardless of what's
configured (an ISP that wants a neutral receipt). No logo image is added to
the receipt template in this phase — text name only, matching FR-91/92's
stated scope; a logo on the printed receipt is a candidate future
enhancement, not attempted here.

### C6. Panel: sidebar, browser title, login screen

New `frontend/panel/src/branding.tsx` — a `BrandingProvider`/`useBranding`
pair, structurally identical to the portal's existing
`frontend/portal/src/branding.tsx` (same fallback-to-generic-while-loading
posture, same single fetch of `GET /api/v1/branding`), mounted once in
`main.tsx` above the router. `frontend/panel/src/hooks/useBranding.ts` (the
existing bare fetch hook `PrintHeader.tsx` alone uses) is replaced by this
context so there is one panel-side branding source, not two.

- **`Sidebar.tsx`** — the top mark currently rendering `t('common.productName')`
  renders the branding name (+ logo if set, else the existing initial-letter
  mark pattern the portal's `LoginPage.tsx`/`brandInitial` already
  establishes — reused verbatim, not reinvented).
- **Browser `<title>`** — `frontend/panel/index.html`'s hardcoded
  `<title>HikRAD</title>` becomes a runtime `document.title` set from
  `useBranding()` once resolved (mirrors the existing pattern note in
  `index.html` that `lang`/`dir` are already updated at runtime by the i18n
  provider — title joins that same runtime-update convention). The static
  `<title>` tag stays as the pre-fetch/no-JS fallback.
- **`LoginPage.tsx`** (panel's own, distinct from the portal's) — gains the
  same logo-or-initial + name treatment the portal's login page already has;
  today it shows neither.
- Permission for the new upload/delete endpoints is the **existing**
  `settings.edit` (`PERM_SETTINGS_EDIT`) — the source brief's draft text
  mentioned a `branding.manage` permission; this phase does not introduce
  it, since every other settings group (locale, billing, notifications, …)
  is already gated by this one generic permission and a branding-only
  exception would be an inconsistency, not a tightening.

### C7. Portal + PWA manifests — re-verify, do not rebuild

`frontend/portal/src/branding.tsx`, both apps'
`frontend/{panel,portal}/src/pwa/BrandedManifestLink.tsx`, and
`frontend/portal/src/pages/LoginPage.tsx` already call `GET /api/v1/branding`
correctly and already degrade to the generic identity on fetch failure
(NFR-7). This phase's only expected change here is that they now receive
**real data** once C3 lands — the gate proves this end-to-end rather than
assuming it. `BrandedManifestLink.tsx`'s existing guard (skip swapping the
manifest when the response is exactly the all-default shape) continues to
work unchanged, since a fresh/unconfigured install still returns exactly
that shape from the fixed endpoint.

### C8. Report print header — re-verify, do not rebuild

`frontend/panel/src/pages/reports/PrintHeader.tsx` and its `useBranding`
call already render the identity correctly; per C6, `useBranding.ts` is
replaced by the new panel-wide context, so `PrintHeader.tsx` is updated only
to import the new hook (one-line change), not to change any rendering logic.
Voucher/card print templates: none currently exist as identified server-side
print surfaces beyond the receipt (C5) and report header (this contract) —
if implementation finds an additional voucher-print surface, thread it
through `platform.LoadIdentity` the same way and note it in the gate result;
none was found during kickoff research.

### C9. Audit + fallback + offline guarantee

Every branding write (`PUT /api/v1/settings/branding`,
`POST`/`DELETE .../branding/logo`) fires the existing `settings.update`
`auth.Audit` event (FR-53.1, unchanged mechanism — the logo endpoints join
the same audit call the group-PUT handler already makes, logging `"logo"` as
the changed field, never the file bytes). An unset/never-configured instance
resolves to the pre-phase generic defaults everywhere (name `"HikRAD"`, no
logo, no custom color) — a fresh install is never broken-looking, matching
the source brief's acceptance sketch verbatim. **No code path introduced or
touched by this phase makes any outbound network request** — `StoreLogo`,
`ReadLogoBytes`, and every consumer of `platform.LoadIdentity` are pure
local-disk/DB reads (NFR-7); the gate's offline leg greps for this the same
way `scripts/lint-vendor-isolation.sh` already greps `internal/radius` for a
different invariant.

### C10. Fixed HikRAD attribution (FR-93, Decision 43) — the one place branding is deliberately NOT data-driven

Every other contract in this file threads `branding.*` settings (C1) through
a consumer. C10 is the opposite by design: a mark that must **never** read
from C1, C2, or `platform.LoadIdentity` at all, so there is no settings
field anywhere a customer's admin account could set to hide or change it.

- **Component:** one small footer component per app —
  `frontend/panel/src/shell/PoweredByFooter.tsx` and
  `frontend/portal/src/shell/PoweredByFooter.tsx` (kept as two small files
  rather than a `@hikrad/shared` export: putting it in the shared package
  would mean a customer's rebrand could theoretically be achieved by
  patching one shared file instead of two, which is a strictly *smaller*
  barrier than intended — two independent, duplicated components is the
  deliberately-chosen redundancy here, not an oversight). Content: a single
  literal string `"Powered by HikRAD"` (locale key `common.poweredBy`,
  trilingual like every other UI string — the *translation* is normal i18n,
  the brand word `"HikRAD"` inside every translation is not a
  template/interpolation slot, it's typed literally into each of the three
  locale files) at small/muted text weight, non-interactive (no click
  target, no outbound link — avoids any question of an unwanted network
  navigation and keeps the element as inert as possible).
- **Mount points:** `AppShell.tsx` (panel, below the routed content, present
  on every authenticated route) and panel `LoginPage.tsx`; `PortalLayout.tsx`
  (portal shell) and portal `LoginPage.tsx`. Both apps' installed-PWA shells
  render the same components (no separate PWA-only variant), so the mark
  survives standalone-mode launch identically to the browser-tab case.
- **Never settings-driven (FR-93.2):** `PoweredByFooter` takes no props,
  calls no branding API, and reads no settings key — it cannot be hidden by
  any `PUT` to any settings endpoint, because no settings endpoint has any
  effect on it. The gate's grep leg (item 15 below) enforces this by
  asserting the component's source contains no `useBranding`/`fetch`/
  `branding` reference at all — a structural guarantee, not a review-time
  one.
- **Scope boundary (FR-93.3):** not mounted in `PrintHeader.tsx` (reports),
  `receipt.go`'s rendered HTML, or any voucher template — those stay fully
  the ISP's own commercial documents, unchanged by this contract.
- **Residual-risk note carried from the PRD (FR-93.4):** this is a product-UI
  guarantee, not a binary-tamper guarantee — same posture as FR-82.4's
  license-cracking risk acceptance. Nothing in this phase attempts code
  obfuscation or a runtime integrity check of the footer's presence; that
  would be a different, explicitly out-of-scope class of work (and this repo
  already has a standing non-goal against exactly that, FR-82.4).

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-11.sh`;
DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, self-skip
otherwise — same convention as every prior phase):

1. **No schema change** — no new migration file above 0590; `go build`/`go vet` clean.
2. **Bug 1/2 fixed, public endpoint (DB-gated)** — `PUT /api/v1/settings/branding`
   with a name/color, then an unauthenticated `GET /api/v1/branding` returns
   that exact name/color (not the hardcoded default) — the core regression
   test for the dead-key bug.
3. **Bug 1/2 fixed, Hotspot package (DB-gated)** — with branding configured,
   the generated Hotspot zip's `login.html` contains the configured name
   (string match) and, with a logo uploaded, a base64 `data:` URI (not a
   fetchable URL) embedding it.
4. **Bug 3 fixed, receipts (DB-gated)** — with `billing.receipt_branding=true`
   (default) and a name configured, a rendered receipt shows that name;
   with it set to `false`, the receipt shows the generic literal regardless
   of the configured name.
5. **Logo upload validation (DB-gated)** — an oversized file, a `.exe`
   renamed `.png`, and an SVG containing `<script` each `422`; a valid PNG
   and a valid SVG each `200` and are byte-identical when read back from
   `GET /api/v1/branding/logo`.
6. **Logo removal falls back cleanly (DB-gated)** — `DELETE .../branding/logo`
   clears `logo_url` to `null`; a subsequent `GET /api/v1/branding` shows no
   logo, and `GET /api/v1/branding/logo` `404`s.
7. **`logo_url` rejected on the generic group PUT (DB-gated)** — `PUT
   /api/v1/settings/branding` with a `logo_url` field in the body `422`s
   naming that field; the stored value (if any) is unchanged.
8. **Audit (DB-gated)** — a logo upload and a name change each produce a
   `settings.update` `audit_log` row.
9. **Panel threading** — a component test asserts `Sidebar`/`LoginPage`
   render the branding name from a mocked `GET /api/v1/branding` (not the
   literal `"HikRAD"` string), and that `document.title` updates once the
   fetch resolves.
10. **Offline / no external fetch (grep)** — `internal/platform/branding.go`,
    the Hotspot builder, and the receipt renderer contain no `http.Get`,
    `http.Client`, or any outbound-URL construction — pure disk/DB reads.
11. **Portal + PWA regression** — the portal's existing branded-login test
    and both apps' `BrandedManifestLink` tests still pass, now against real
    (not default-shaped) data from a seeded instance.
12. **Panel/portal** — build + lint + vitest green; every new/changed string
    trilingual; `i18n:check` green (0 hardcoded, 0 missing keys across
    en/ar/ku).
13. **Full regression** — `internal/platform`, `internal/portalapi`,
    `internal/radius`, `internal/billing` unit + DB-gated suites stay green.
14. **Fixed attribution present, everywhere it should be (FR-93.1)** — a
    component test per app asserts `PoweredByFooter` (or the literal string
    `"Powered by HikRAD"`/its localized equivalent) renders on: the
    authenticated shell, the login screen, in **both** panel and portal —
    even when a full custom identity (name/logo/color) is configured via
    FR-91, proving the two coexist rather than one crowding out the other.
15. **Fixed attribution is structurally non-configurable (FR-93.2, grep)** —
    `PoweredByFooter.tsx` (both apps) contains no reference to
    `useBranding`, `branding`, or any `fetch`/settings call; no settings
    group anywhere in `backend/internal/platform/setupapi/settings_api.go`'s
    `settingsGroups` map contains a field whose name matches
    `*attribution*`/`*powered_by*`/`*footer*` — asserting the negative
    (no such toggle exists) is exactly what this leg exists to prove, not
    just that a toggle currently defaults to "on."
16. **Fixed attribution absent from print surfaces (FR-93.3)** — a rendered
    receipt (C5), a report print-header snapshot (C8), and the Hotspot
    package's `login.html` (C4) each contain no "HikRAD" attribution text
    beyond whatever the *configured* identity itself produces (i.e., an
    unbranded instance's receipt naturally says "HikRAD" as the fallback
    identity per C5 — that's C1's generic-default behavior, not C10's mark;
    this leg specifically checks for the literal `"Powered by"` phrase,
    which should appear in none of the three).
17. **Docs accuracy** — PRD carries FR-91/FR-92/FR-93; sub-PRDs 01/07/08
    carry their respective pieces (already committed in this phase's Step-1
    docs commits, verified again here); `docs/ops/known-issues.md`'s
    branding row updated from "Open" to "Fixed" once this phase's fix commit
    lands, plus any further bug found while building.

Human/hardware legs: **none** — no router/device dependency (the Hotspot
package is generated and inspected as a file, never uploaded to a live
router by this phase's own gate), same posture as v2-4/v2-6/v2-9/v2-10.

## Open implementation questions for whoever builds this (not blocking)

- **Maskable icon safe zone (source brief FR-B, PWA icons):** this phase
  serves the uploaded logo as-is for both `purpose: any` and `purpose:
  maskable` (FR-92.3, sub-PRD 07) — no server-side padding/cropping to the
  ~80%-safe-zone convention maskable icons are supposed to respect. A
  non-square or edge-to-edge logo may be clipped by some OS launchers. Noted
  as a known cosmetic limitation in sub-PRD 07, not blocking; a proper fix
  (server-side canvas padding at upload time) is a small, separable
  follow-up if it bothers a real install.
- **Receipt logo image:** C5 deliberately keeps receipts text-only (name,
  no image) — the source brief's acceptance sketch only requires the name to
  show ("a printed receipt... show Nur Net"). Adding the logo image to the
  thermal/A5 receipt template is a reasonable small follow-up, left out here
  to keep this phase's diff to what the brief actually asked for.
- **Existing uploaded logos on upgrade:** an install that already has a
  base64 data-URI `branding.logo_url` from before this phase (the old,
  unvalidated write path) will have that value silently fail C2's "must be a
  servedPath under `/api/v1/branding/logo`" assumption the moment any
  consumer expects the new shape. Given bug 1/2 above, **no consumer ever
  actually rendered that stored value correctly in the first place** (they
  all read from a nonexistent key), so there is no working install to
  regress — but the implementer should still decide at build time whether to
  (a) leave a stale data-URI value in place (harmless — `<img src>` still
  works with a data: URI, it's just no longer disk-backed or validated) or
  (b) write a one-time migration-free cleanup that clears any `branding.logo_url`
  value not matching the new served-path shape, forcing a re-upload. Either
  is acceptable; note the choice in the gate result.
