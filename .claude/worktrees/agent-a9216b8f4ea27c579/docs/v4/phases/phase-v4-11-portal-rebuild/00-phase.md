# Phase v4-11 — Client portal rebuild, auto-login & self-registration

> **FRs:** FR-166 … FR-172 · **Migrations:** 0629–0633 · **Depends on:** v4-1, v4-4, v4-5, v4-9, v4-10
> **Owner sub-PRD:** 07 (portal) + 05 (payments) + 02 (session lookup) · **Decisions committed at kickoff:** 64
> **Contains a credential-free authentication path. §3.2's threat model is binding.**

## 1. Intent

The subscriber portal is the surface most customers actually touch, and it is HikRAD's thinnest. It shows status and usage, redeems a code, and submits a payment ticket. It cannot show an invoice, raise a ticket, manage notification preferences, or explain a throttle. Meanwhile every subscriber who opens it is already authenticated *at the network layer* — they are online through our own NAS, on a session we are tracking — and we still make them remember a password.

This phase rebuilds the portal on v4-1's tokens with the full surface, and adds the two features that make it feel like the ISP's own app: **connected-client auto-login** and **self-service guest registration**.

## 2. Scope

**In:** the portal rebuild, auto-login, captive-portal hand-off, self-service registration, and the portal's ticket, invoice and preference surfaces.
**Not in:** online payment gateways (Decision 67), the panel side of anything here (already built in v4-9/v4-10).

## 3. Work

### 3.1 FR-166 — Portal rebuild
On v4-1's tokens, trilingual, true RTL, PWA, phone-first:
- Status, expiry, **consumed data** — and never the quota ceiling or remaining balance (Decision 21 stands, deliberately).
- **Current effective speed**, including a plain-language explanation when it is reduced: which limit, and when it restores. A subscriber who can see "your daily allowance is used, full speed returns at midnight" does not call the front desk.
- Usage over 7/14/30/90 days, session history.
- Invoices with PDF (FR-171, from v4-9), payment history, the unified Pay screen (unchanged mechanism).
- Tickets (FR-170, from v4-10), notification channel preferences (FR-170, from v4-10).
- Self-service profile edit: password, phone with confirmation, email.

### 3.2 FR-167 — Connected-client auto-login (Decision 64)

**A subscriber opening the portal from a device with a live session is signed in without typing credentials.** This is a credential-free authentication path, so it is specified as a threat model first and a feature second.

**How it works.** The portal calls `POST /portal/api/v1/session/auto`. The server takes the request's source IP, looks it up against the live-session table (`live:sessions`, which already carries framed IP per session), and if — and only if — every condition below holds, issues a portal token for that subscriber.

**Conditions, all required:**

| # | Condition | Why |
|---|---|---|
| 1 | The feature is **enabled** for this instance | Off by default. An operator opts in knowingly. |
| 2 | Source IP falls inside an operator-configured **trusted network** list | Prevents the whole class of attack where a request from the internet claims a subscriber IP. |
| 3 | The request arrived **without an untrusted proxy chain** — `X-Forwarded-For` is honoured only from configured trusted proxies | Otherwise a header spoofs the source IP outright. |
| 4 | The IP matches **exactly one** live session | Ambiguity — CGNAT, shared NAT, a re-used lease — **refuses**. It never picks the newest or the "most likely". |
| 5 | Where the NAS supplies a MAC, it **corroborates** the session | A second factor at no cost to the user, on hotspot in particular. |
| 6 | The account is active | An expired or disabled account logs in normally, so it can renew. |

**What the resulting session may do.** A **reduced scope**: view everything, renew, and pay. It may **not** change the password, contact details, notification channels, payment configuration, or anything that would let someone who walked onto the network take the account over. Any of those requires a **password step-up** — the portal prompts, the subscriber types their password once, and the session upgrades to full for its remaining lifetime.

**Session lifetime** is short and configurable (default 30 minutes), separate from a password login's lifetime, and does not refresh silently.

**Auditing.** Every auto-login writes an audit row with source IP, MAC, NAS, session id, and the resulting scope. Every *refusal* is counted, and a spike is an alert condition — a burst of ambiguous-IP refusals is what an attack looks like.

`0629` stores the feature toggle, trusted networks, trusted proxies, and session TTL. `0630` records auto-login events.

### 3.3 FR-168 — Captive-portal hand-off
MikroTik's hotspot redirect carries MAC and IP parameters. These are **hints, validated against the live-session table**, never trusted on their own — a parameter in a URL is attacker-controlled by definition. The validation path is the same one FR-167 uses; there is no second, looser implementation for the captive case.

### 3.4 FR-169 — Self-service guest registration (0631–0632)
Visitor connects → taps Register on the hotspot page → enters name and mobile → receives a passcode by SMS/WhatsApp/email (v4-10's channels) → enters it → gets a real, named account.

Abuse controls, all required:
- **One live passcode per mobile number.** A re-ask during validity says "check your messages" rather than sending another.
- A **waiting period** after expiry before a new request.
- Validity counts from **send** time, not from when the visitor gets around to typing it.
- **Every attempt recorded against the device MAC**, which the visitor cannot fake from a browser.
- **Daily volume capped by licence entitlement** (v4-3's `self_service` entitlement).

Setup order is a documented prerequisite: a messaging channel must be configured first, then the feature enabled, then the login page **pushed to the router** — and re-pushed after a RouterOS upgrade, because the pages live on flash (v4-7's pushed-version tracking is what warns the operator).

Fully customisable page text: register tab label, intro line, field labels, button, and every reply message — trilingual.

### 3.5 FR-172 — OTP login for passwordless accounts (0633)
Closes the long-open known-issues row: hotspot subscribers created without a password (v1.1's `no_password`) currently cannot use the portal at all. They log in with a one-time code to their registered number, reusing FR-169's passcode machinery.

## 4. Frozen contracts

- `POST /portal/api/v1/session/auto` — returns a token with an explicit `scope` field; the client never infers scope.
- **Scope is enforced server-side on every endpoint.** A reduced-scope token calling a full-scope endpoint gets 403 regardless of what the UI offered.
- Ambiguous IP → refuse. There is no tie-break rule, and adding one later requires an amendment and a security review.
- Captive-portal parameters are validated through the same path as FR-167. No second implementation.
- The portal never displays the quota ceiling or remaining balance (Decision 21).
- Passcode issuance goes through v4-10's queue like every other message.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Portal surface complete | Status, speed-with-reason, usage, sessions, invoices+PDF, payments, tickets, preferences, profile edit |
| 2 | Throttle explained | A throttled subscriber sees which limit and when it restores |
| 3 | Auto-login works | From a device with a live session on a trusted network, portal opens signed in |
| 4 | **Off by default** | Fresh install: auto-login disabled; the endpoint refuses |
| 5 | **Untrusted network refused** | Same request from outside the trusted list is refused |
| 6 | **Spoofed XFF refused** | `X-Forwarded-For` from an untrusted proxy does not change the decision |
| 7 | **Ambiguous IP refused** | Two live sessions on one IP → refused, not resolved |
| 8 | **MAC mismatch refused** | Where the NAS supplies a MAC and it disagrees, refused |
| 9 | Reduced scope enforced server-side | Reduced token calls password-change, contact-change, payment-config → 403 each |
| 10 | Step-up works | Password prompt upgrades the session; the upgrade is audited |
| 11 | TTL honoured | Session expires at the configured TTL and does not silently refresh |
| 12 | Every auto-login and refusal audited | Rows contain IP, MAC, NAS, session id, scope |
| 13 | Captive params validated | A forged MAC/IP in the redirect URL is refused |
| 14 | Self-registration end-to-end | Register → passcode → account, on a real hotspot |
| 15 | Abuse controls | One-live-passcode, cooldown, send-time validity, per-MAC recording, entitlement cap — each verified |
| 16 | OTP login | A passwordless hotspot account signs in |
| 17 | Trilingual + RTL | i18n gate green; RTL pass on every new screen |
| 18 | **Security review** | Written review of the auto-login path recorded in the gate result |
| 19 | Migrations linear | 0629–0633 clean |

## 6. Commit plan

1. `feat(portal): rebuild on v4-1 tokens — status, usage, sessions, profile (FR-166)`
2. `feat(portal): effective speed with plain-language throttle explanation (FR-166)`
3. `feat(portal): invoices with PDF and payment history (FR-171)`
4. `feat(portal): tickets and notification preferences (FR-170)`
5. `feat(portal): auto-login config — toggle, trusted networks, trusted proxies, TTL (FR-167, 0629)`
6. `feat(portal): connected-client auto-login with reduced scope (FR-167, 0630)`
7. `feat(portal): server-side scope enforcement and password step-up (FR-167)`
8. `feat(portal): captive-portal parameter validation through the same path (FR-168)`
9. `feat(portal): self-service guest registration with abuse controls (FR-169, 0631-0632)`
10. `feat(portal): OTP login for passwordless hotspot accounts (FR-172, 0633)`
11. `docs: v4-11 gate result`

## 7. Kickoff prompt

```
Execute phase v4-11 (Client portal rebuild, auto-login & self-registration) of the HikRAD
v4 program, solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md              (Decision 64)
  - docs/v4/phases/phase-v4-11-portal-rebuild/00-phase.md   (this brief — §3.2's threat
    model and §4 are BINDING, not advisory)
  - docs/prd/07-subscriber-portal-pwa.md      (FR-41-44, FR-54)
  - backend/internal/portalapi/  (login.go, middleware.go, sessions.go, tokens.go, ratelimit.go)
  - backend/internal/live/       (the live-session state you will match source IPs against)

Deliver FR-166 through FR-172. Migrations 0629-0633; confirm the repo maximum first.

FR-167 IS A CREDENTIAL-FREE AUTHENTICATION PATH. Build it as a threat model first and a
feature second. All six conditions in §3.2 are REQUIRED, not a menu:

  1. Feature enabled for the instance — OFF BY DEFAULT.
  2. Source IP inside an operator-configured trusted network.
  3. X-Forwarded-For honoured ONLY from configured trusted proxies.
  4. The IP matches EXACTLY ONE live session. Ambiguity REFUSES. There is no tie-break,
     no "pick the newest", no "most likely". CGNAT and shared NAT are the normal case in
     this market and a wrong guess hands one subscriber another's account.
  5. MAC corroboration wherever the NAS supplies it.
  6. Account active — expired accounts log in normally so they can still renew.

The resulting session is REDUCED SCOPE: view, renew, pay. It may NOT change password,
contact details, notification channels, or payment configuration. Those need a password
step-up. Scope is enforced SERVER-SIDE on every endpoint (gate item 9) — a reduced token
hitting a full-scope endpoint gets 403 regardless of what the UI chose to render. A
client-side scope check is not a security control.

Captive-portal MAC/IP parameters are attacker-controlled by definition. They are HINTS,
validated through the SAME path as FR-167 (gate item 13). Do not write a second, looser
implementation for the captive case — that is exactly where this kind of feature breaks.

Audit every auto-login AND every refusal with IP, MAC, NAS, session id and scope. A spike
in ambiguous-IP refusals is what an attack looks like; make it visible.

Everything else this phase ships (portal surface, self-registration, OTP login) is
conventional work — do it well, but spend the care budget on FR-167.

Gate items 3-13 are the security legs and gate item 18 is a written security review. If you
cannot exercise the auto-login path against real live sessions, say so and mark the phase
INCOMPLETE. A credential-free login path that has not been attacked in testing is not done.

Finish with docs/history/gates/v4-11.md.
```
