# Admin guide

> **Status: skeleton (Phase 5).** Covers the areas this phase's agent (A,
> Platform & Security) owns: license, backups/updates, settings, and the
> optional Cloudflare tunnel. Panel screens for subscribers/billing/
> monitoring are documented by their owning agents as those UIs land; this
> file is the single admin-guide doc every agent's section gets added to,
> not a per-agent fork.

## Contents

- [License](#license)
- [Backups](#backups)
- [Updates](#updates)
- [Settings](#settings)
- [Remote access: the optional Cloudflare tunnel](#remote-access-the-optional-cloudflare-tunnel)

## License

HikRAD is licensed per server, validated **entirely offline** — nothing
about licensing ever phones home (NFR-7, FR-50).

**How it works:** your server has a *fingerprint* (a hash of its machine-id
and primary MAC), shown in the first-run wizard and at Settings > System
(`GET /api/v1/license`). You send that fingerprint to HikRAD by whatever
channel you already use (email, ticket, USB) and get back a signed license
file for it, which you upload in the same place.

**States**, always visible in the panel header once a license is installed:
- **Valid** — fingerprint matches. Full functionality.
- **Grace** (14 days) — the fingerprint stopped matching (you cloned/moved
  the VM, or restored a backup onto different hardware). Everything still
  works; a banner appears and an in-app alert fires. Upload a re-issued key
  for the new fingerprint to clear it.
- **Expired grace** — 14 days passed with no re-issued key. The panel
  becomes **read-only** (mutations return `403 license_expired`), but **RADIUS
  authentication and accounting keep running without interruption** —
  subscribers are never cut off over a licensing lapse (FR-50.3). Upload a
  valid key for the current fingerprint to restore full access instantly.

**Common hardware-change cases**, so you know what to expect:
- Restoring a backup onto the *same* server → no change, stays valid.
- A VM clone that gets a new virtual MAC but keeps the same disk → still
  tolerated as valid (one of the two fingerprint components changed, not
  both) — no grace banner.
- A genuine move to different hardware → both components change → grace,
  as designed. Request a re-issue for the new fingerprint.

**Re-issue request:** Settings > System > License > "Request re-issue blob"
(or `POST /api/v1/license/request-blob`) produces your current key id + new
fingerprint as a small JSON blob to send the vendor.

## Backups

See [backup-restore.md](backup-restore.md) for the full runbook. Summary:
nightly automatic backups (cron, installed by `install.sh`), passphrase-
encrypted, retained per `HIKRAD_BACKUP_RETENTION` (default 14). Settings >
System shows the age of the last backup; a stale backup is alertable.

## Updates

See [update.md](update.md). Summary: `hikrad update` takes a pre-update
backup automatically, rebuilds/pulls images, runs forward-only database
migrations, and rolls back the images if the new version doesn't come up
healthy.

## Settings

`Settings` in the panel maps directly onto `GET`/`PUT /api/v1/settings/{group}`
(admin-permission-gated: `settings.view`/`settings.edit`). Groups (FR-53.2):

| Group | Fields | Notes |
|---|---|---|
| `locale` | `timezone`, `currency`, `date_format`, `language` | Default Asia/Baghdad / IQD |
| `branding` | `name`, `logo_url`, `primary_color`, `secondary_color` | Consumed by panel + portal |
| `notifications` | `smtp`, `telegram`, `whatsapp` | Each a credentials object; use the "send test" button (or `POST /api/v1/settings/notifications/test`) to verify before relying on alerts |
| `billing` | `renewal_anchor`, `admin_balance_bypass`, `receipt_prefix`, `receipt_branding`, `voucher_prefix`, `receipt_numerals` | Read directly by `internal/billing` |
| `backups` | `schedule_hour`, `retention_count`, `path` | The cron entry and `HIKRAD_BACKUP_*` env vars are the enforced source of truth today; these fields are informational pending a cron-rewrite endpoint |
| `data_retention` | `raw_months` (≥ 12), `rollup_years` (≥ 3) | Floors enforced server-side (FR-33) — a lower value is rejected with `422`, not silently clamped |
| `remote_access` | `enabled`, `token` | See tunnel section below; `token` is write-only (encrypted at rest, GET returns only `token_set: true/false`) |

## Remote access: the optional Cloudflare tunnel

Off by default. Every daily operation — panel, portal, RADIUS, CoA — works
identically on the LAN whether this is configured or not, and whether the
internet is reachable or not (NFR-7, FR-57). Turn it on only if you want to
reach the panel/portal from outside your network without opening firewall
ports.

**What it does and doesn't expose:** the tunnel fronts Caddy only — the
panel (`/`), portal (`/portal`), and API (`/api/*`). RADIUS (UDP 1812/1813)
and CoA (UDP 3799) are never reachable through it; there is no path in the
tunnel container's config that could route to those ports even by mistake.

**Setup (in the Cloudflare dashboard, one time):**
1. Log into the Cloudflare Zero Trust dashboard for your account.
2. Networks > Tunnels > Create a tunnel (Cloudflared connector type).
3. Name it (e.g. `hikrad-<your-isp>`), copy the **tunnel token** it gives
   you.
4. Public Hostname: add a hostname (e.g. `panel.your-isp.example`) and point
   it at `http://caddy:80` (the tunnel runs inside HikRAD's own Docker
   network, so it reaches Caddy by its service name — do not use `localhost`
   or an external IP here).
5. **Strongly recommended:** Access > Applications > Add an application for
   that hostname, and require your team's SSO/email OTP before the tunnel
   will even proxy a request through to Caddy. The tunnel gets you past NAT;
   it is not itself an authentication layer, and the panel's own login is
   your only defense if you skip this step.

**Enable it on the server:**
```sh
# paste the token from step 3 into Settings > Remote Access > Token, enable it, save
# then:
hikrad tunnel enable
```
This starts only the `cloudflared` container (Compose profile `tunnel`,
never started by a plain `hikrad up`). `hikrad tunnel disable` stops only
that container — nothing else restarts.

**Verify:** the health page (`GET /api/v1/health`) reports the tunnel state
(disabled/connected/disconnected), and it's alertable the same way NAS
down-state is. Confirm the negative guarantee yourself once: from outside
your LAN, `nc -zv -u <your-cloudflare-hostname> 1812` (or any RADIUS client
pointed at the tunnel hostname) should simply fail to connect — only Caddy's
ports are ever reachable through the tunnel.
