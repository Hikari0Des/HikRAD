# HikRAD — v3 Backlog Index (parking list)

> Established 2026-07-17 by owner request, **while v2 is still in progress** (v2 phase 1 complete at the time). This is a *parking list*: items the owner reports mid-v2 land here so they are never forgotten, and **must not be interleaved into v2 phases uninvited** — a v2 session fixes what its own scope touches and parks everything else here.
>
> These are **not yet briefed**. When v2 completes (or the owner explicitly pulls an item forward), each item gets the same treatment as v2 features: a self-contained mini-PRD + AI kickoff prompt file in this directory, a `phases/00-v3-execution-plan.md` ordering them into sequential single-agent phases with frozen contracts and gates, and master-PRD amendments (FR numbers continue from wherever v2 stops). Until then, this index is the whole record — keep the owner's intent verbatim enough that a future session can brief it without guessing.
>
> **Migrations: v3 reserves 0600–0689** (partitioned per phase when the execution plan is written). v2 owns 0500–0589; v1.x maintenance stays in 04xx.

> **2026-07-17 owner follow-up ("if applicable now, implement now")**: the small
> items were pulled forward into v1.x the same day — **v3.4** (dashboard NAS
> uuid), **v3.5** (manager removal: disable + guarded hard delete, migration
> 0505), **v3.6** (`hikrad factory-reset`), and v3.1's worst *symptom* (themed
> slim scrollbars replacing the OS-chrome look). What remains below for v3 is
> what genuinely needs its own phase: v3.1's full responsive audit, v3.2, v3.3.

## v3 items (owner, 2026-07-17 — unordered until briefed)

| # | Item | What the owner asked for | Notes / dependencies |
|---|---|---|---|
| v3.1 | **Frontend modernization & responsiveness pass** | "Improve frontend, make it more responsive, remove its bugs — like the legacy scrollbar showing when the web can't fit the screen, etc. — modernize it." | A repo-wide UI audit of panel + portal at phone/tablet/desktop widths: kill overflow-caused legacy scrollbars (containers should wrap/scroll internally, the body never horizontally), modern polish (spacing, transitions, loading/empty states), keeping RTL + i18n + LTR-islands (usernames/MACs/IPs) intact. Symptoms are tracked in [known-issues](../ops/known-issues.md). Schedule **after** v2's UI-touching phases so the pass isn't redone. **Partial pull-forward 2026-07-17**: scrollbars are now slim + theme-colored in panel and portal (`index.css`) — the *appearance* half; the *why-does-it-overflow* audit is still this item. |
| v3.2 | **Customizable dashboard per manager** | "Adjustable and customizable dashboard per manager." | Widget catalog (live sessions, revenue, NAS health, alerts…), per-manager choice/order/size persisted server-side. Builds directly on v2-4's per-manager preferences storage — brief it as an extension of that model, respecting permission gating (a widget a manager can't view is not offered). |
| v3.3 | **Instance branding: company logo + name** | "Per-company logo and instance name configuration." | Owner-configurable instance name + uploaded logo shown on panel/portal login, headers, PWA manifest/icons, printed receipts and vouchers. Asset stored locally (NFR-7: no internet dependency). Settings > Branding screen, `branding.manage` permission. |
| ~~v3.4~~ | **Dashboard NAS shows id/uuid instead of name** (bug) — **SHIPPED in v1.x, 2026-07-17** | "Clicking a NAS device in dashboard gives its id/uuid instead of its name." | The NAS *status page* (`/nas/:id/status`, the dashboard card's target) titled itself with the route uuid; the device status page had the same bug. Both now resolve and show the entity's name (+ IP subtitle). See [known-issues](../ops/known-issues.md). |
| ~~v3.5~~ | **Manager removal** — **SHIPPED in v1.x, 2026-07-17** | "No managers remove option." | Shipped as **disable** (blocks login/refresh, revokes sessions; migration 0505 — see known-issues for why not 0413) + **guarded hard delete** (`DELETE /managers/{id}`, `managers.delete` perm): never yourself, never the last active admin-capable manager, and a manager with ledger history physically cannot be hard-deleted (the append-only ledger trigger rejects the FK `SET NULL`) — the API maps that to "disable instead", and the panel offers exactly that. |
| ~~v3.6~~ | **Factory reset** — **SHIPPED in v1.x, 2026-07-17** | "Add factory resetting — wiping the whole database and clearing all data without installing a fresh VM." | Shipped as `hikrad factory-reset [--yes] [--no-backup]`: safety backup → stack down → delete the data dir (append-only tables make a fresh cluster the only clean zero state) → recreate bind-mount targets with install.sh's ownerships → empty the generated FreeRADIUS client list → fresh boot into the first-run wizard (license re-entered there). Keeps images, `.env`, backups, cron, CLI. Docs: [install-guide.md](../ops/install-guide.md). |

## Rules already binding on v3 (inherited from v2)

Sequential single-agent execution, frozen-contract phase briefs, integration gates with written `gate-result.md`, docs updated in the same phase, every bug recorded in [docs/ops/known-issues.md](../ops/known-issues.md) — see [docs/v2/phases/00-v2-execution-plan.md](../v2/phases/00-v2-execution-plan.md) §Standing rules. Commits pushed with clear messages as work lands.
