# Phase v4-7 — Router manager II: wizard, services & write support

> **FRs:** FR-144 … FR-149 · **Migrations:** 0609–0612 · **Depends on:** v4-6 · **Blocks:** v4-11 (hotspot pages)
> **Owner sub-PRD:** 02 (RADIUS & NAS)

## 1. Intent

v4-6 made the router readable. This phase makes it configurable — the guided wizard that takes a factory router to a working PPPoE/Hotspot NAS, the service tabs, and the hotspot login page. This is where the router-ownership principle stops being documentation and starts being enforced code.

## 2. Scope

**In:** the 10-step wizard, PPPoE/Hotspot/Internet/Firewall/Update tabs, hotspot login page management, IPv6 dual-stack, and write support for v4-6's network tabs.
**Not in:** QoS and application filtering (candidate items, deliberately excluded — they are large and off the daily path).

## 3. Work

### 3.1 FR-144 — 10-step setup wizard (0609)
Steps: **detect** (probe existing config, skip what is already correct) → **bridge** → **bridge ports** → **DNS** (Google/Cloudflare/Quad9 presets, cache, optional filter) → **NTP** → **Internet** (one or more WAN uplinks: static / DHCP / PPPoE) → **RADIUS to HikRAD** (auth, accounting, CoA) → **services** (PPPoE / Hotspot / IPoE, optional IPv6) → **details & firewall** (pools, profiles, lease times, NAT, optional expired pool with redirect to the subscription-expired page) → **review & push** (the exact command list, then apply).

Every step is resumable, Back works everywhere (the first-run wizard's missing-Back bug is not repeated), and `0609` persists wizard state per NAS so a half-finished setup survives a browser close.

The review step shows **the literal commands** that will run. An operator who cannot see exactly what will be written to their router will not press the button — and should not.

### 3.2 FR-145 — Service tabs (0610)
PPPoE: servers, profiles, IP pools, accounting interim interval. Hotspot: servers, profiles, user profiles, DHCP and pools. Internet: WAN uplinks — **ours editable, pre-existing shown read-only**. Each create/edit runs through the existing FR-66/FR-67 pipeline: form → plan → preview diff → per-item resolution (keep / update / abort, **abort remains the default**) → hash-gated apply that refuses if router state moved since preview. `0610` extends `nas_services` for the additional per-service parameters the forms now expose.

Router-field inputs are dropdowns fed by discovery (interfaces, server names, pool names) with a custom escape hatch for values we own — never blind free text, which is the common cause of name mismatches.

### 3.3 FR-146 — Firewall tab (0611)
Rules list, a security analysis surface, and **auto-security snapshots with timed auto-revert**: before applying a security change, snapshot the relevant config; if the operator does not confirm within the window, revert automatically. This is the safety net for the one place where a wrong push locks the operator out of their own router. `0611` stores snapshots with their revert deadline.

### 3.4 FR-147 — Update tab
RouterOS update channel, check, apply, and package list. Read and report by default; applying a RouterOS upgrade is double-confirmed and audited, with an explicit warning that the device will reboot and sessions will drop.

### 3.5 FR-148 — Hotspot login page (0612)
Three modes: keep the router's defaults · apply our built-in branded template · a saved custom design. Branding comes from the identity resolver (never a second read). The page supports voucher-code login and reserves the self-registration tab that v4-11 fills in.

**Document the flash constraint loudly:** these pages live on router flash, so they must be re-pushed after an upgrade. SAMM operators hit this; ours will too. `0612` records which design version is currently pushed per service so the panel can *tell* the operator it is stale rather than leaving them to discover it.

### 3.6 FR-149 — IPv6 dual-stack
Opt-in during the wizard's services step: DHCPv6-PD prefix length (/48–/64, default /56), pool construction, attachment to the PPP profile, and per-subscriber static prefix pinning for business accounts. Emitted through a vendor-neutral intent like every other reply attribute.

## 4. Frozen contracts

- **Every write goes through plan → preview → per-item resolution → hash-gated apply.** No endpoint writes to a router without a preview hash. This is the phase's central contract and its central test.
- **Nothing is ever deleted from a router**, by any path in this phase. Disabling a service stops HikRAD serving it; it does not remove router config.
- Adopted objects stay read-only until explicitly adopted; adoption flips ownership and is always an operator action.
- Wizard state shape (0609) and pushed-design version tracking (0612).
- IPv6 delegation is an abstract intent; MikroTik mapping stays in the vendor adapter.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Wizard end-to-end | Factory-reset router → working PPPoE NAS authenticating a subscriber |
| 2 | Wizard detects and skips | Re-run on a configured router; already-correct steps are skipped, not re-pushed |
| 3 | Back works at every step | Manual pass, all 10 steps |
| 4 | Wizard resumes | Close the browser mid-wizard; state restored |
| 5 | Review shows literal commands | Command list matches what is actually sent, verified by API capture |
| 6 | Preview/apply gate holds | Change router state between preview and apply; apply refuses |
| 7 | Conflict resolution | keep / update / abort each verified; abort is the default |
| 8 | Nothing deleted | Full API capture across every write path shows no removal command |
| 9 | Adopted stays read-only | An adopted service refuses edit and push until adopted |
| 10 | Dropdowns fed by discovery | Interface/server/pool fields populate from the router |
| 11 | Auto-revert | Apply a lock-out firewall change, do not confirm, router self-restores |
| 12 | Hotspot page | All three modes; branding from the identity resolver; stale-after-upgrade warning appears |
| 13 | IPv6 | Subscriber receives a delegated prefix; static pin honoured |
| 14 | Migrations linear | 0609–0612 clean |
| 15 | **Real hardware, ROS 6.49 + 7.x** | Everything above on real devices. **Hardware leg, declared at kickoff** |

## 6. Commit plan

1. `feat(nas): wizard state persistence (FR-144, 0609)`
2. `feat(panel): 10-step router setup wizard with detect, Back, resume, literal review (FR-144)`
3. `feat(nas): extended service parameters and discovery-fed dropdowns (FR-145, 0610)`
4. `feat(panel): PPPoE, Hotspot, Internet tabs on the preview/apply pipeline (FR-145)`
5. `feat(nas): firewall auto-security snapshots with timed auto-revert (FR-146, 0611)`
6. `feat(panel): RouterOS update tab (FR-147)`
7. `feat(nas): hotspot login page management and pushed-version tracking (FR-148, 0612)`
8. `feat(radius): IPv6 dual-stack intent and prefix delegation (FR-149)`
9. `docs: v4-7 gate result`

## 7. Kickoff prompt

```
Execute phase v4-7 (Router manager II — wizard, services & write support) of the HikRAD v4
program, solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md
  - docs/v4/phases/phase-v4-7-router-manager-2/00-phase.md   (this brief — §4 is frozen)
  - docs/v4/phases/phase-v4-6-router-manager-1/00-phase.md   (what you are building on)
  - docs/prd/02-radius-nas-aaa.md  (FR-65, FR-66, FR-67 — the preview/apply pipeline)
  - the existing auto-setup plan/preview/apply implementation in backend/internal/radius/

Deliver FR-144 through FR-149. Migrations 0609-0612; confirm the repo maximum first.

This is where the router-ownership principle becomes enforced code rather than a documented
intention. The central contract, and the central test:

  EVERY write goes plan -> preview -> per-item resolution (keep/update/abort, abort is the
  DEFAULT) -> hash-gated apply that refuses if router state moved since preview.
  NOTHING is ever deleted from a router, by any path, ever.
  Adopted objects are read-only references until explicitly adopted by an operator action.

Gate items 6, 7, 8 and 9 exist to prove exactly that, by API capture. Do not add a
convenience path that bypasses the pipeline "just for this one field".

Other binding points:
  - The wizard's review step shows the LITERAL commands that will run. An operator who
    cannot see exactly what will be written should not press the button (gate item 5).
  - The wizard must detect and SKIP already-correct config on re-run (item 2), support Back
    at every step (item 3 — the first-run wizard's missing-Back bug is not repeated), and
    resume after a browser close (item 4).
  - Firewall auto-revert is the safety net for the one change class that can lock an
    operator out of their own router. Build it before the firewall tab is usable, not after.
  - Hotspot login pages live on ROUTER FLASH and go stale after a RouterOS upgrade. Track
    the pushed version so the panel can tell the operator, rather than leaving them to
    discover it when self-registration silently stops working.
  - QoS and application filtering are explicitly OUT of v4. Do not start them.

Gate item 15 needs real MikroTik hardware on ROS 6.49 and 7.x, including at least one
factory-reset device for the wizard run. Declared here at kickoff. Without it, mark the
phase INCOMPLETE — a wizard that has never met a real factory router is not verified.

Finish with docs/history/gates/v4-7.md.
```
