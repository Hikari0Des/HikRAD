# Phase v4-6 — Router manager I: monitoring & network tabs

> **FRs:** FR-139 … FR-143 · **Migrations:** 0606–0608 · **Depends on:** v4-1, v4-2 · **Blocks:** v4-7
> **Owner sub-PRD:** 02 (RADIUS & NAS)

## 1. Intent

SAMM's device manager is the feature operators demo first: a full MikroTik configuration surface over the RouterOS API, no WinBox, no SSH. HikRAD has the harder half already — read-only config inspection, service discovery, hash-gated preview/apply, and the router-ownership model — but exposes almost none of it as a device UI.

This phase builds the **read and monitor** surface. v4-7 adds the write-heavy tabs and the wizard. Splitting it this way means every tab ships read-only first, and write support only ever arrives behind the existing preview/apply pipeline.

## 2. Scope

**In:** the monitor grid, the device page shell, and the Overview, Interfaces, General, Bridge, VLAN, IP and DNS tabs.
**Not in:** PPPoE/Hotspot/Internet/Firewall/Update tabs, the wizard, hotspot login pages, IPv6 (all v4-7). QoS and app filtering are out of v4 entirely — candidate items.

## 3. Work

### 3.1 Data (0606–0608)
- **0606** — `device_metrics` hypertable: CPU, memory, session count, uptime, per-device, sampled by the device worker.
- **0607** — `device_interfaces` and `interface_traffic` (hypertable) for per-interface state and history.
- **0608** — `device_config_cache`: the last-read router state per object class, with the hash the preview/apply pipeline already uses, so a tab renders instantly and refreshes explicitly rather than hitting the router on every paint.

### 3.2 FR-139 — Monitor grid
Reachability dot, RouterOS version, CPU load and cores, memory use, session count, uptime, and NAS-vs-monitor tag. Auto-refresh on the existing worker cadence with an explicit "refresh now". Sourced from the device worker's samples, never from a synchronous API call in the request path — a slow or dead router must never hang the page.

### 3.3 FR-140 — Device page shell
Tab groups: **Monitoring** (Overview, Interfaces) · **Manager** (General) · **Network** (Bridge, VLAN, IP, DNS). Every tab reads through the existing read-only inspection path. A router that is unreachable renders the cached state with a clear staleness marker — never an error page, never a blank tab.

Every object shown carries its **ownership badge**: system-managed (ours, editable in v4-7) or router-adopted (read-only reference). This badge is the visible face of the router-ownership principle and appears from the first tab onward.

### 3.4 FR-141 — Overview and Interfaces
Overview: identity, model, RouterOS version, serial, uptime, and CPU/memory/session history charts from `device_metrics` using v4-1's chart palette. Interfaces: live per-interface state (up/down, speed, MAC, MTU) and per-interface traffic history.

### 3.5 FR-142 — General tab
Identity, time/timezone/NTP, and the reboot and ping tools. **Reboot is the first genuinely destructive router action in the product** — double-confirmed, permission-gated, audited with the operator's identity, and refused for router-adopted devices the operator has not explicitly taken responsibility for.

### 3.6 FR-143 — Network tabs (read-only in this phase)
Bridge (bridges and member ports), VLAN (VLAN interfaces), IP (static addresses, DHCP client, DHCP server **with the live lease table**), DNS (resolvers, cache, static entries). All read-only here; write support lands in v4-7 behind preview/apply.

The DHCP lease table is worth building carefully: it is the operator's answer to "which device is that?" and it is what makes the IPoE/Option-82 work later possible.

## 4. Frozen contracts

- `GET /api/v1/nas/{id}/device/{tab}` — one read endpoint shape per tab, all returning `{data, read_at, stale, ownership}`.
- The router-ownership badge is part of every object payload, never inferred client-side.
- No tab in this phase issues a write to a router. Enforced by a test asserting the RouterOS client is used in read-only mode throughout, and by code review as a gate item.
- Device metrics sampling cadence is the existing worker interval setting — not a new one.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Monitor grid | Real or simulated router shows version, CPU, memory, sessions, uptime |
| 2 | Dead router degrades | Unreachable device renders cached state with a staleness marker, page loads normally |
| 3 | No synchronous router call in request path | Timed test: a 30s-hanging router does not delay the page |
| 4 | Every tab renders | Overview, Interfaces, General, Bridge, VLAN, IP, DNS against a real device |
| 5 | Ownership badge everywhere | Every object carries system/adopted; adopted objects offer no edit control |
| 6 | Zero writes | Packet/API capture during a full tab sweep shows read commands only |
| 7 | Reboot guarded | Double-confirm, permission-gated, audited; refused where ownership is not established |
| 8 | DHCP leases live | Lease table matches the router's own view |
| 9 | Charts use tokens | No raw color; v4-1 chart palette only |
| 10 | Migrations linear | 0606–0608 clean |
| 11 | **Real hardware** | All of the above against a real MikroTik on both ROS 6.49 and 7.x. **Hardware leg, declared at kickoff** |

## 6. Commit plan

1. `feat(nas): device metrics and interface history tables (FR-139, FR-141, 0606-0607)`
2. `feat(nas): config cache keyed by the existing preview hash (FR-140, 0608)`
3. `feat(nas): device worker samples metrics and interfaces (FR-139)`
4. `feat(panel): device monitor grid (FR-139)`
5. `feat(panel): device page shell with tab groups and ownership badges (FR-140)`
6. `feat(panel): Overview and Interfaces tabs (FR-141)`
7. `feat(panel): General tab with guarded reboot and ping (FR-142)`
8. `feat(panel): Bridge, VLAN, IP with DHCP leases, DNS — read-only (FR-143)`
9. `docs: v4-6 gate result`

## 7. Kickoff prompt

```
Execute phase v4-6 (Router manager I — monitoring & network tabs) of the HikRAD v4
program, solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md
  - docs/v4/phases/phase-v4-6-router-manager-1/00-phase.md   (this brief)
  - docs/prd/02-radius-nas-aaa.md  (FR-65-67, FR-107-110 — the existing router surface)
  - backend/internal/radius/ — the RouterOS API client, discover.go, config inspection,
    and the hash-gated preview/apply pipeline you must reuse rather than reinvent
  - docs/ops/ros-matrix.md

Deliver FR-139 through FR-143. Migrations 0606-0608; confirm the repo maximum first.

HikRAD already has the hard half of this: read-only config inspection, service discovery,
hash-gated preview/apply, and the router-ownership model. This phase is the UI that finally
exposes it. Reuse those paths — do not write a second RouterOS client or a second
inspection path.

Binding rules:
  - THIS PHASE WRITES NOTHING TO A ROUTER. Every tab is read-only. Gate item 6 captures the
    API traffic during a full tab sweep and asserts read commands only. Write support is
    v4-7's, and only behind the existing preview/apply pipeline.
  - The single exception is the General tab's reboot/ping tools, which are actions rather
    than config writes — double-confirmed, permission-gated, audited, and refused where
    ownership is not established.
  - No synchronous RouterOS call in the HTTP request path. A slow or dead router must never
    hang a page (gate items 2 and 3). Serve from the worker's samples and the config cache;
    refresh explicitly.
  - Every object carries its ownership badge (system-managed vs router-adopted) in the
    payload, never inferred client-side. Adopted objects render no edit control at all.
  - Charts use v4-1's chart palette tokens. No raw color anywhere.

Gate item 11 needs a real MikroTik on ROS 6.49 and 7.x. Declared here at kickoff. If no
hardware is available, say so in the gate result and mark the phase INCOMPLETE rather than
GREEN-with-pending.

Finish with docs/history/gates/v4-6.md.
```
