# Phase v3-3 — Licensing Hardening + Keygen CLI

> Kicked off 2026-07-22 (PRD Decision 49). First phase of the v3.x 16-item owner review (Decision 48), run **ahead of** v3-1/v3-2. SOLO, sequential, single-agent — inherits the v2/v3 execution model and every standing rule from [`../00-v3-execution-plan.md`](../00-v3-execution-plan.md). Owned by **sub-PRD 01** (platform/install/licensing). Commits **FR-103–105**.
>
> **Standing kickoff protocol applies:** this brief is presented for owner confirmation. **No feature code until the owner signs off.** Frozen contracts below are amended explicitly, never silently.

## 1. Problem (restated from the owner review, sharpened by kickoff research)

Three licensing/tooling problems from the owner's 16-item review, all grounded in the current code:

1. **Keygen is a raw flag CLI (#13).** `scripts/license-tool/main.go` issues licenses via `go run . issue -key … -keyid … -licensee … -tier … -max-subscribers … -fingerprint … -out …` (`main.go:118-183`), with required-field validation but no guidance — the operator must remember every flag and the tier vocabulary. There is no interactive mode. The tool is a standalone module (its own `go.mod`) deliberately outside `backend/` so Docker never bundles it.

2. **License reactivation can stay stuck in grace (#6), and the licensing UI mislabels/hides state (#7, #8).**
   - **Persistent grace:** `installLicense` stores `IssuedFingerprint = payload.Fingerprint` (`license_api.go:111`) — the value the vendor *signed for*. `Evaluate` (`state.go:49`) only clears to `valid` when that issued fingerprint is `WithinTolerance` (`license.go:207-224`: identical, or exactly one of the two components — machine-id / primary-MAC — differs) of the server's **current** fingerprint. So a re-issued key signed for a stale fingerprint, or a machine whose fingerprint has since drifted in **both** components, re-evaluates straight back to `grace` on upload, and the banner never clears. Nothing in the UI tells the operator what the server's *current* fingerprint actually is, so they cannot ask for a correctly-signed re-issue.
   - **Low-contrast grace banner (#7):** `LicenseBanner.tsx:36` styles the grace banner `bg-warn … text-ink-inverse`, but **`warn` is not a defined color** — `tailwind.config.ts:11-29` and `theme/tokens.css` define only `danger` and `ok`. `bg-warn` produces no background, leaving white text on the page background: near-invisible.
   - **Untranslated state labels (#8):** `LicensePage.tsx:89` renders `t(\`license.state.${license.state}\`)`, but the locale files store `license.state` **both** as a plain string (`"License state"`) and as dotted child keys (`"state.valid"`, …). The dotted-path resolver (`frontend/shared/src/i18n/messages.ts:42-49`) walks `license → state`, hits the string, and dead-ends → falls back to the raw key. The page shows the literal text `license.state.valid`. Broken identically in en/ar/ku.

3. **The licensed subscriber cap is signed but never enforced (#4).** `Payload.MaxSubscribers`/`Tier` (`license.go:64-72`) are only signed, echoed in `GET /api/v1/license`, and rendered read-only. Confirmed by grep: **no code compares `MaxSubscribers` to any live subscriber count** anywhere in v1 or v2. The only license-driven gate is `licenseGate` (`httpapi/license_gate.go`), which blocks panel writes **only** in `expired_grace` — nothing tier- or count-based. There is also no agreed tier catalog.

## 2. Scope for this implementation pass

**In scope** (FR-103–105):
- Interactive keygen/issuance CLI, non-interactive flag mode retained for CI (FR-103, contract **C1**).
- A defined tier catalog (FR-105, contract **C2**).
- Subscriber-cap enforcement: hard wall + overage buffer, at subscriber-create and CSV import (FR-105, contract **C3**).
- Fingerprint reconciliation: expose the server's current fingerprint + per-component match in the API/UI, clearer re-issue guidance (FR-104, contract **C4**).
- The two licensing-UI bug fixes: `warn` color token + `license.state.*` i18n restructure (FR-104, contract **C5**).

**Explicitly out of scope** (unchanged by construction):
- The grace **state machine** and FR-50.3 grace semantics — RADIUS auth and accounting never stop; only panel *writes* are gated in `expired_grace`. Cap enforcement (C3) is a **separate, additive** gate on *creation only*; it never touches RADIUS/accounting and never blocks a login.
- Any DRM / obfuscation / anti-debug (FR-82.4 non-goal stands).
- The signed `license.Blob` wire format, the Ed25519 keys, and `license.Verify` — untouched; C1 must emit byte-identical blobs.
- v2-5's bundle/registry pull credential and the boot/ticker re-verification in all three binaries — untouched.

## Migration budget

**None.** Cap enforcement reads a live `COUNT` against the already-signed `MaxSubscribers`; the tier catalog is a keygen-side constant carried in the existing `Tier` payload field; fingerprint display reads `license.Current()`. No schema change. The repo migration max stays **0592**; the next free number **0593** is preserved for a later v3 phase.

## Frozen contracts

### C1. Interactive license-issuance CLI (`scripts/license-tool/`)

- `license-tool issue` **with no flags** enters an interactive prompt flow (stdin/tty): **tier** (single-select from C2's catalog; selecting a named tier pre-fills `max-subscribers`, `unlimited` → `0`; a `custom` option prompts for an arbitrary positive integer), **licensee** (free text, required, non-empty), **max-subscribers** (pre-filled from tier, editable; `>= 0`, `0` = unlimited), **key-id** (required), **private-key file** (path; validated readable base64 Ed25519 seed), **fingerprint** (paste the target server's `license.Current()` value; required, non-empty — the UI in C4 is where the operator copies it from), **output path**.
- **Non-interactive mode is retained unchanged:** if any of the existing flags is passed, the tool runs exactly as today (no prompts) — CI and scripts keep working. Interactive mode is entered only when `issue` is invoked with no issuing flags and a tty is present; no-tty + no-flags is an explicit error, never a silent hang.
- **`keygen` is unchanged.**
- **Byte-for-byte invariant (hard requirement):** the emitted `license.Blob` — `json.Marshal(payload)` (not indented) + Ed25519 signature over those exact bytes — is identical to what the current flag path produces for the same inputs. The tool's hand-mirrored `payload`/`blob` structs (kept in sync with the backend because it can't import the internal package) are not restructured. A regression check signs a fixed input both ways and asserts identical bytes.
- Still a standalone module outside `backend/`; still never shipped to customers; **zero** change to any runtime binary or API.

### C2. Tier catalog

A single source-of-truth list in the license-tool, each row `{ key, label, max_subscribers }` (**owner-confirmed 2026-07-22**):

| key | label | max_subscribers |
|---|---|---|
| `starter` | Starter — 200 | 200 |
| `standard` | Standard — 500 | 500 |
| `pro` | Pro — 1000 | 1000 |
| `unlimited` | Unlimited | 0 (= no cap) |

- `Tier` in the payload remains the **label/key string** it is today — the catalog does not add a validated enum to `license.Verify` (kept minimal). `max_subscribers` is the enforced number; the tier is presentation/convenience. The `custom` option (arbitrary positive integer) stays available in the CLI for one-off sizes.

### C3. Subscriber-cap enforcement — hard wall + overage buffer

- **Where:** the subscriber-**create** path (`internal/subscribers` create handler) and the FR-6 CSV **import** execute step (`internal/importer`). Edits, renewals, and every non-create operation are never blocked by the cap.
- **Rule:** let `cap = MaxSubscribers` from the current verified license, `buffer = max(10, ceil(cap * 0.05))`. Creation is **refused** when the resulting active count would exceed `cap + buffer`. `cap == 0` (`unlimited`) is fully exempt (no count query).
- **Warn band:** when active count is `> cap` but `<= cap + buffer`, creation still succeeds and the API response/inbound dashboard surfaces a persistent renew/upgrade prompt (a `license_over_cap` warning surfaced the same way other soft warnings are; exact channel frozen with the panel wiring).
- **"Active count" definition (owner-confirmed 2026-07-22):** count of **all non-deleted subscribers** — disabled and expired subscribers **do** count toward the cap, because they still occupy a licensed slot; counting only actives would let a customer far exceed the sold size.
- **Error contract:** refusal returns the standard error envelope with code `license_subscriber_cap` (HTTP 403), `message` naming the cap and current count, so both the panel form and the import dry-run/execute surface it as a field/summary error — never a generic 500.
- **Import interaction:** the import **dry-run** reports how many rows would exceed the cap so the operator sees it before executing; execute refuses the overflow rows with the same code rather than silently truncating (no silent cap per the standing "no silent truncation" rule).
- **Independence:** this gate is evaluated from the same cached license record `RefreshLicenseCache` maintains; it is **orthogonal** to `expired_grace`/`licenseGate` and to grace semantics — an `expired_grace` install already blocks all writes anyway, and a `valid`/`grace` install enforces the cap on creates only.

### C4. Fingerprint reconciliation — surface the current fingerprint

- `GET /api/v1/license` (`setupapi/license_api.go`) and `GET /api/v1/setup/license` gain, in their response, the server's **current** fingerprint (`license.Current()`) alongside the already-returned issued fingerprint, plus a per-component match summary: whether machine-id matches, whether primary-MAC matches, and the resulting `within_tolerance` boolean (the exact predicate `Evaluate` uses). No secret is exposed — the fingerprint is already a truncated hash of machine-id + MAC.
- **License page** (`LicensePage.tsx`) shows the current fingerprint prominently with a copy button, and when state is `grace`/`expired_grace` shows a one-line diagnostic naming which component drifted ("machine-id changed" / "network card changed" / "both changed — a re-issue is required"), so the operator knows exactly what to send the vendor for a correctly-signed re-issue.
- **First-run setup wizard** license step shows the same current fingerprint (read from `GET /api/v1/setup/license`) so a fresh install is licensed against the right value the first time.
- No change to `installLicense`, `Evaluate`, `WithinTolerance`, or the grace timer — this is **display + diagnostics only**. The persistent-grace *bug* is resolved operationally (the operator can now obtain a correctly-signed key) rather than by weakening tolerance.

### C5. Licensing-UI bug fixes

- **`warn` status color:** add a `warn` token to `frontend/panel/tailwind.config.ts` and `frontend/panel/src/theme/tokens.css`, with a light/dark value that meets the same contrast bar as `danger`/`ok` against `text-ink-inverse` (an amber/warning hue). `LicenseBanner.tsx:36` keeps `bg-warn text-ink-inverse` and now renders visibly in both themes. No other consumer of a (previously no-op) `bg-warn` changes behavior — grep confirms the banner is the only user.
- **`license.state.*` i18n restructure:** resolve the string/object collision in all three locale files (`frontend/shared/locales/{en,ar,ku}/panel.json`). Rename the object children so the dotted path resolves — e.g. keep a `license.state.label` for the "License state" heading and put the values under `license.state.valid`/`.grace`/`.expired_grace` as siblings under a genuine object node (no key is both a string and a parent). `LicensePage.tsx:89` continues to call `t(\`license.state.${license.state}\`)` and now returns the translated label. The displayed English/Arabic/Kurdish text is the intended wording; `npm run i18n:check` stays green.
- Both fixes are behavior-preserving except that the intended text/contrast now actually renders. Every touched string ships trilingual in the same commit (NFR-6.1).

## Integration gate

GREEN requires all of:

1. **C1 round-trip:** `license-tool issue` in interactive mode produces a `license.Blob` that `license.Verify` accepts, and — for a fixed input set — is byte-identical to the flag-mode output (automated equality check). Flag mode still works unmodified (CI unaffected).
2. **C2:** the tier catalog exists as one source of truth; selecting a named tier pre-fills the correct `max_subscribers`; `unlimited` → `0`.
3. **C3 enforcement:** with a test license of small `MaxSubscribers`, creating subscribers up to `cap + buffer` succeeds (with the warn surfaced above `cap`), and the next create is refused with `license_subscriber_cap` (403); CSV import dry-run reports the overflow and execute refuses only the overflow rows; an `unlimited` license never blocks and never runs the count query. Edits/renewals never blocked.
4. **C4:** `GET /api/v1/license` and `/setup/license` include the current fingerprint + per-component match; the License page and setup wizard display it with copy; the drift diagnostic names the right component for machine-id-only, MAC-only, and both-changed cases (unit-tested against `WithinTolerance`).
5. **C5 banner:** the grace banner is visibly rendered (non-transparent background, passing contrast) in light and dark — verified in a jsdom/snapshot assertion that the resolved background is not empty, plus the documented visual leg.
6. **C5 labels:** the License page renders translated state labels (not `license.state.*`) in en/ar/ku; `npm run i18n:check` green.
7. **v2-12 CI floors stay green** (regardless of this phase's own items): `scripts/lint-no-native-controls.sh`, both apps' `layoutSmoke.test.tsx`, stylelint logical-properties, `npm run i18n:check`. Any new control added to the License page / wizard uses the FR-94 component set, not native chrome.
8. **No regressions:** existing licensing tests (`internal/platform/license/…`, `setupapi/…`) pass unchanged; the grace state machine and FR-50.3 semantics are demonstrably untouched (RADIUS/accounting unaffected).
9. **Docs:** every bug fixed here is updated (root cause + fix) in `docs/ops/known-issues.md`; sub-PRD 01 is updated for FR-103–105 in the same effort.

A live-browser visual confirmation of the banner/labels across light/dark × en/ar/ku is the human-review half (same posture as v2-12's documented-pending visual matrix) if no browser is available in-session.

## Owner decisions (confirmed 2026-07-22 — all five resolved)

1. **Tier catalog (C2):** `starter`=200, `standard`=500, `pro`=1000, `unlimited`=0. ✅
2. **Overage buffer (C3):** `max(10, 5% of cap)`. ✅
3. **Cap counts disabled/expired (C3):** yes — all non-deleted subscribers count. ✅
4. **Warn-band surfacing (C3):** yes — `license_over_cap` warning on the create response + a dashboard/banner prompt. ✅
5. **Setup-wizard fingerprint (C4):** yes — the current fingerprint appears on the wizard license step as well as the post-install License page. ✅
