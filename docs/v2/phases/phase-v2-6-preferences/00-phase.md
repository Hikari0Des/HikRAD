# Phase v2-6 — Per-Manager Preferences & Subscriber Email

Source brief: [docs/v2/06-preferences-and-account-fields.md](../../06-preferences-and-account-fields.md). Requirements FR-84 (per-manager preferences) and FR-85 (subscriber email), committed by PRD Decision 39. FR-84 owned by sub-PRD [01-platform-install-licensing.md](../../../prd/01-platform-install-licensing.md); FR-85 owned by sub-PRD [04-subscribers-profiles.md](../../../prd/04-subscribers-profiles.md). No kickoff blockers — the source brief needed no owner decisions before scoping (small, independent, per the execution plan's own note).

## 1. Problem (restated from the source brief)

Every v1 setting is global (`platform.Settings`); a manager's language/theme live only in one browser's localStorage, so nothing follows their account across devices and nothing per-manager (landing page, table density, which alerts reach them) is configurable at all. Separately, subscribers have no email column anywhere — the portal can't collect one, receipts can't be mailed, and no future notification channel has anything to send to.

## 2. Scope for this implementation pass

1. **Schema** — `manager_preferences` (C1), `subscribers.email` (C4).
2. **Backend** — `GET/PUT /api/v1/me/preferences` self-only endpoint (C2/C3), email validation threaded into `internal/subscribers` create/edit (C4), CSV import mapping key (C5).
3. **Panel** — a "My preferences" screen off `UserMenu` (not admin-gated — every manager edits their own); login-time seeding of the theme store / `I18nProvider` from the server value, replacing the pure-localStorage flow (C6); subscriber form/list gain email; CSV import mapping UI picks up the new field automatically (it already renders `hikradFields` generically).
4. **Portal** — the existing FR-44 self-edit screen (`portalapi/me.go`'s `updateMeHandler`) gains an email field, same pattern as phone.
5. **Gate** — DB-gated tests for cross-device seeding, cross-manager isolation, the presentation-only boundary (grep), email validation, and CSV import mapping; `scripts/gate-v2-phase-6.sh`; `gate-result.md`.

Commit in reviewable chunks along the order the kickoff prompt specified: **backend prefs → panel seeding → subscriber email end-to-end** (schema+API for both features can land in one "schema+backend" chunk since they're independent and small; then panel wiring; then portal+CSV; then gate).

## Migration budget

No dedicated range was ever assigned to v2-6 in the execution plan (its listed budget, 0550–0559, was already passed by the time phases 3/4/9/2/5 ran ahead of it — expected, the plan flags this explicitly). Per the standing linear-numbering rule, this phase takes the next free numbers above the repo's actual max at kickoff time, which is **0588** (v2-2's last-used migration). **Verify this hasn't advanced before implementing** — if a stray commit added 0589+ in the meantime, take the next free number instead.

| Migration | Owns |
|---|---|
| `0589_manager_preferences` | `manager_preferences(manager_id uuid PK REFERENCES managers(id) ON DELETE CASCADE, schema_version int NOT NULL DEFAULT 1, doc jsonb NOT NULL DEFAULT '{}'::jsonb, updated_at timestamptz NOT NULL DEFAULT now())`. No seed rows — absence of a row is the valid "no preferences set yet" state (C1). |
| `0590_subscribers_email` | `subscribers` gains nullable `email text` (no DB-level format CHECK — validated in Go, same posture as the existing `phone` column). No uniqueness constraint (C4). |
| `0591`–`0599` | Reserved (follow-ups discovered during build, same convention as every prior phase's tail). Note this range overlaps v2-10's *listed* budget (0590–0599) in the execution plan — that budget is stale the moment this phase consumes any of it, per the standing rule; v2-10 takes whatever is next free when *it* builds. |

Forward-only, no `.down.sql` (repo-wide rule, Decision 25's amendment / FR-51.4).

## Frozen contracts

### C1. `manager_preferences` schema (FR-84.1) — absence is meaningful, not backfilled
```sql
CREATE TABLE manager_preferences (
    manager_id     uuid PRIMARY KEY REFERENCES managers(id) ON DELETE CASCADE,
    schema_version int NOT NULL DEFAULT 1,
    doc            jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
```
No row is created at manager-creation time and no migration backfills one for existing managers — the same "query, don't mirror" posture v2-9's `profile_cost_history` established (v2-9 C1): a manager with no row simply has every preference at its unset/default value, resolved the same way whether the row is missing or present with empty fields. `PUT` upserts (`INSERT ... ON CONFLICT (manager_id) DO UPDATE`) so the first write creates the row implicitly.

`doc`'s JSON shape (schema_version 1 — a future incompatible shape bumps this and the read path branches on it, same idea as `authview`'s cache versioning; not expected to change in this phase):
```go
// internal/auth (co-located with managers_store.go — same module owns FR-53's
// settings and now FR-84's per-manager layer over it).
type Preferences struct {
    Language          string                   `json:"language,omitempty"`           // "" | "en" | "ar" | "ku" (locales.Locale)
    Theme             string                   `json:"theme,omitempty"`              // "" | "light" | "dark" | "system"
    Numerals          string                   `json:"numerals,omitempty"`           // "" | "auto" | "latn" | "arab"
    LandingPage       string                   `json:"landing_page,omitempty"`       // a panel route path, e.g. "/dashboard"; "" = client default
    TablePageSize     int                      `json:"table_page_size,omitempty"`    // 0 (unset) | 10 | 25 | 50 | 100
    NotificationPrefs map[string]NotifChannels `json:"notification_prefs,omitempty"` // key: an FR-36 rule type, or "payment_tickets_all" (FR-80)
}
type NotifChannels struct {
    InApp bool `json:"in_app"`
    Push  bool `json:"push"`
}
```
`""` / `0` / an absent map key all mean "unset — the client falls back to its own default (browser language, `system` theme, etc.)," never "explicitly set to the zero value." This mirrors `I18nProvider`'s existing `defaultLocale`/`defaultNumerals` fallback chain (`locale → en → key`) — the server preference is one more link prepended to that chain, not a replacement of it.

`notification_prefs` keys are validated against a closed set at write time: the nine existing `monitorsvc.validRuleTypes` (`nas_down`, `nas_up`, `device_down`, `device_up`, `radius_reject_spike`, `acct_backlog`, `disk_low`, `expiring_digest`, `agent_balance_low`) plus `payment_tickets_all` (FR-80's "a manager above the owning agent may opt into all-tickets notifications" — the one payment-ticket-specific key, not an alert rule type). An unknown key is a `422` validation error, not silently stored — a typo must never look like it took effect.

### C2. `GET /api/v1/me/preferences` (FR-84.2)
Any authenticated manager; always resolves the caller from `auth.ManagerFrom(ctx)` (the access-token identity) — **the route takes no id parameter**, so "another manager's preferences" is not an addressable resource through this endpoint at all, by construction (stronger than a runtime check that could be forgotten).
```
200 OK
{
  "language": "ku",
  "theme": "dark",
  "numerals": "auto",
  "landing_page": "/dashboard",
  "table_page_size": 25,
  "notification_prefs": {
    "nas_down": {"in_app": true, "push": true},
    "payment_tickets_all": {"in_app": true, "push": false}
  }
}
```
A manager with no `manager_preferences` row gets `200` with every field at its zero value (`{}` effectively) — **never `404`**; "no preferences set" is the common, valid state for every manager on a fresh install, not an error condition.

### C3. `PUT /api/v1/me/preferences` (FR-84.2) — full-document replace
```
PUT /api/v1/me/preferences
Body: same shape as C2's response, every field optional.
200 OK -> the resulting document, in the same shape as GET (so the caller can trust the write without a re-fetch).
422 on any invalid enum value, an unknown notification_prefs key, or an out-of-range table_page_size,
    field_errors naming the JSON path, e.g. {"field":"theme","message":"..."} or
    {"field":"notification_prefs.nas_down","message":"unknown notification key"}.
```
**Replace, not merge**: an omitted field reverts to unset on this write (standard PUT semantics) — the panel's preferences screen always submits the whole document it just read via `GET`, so this is never a footgun in practice, and it avoids the ambiguity of "does omitting `theme` mean 'don't touch it' or 'clear it'" that a partial-patch endpoint would carry. `internal/auth` exposes:
```go
func GetPreferences(ctx context.Context, db *pgxpool.Pool, managerID string) (Preferences, error)
func SetPreferences(ctx context.Context, db *pgxpool.Pool, managerID string, p Preferences) error // upsert
```

### C4. Subscriber email (FR-85.1)
```sql
ALTER TABLE subscribers ADD COLUMN email text;  -- nullable, no uniqueness constraint
```
Validated in `internal/subscribers/normalizeWrite` (`api.go`) exactly alongside the existing phone check — a new `validateEmail(s string) bool` (simple RFC-5322-shaped check: one `@`, a non-empty local part, a domain part containing at least one `.`, no whitespace; not a full grammar, matching the existing phone validator's "reject clearly wrong, don't over-engineer" posture) feeding the same `add("email", "not a valid email address")` field-error path already used for phone. `normalized` gains `emailPtr *string`. No encryption (email is not a credential, unlike the RADIUS password — contrast NFR-4.2). Write path: create/edit accept `email *string` in the request body; empty string clears it to `NULL` (same convention `phone`/`address` already use).

Every write touching `email` is audit-logged via the existing before/after mechanism (`auth.Audit`), no special-casing needed — it is already a plain column on the row `normalizeWrite`/the update handler diff.

### C5. CSV import mapping key (FR-85.2 / FR-6)
`backend/internal/importer/preset.go`:
```go
var hikradFields = map[string]bool{
    "username": true, "password": true, "name": true, "phone": true,
    "address": true, "profile": true, "expires_at": true, "service_type": true,
    "email": true, // NEW (FR-85)
}
var presets = map[string]map[string]string{
    "sas4": {
        "username": "UserName", "password": "Password", "name": "FullName",
        "phone": "Mobile", "address": "Address", "profile": "Package",
        "expires_at": "ExpireDate",
        "email": "Email", // NEW — matched case-insensitively like every other preset column, only if present in the uploaded header
    },
}
```
Dry-run validation reuses C4's `validateEmail` per row; a malformed cell is a per-row dry-run error (never a silent drop, never a partial write — matches FR-6's existing "zero rows written until every row is clean" contract). The mapping UI needs no code change: it already renders `hikradFields` generically, so `email` simply appears as one more mappable target.

### C6. Panel/portal client seeding (FR-84.4)
- **Panel** (`frontend/panel/src/main.tsx` / `AuthProvider`): after a successful login (and on session restore, if a valid token already exists), fetch `GET /api/v1/me/preferences` once and apply it — `setThemePreference(prefs.theme)` (`@hikrad/shared` theme store, only when `prefs.theme` is non-empty; leaves the localStorage-detected value alone otherwise) and `setLocale`/`setNumerals` (`I18nProvider`, same non-empty guard) — server value wins over whatever localStorage held from a different device. `landing_page`/`table_page_size`/`notification_prefs` are read by the screens that need them (dashboard redirect, list page-size default, the alerts/notification settings), not by this seeding step itself.
- A new **"My preferences" panel screen** (`frontend/panel/src/pages/preferences/MyPreferencesPage.tsx`, linked from `UserMenu`, **no permission gate** — every manager edits only their own row) edits the full document and `PUT`s it on save.
- **Portal**: out of scope for this phase — the source brief and FR-84 are manager-only; the portal's existing `PUT /portal/language` (subscriber-scoped, migration 0301) is untouched and not merged into this table.
- localStorage remains the pre-login/offline fallback exactly as it already works — this phase adds a seed-from-server step, it does not remove the existing detection chain.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-6.sh`; DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, self-skip otherwise — same convention as every prior phase):

1. **Schema & migration** — `0589_manager_preferences` / `0590_subscribers_email` present, no `.down.sql`; `go build`/`go vet` clean.
2. **No-row default is 200, not 404 (C1/C2)** — a manager with zero `manager_preferences` rows gets `GET /me/preferences` → `200` with every field at its zero value.
3. **Cross-device seed test (AC-84a)** — manager A `PUT`s `{theme:"dark", language:"ku"}`; a second, independent `GET /me/preferences` call (simulating a second device/session — no shared client state) returns the same values, proving the value followed the account, not a browser.
4. **Cross-manager isolation (AC-84b)** — manager A `PUT`s preferences; manager B (a distinct account, authenticated separately) calls `GET /me/preferences` and gets B's own (still-default) document, never A's; a `PUT` body carrying a spoofed `manager_id`/`id` field is ignored (the row written is always the caller's own — proven by asserting the *other* manager's row is untouched, since the endpoint has no id parameter to even attempt redirecting the write).
5. **Presentation-only boundary never crossed (FR-84.3)** — a grep leg over `internal/auth` (excluding this phase's own preferences files), `internal/radius`, and `internal/billing` asserting no code path reads `manager_preferences`/`Preferences`/`notification_prefs` to decide a permission check (`Can`), a `ScopeFilter` result, or any monetary amount/currency — mirrors v2-9's "no ancestry column" grep pattern (gate item 8 of that phase).
6. **Validation (C3)** — an invalid `theme`/`language`/`numerals` value, an out-of-range `table_page_size`, and an unknown `notification_prefs` key each `422` with a `field_errors` entry naming the offending path; nothing is written on a rejected `PUT` (the pre-existing document, or its absence, is unchanged).
7. **Email validation (C4)** — a valid email persists and round-trips on the subscriber read shape; a malformed value `422`s with a `field_errors` entry on `email` and writes nothing (same shape as the existing phone-validation test).
8. **CSV import mapping (AC-85b, C5)** — the `sas4` preset maps a header named `Email` (case-insensitively) automatically; a dry run over rows with valid and malformed emails reports the malformed ones as per-row errors and writes zero rows; a subsequent import of the corrected file creates exactly the valid rows with `email` populated.
9. **Full regression** — the pre-existing `internal/auth`, `internal/subscribers`, and `internal/importer` DB-gated suites pass unchanged (this phase adds a table and a nullable column, it must not change any existing login/CRUD/import outcome when no preference/email data exists).
10. **Panel/portal** — build + lint + vitest green; the preferences screen exists and calls `GET/PUT /me/preferences`; the subscriber form/list render the email field; the portal self-edit screen renders it too; `i18n:check` green (0 hardcoded strings, 0 missing keys across en/ar/ku).
11. **Docs accuracy** — PRD/sub-PRDs 01 and 04 reflect FR-84/FR-85 (done in this brief's own Step 1 commit, before this file); `docs/ops/known-issues.md` carries any bug found while building, dated the day it's found.

Human/hardware legs: none — this phase has no router/device/hardware dependency (same posture as v2-4/v2-9).

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- **`landing_page`'s value space** is intentionally left as a free-form string rather than a server-side enum of valid panel routes in this brief — the panel's route table is frontend-owned and changes independently of the backend; validating it server-side would create a second place to keep routes in sync. If a bad value is stored (a route later removed), the panel should fail soft (fall back to the dashboard), not error — worth a one-line note in the panel screen's own comments when built, not a schema constraint.
- **`payment_tickets_all`'s actual consumption** (wiring the FR-80 "notified on every ticket, not just their own" behavior to this preference key) is *not* built in this phase — FR-84 only guarantees the key exists, validates, and round-trips. The v2-2 payment-ticket notification code (already shipped) does not yet read it; wiring that read is a small follow-up left to whoever next touches `internal/billing`'s ticket notifications, noted here so it isn't lost.
