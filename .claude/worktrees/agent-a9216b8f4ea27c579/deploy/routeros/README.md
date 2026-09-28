# HikRAD on RouterOS (container target)

**v4-2, FR-120, Decision 58.** This is a completely separate artifact from
the Linux native install (`scripts/install.sh`) — it exists only because
RouterOS 7.23+'s `/container` feature is the *only* way to run third-party
software on a MikroTik router, and that feature consumes an OCI image, not
a Docker daemon. Nothing here reinstates Docker as a delivery mechanism:
one image is built in CI for this one device target, imported directly on
the router, and no Compose file or Docker daemon is ever involved on the
router or on any Linux server.

> **HARDWARE LEG, NOT YET VERIFIED (gate item 13).** This procedure has not
> been run against a real RouterOS 7.23+ device as of this commit — see
> `docs/history/gates/v4-2.md`. Rehearse it yourself on real hardware
> before relying on it for a customer.

## What this target is for — read this before you deploy it

**This is for small deployments, not server parity.** A router's CPU, RAM,
and flash/disk I/O are a small fraction of the NFR-3 server tier (4 vCPU /
8 GB / 200 GB) this product is otherwise sized for. Running PostgreSQL,
Redis, FreeRADIUS, and three Go services on a router's own storage is
**recommended only up to roughly 200 concurrent subscriber sessions** on
mid-range RouterOS hardware (RB5009-class or better, with a real SSD/NVMe
mounted for container storage — a microSD or slow USB stick will bottleneck
PostgreSQL badly). Above that, or for anything customer-facing at scale,
install natively on a real server instead (`scripts/install.sh`).

## ⚠️ Removing the container app destroys the database — read this twice

Everything this install has — every subscriber, every ledger entry, every
session — lives **only** on the ext4 disk you mount into the container at
`/hikrad-data`. RouterOS's container feature keeps no state anywhere else.
**`/container remove` (or replacing the container without its data disk
still attached) destroys the database with no confirmation prompt beyond
the command itself.** Back up before every upgrade:

```
# from a machine that can reach the router's panel:
curl -k -u admin:<password> https://<router-ip>/api/v1/backups/latest/download -o backup.tar.gz.gpg
```

or use `hikrad backup now` (see "Backups from the on-router install" below,
gate item 14) — either way, get the archive **off the router**, onto a
machine that isn't the one you're about to reboot/reformat. **This is the
single most consequential fact about this target — read it twice.**

## Procedure

### 1. Enable container mode (requires physical access)

```
/system/device-mode/update container=yes
```

RouterOS refuses this change until you confirm it with a **hard reboot** —
either press the physical reset button per the console's prompt, or, on
x86 RouterOS, a full power cycle. **A soft `/system reboot` is not
sufficient**; this is a deliberate RouterOS security measure for any
device-mode change, not a HikRAD requirement.

### 2. Format and mount an ext4 disk

Containers need real ext4 storage — most onboard flash is unsuitable and
too small. Attach a USB drive, NVMe, or SATA disk rated for the traffic
level in "What this target is for" above, then, in Winbox/CLI:

```
/disk format-drive disk=[find slot=1] file-system=ext4
```

(Exact syntax varies slightly by RouterOS build/device — see
[MikroTik's disk documentation](https://help.mikrotik.com/docs/spaces/ROS/pages/8978473/Disk)
for your device. RouterOS mounts a formatted container-capable disk
automatically, typically surfacing as `disk1`.)

### 3. Networking: a veth interface for the container

```
/interface/veth/add name=veth1 address=172.17.0.2/24 gateway=172.17.0.1
/interface/bridge/add name=hikrad-br
/ip/address/add address=172.17.0.1/24 interface=hikrad-br
/interface/bridge/port/add bridge=hikrad-br interface=veth1
```

### 4. Import and configure the image

Copy the built `.tar` (`deploy/routeros/build.sh`, or the one attached to
the signed release) onto the router's `disk1` (drag-and-drop in
Winbox/WebFig's Files list, or `scp`), then:

```
/container/config/set tmpdir=disk1/hikrad-tmp
/container/mounts/add list=MOUNT_HIKRAD src=disk1/hikrad-data dst=/hikrad-data
/container/add file=disk1/hikrad-routeros-vX.Y.Z-<arch>.tar interface=veth1 \
    root-dir=disk1/hikrad mountlists=MOUNT_HIKRAD name=hikrad
```

`<arch>` must match the router's own architecture — `arm64` for ARM64
devices (most current RB5009/CCR2xxx-class hardware), `amd64` for x86
RouterOS. `deploy/routeros/build.sh` takes the target as its third
argument.

### 5. Start it

```
/container/start hikrad
```

First boot initializes PostgreSQL and generates secrets on the mounted
disk (mirrors `scripts/install.sh`'s own first-run behavior) — this can
take a minute or two. Watch progress:

```
/container/print detail
/log print where topics~"container"
```

### 6. Point the router's own RADIUS client at the container

The container's veth address (`172.17.0.2` in the example above) *is* this
router's own RADIUS server — from the router's perspective, this is as
local as RADIUS gets without literally being the router's own process:

```
/radius add address=172.17.0.2 secret=<the NAS secret you set in the panel> \
    service=ppp,hotspot authentication-port=1812 accounting-port=1813
/radius incoming set accept=yes port=3799
```

(This is the same RADIUS-client config the panel's NAS config-snippet
generates for any router — see `docs/prd/02-radius-nas-aaa.md` — pointed
at the container's own address instead of a separate server.)

### 7. Open the panel

`http://172.17.0.2/` from a device that can reach the container's bridge
(or NAT/firewall-forward it as you would any other on-router service).
The RouterOS target serves **HTTP only** on the container's own address —
TLS termination for remote access is the operator's own choice (a
WireGuard tunnel to the panel, or the router's own HTTPS proxy in front of
it), not something this all-in-one image does for you, given the
constrained target.

## Backups from the on-router install (gate item 14)

`hikrad backup now` works exactly as it does on a native Linux install —
run it inside the container:

```
/container/shell hikrad
hikrad backup now
```

produces a downloadable file under `/hikrad-data/backups/` inside the
container's own filesystem view, which maps to `disk1/hikrad-data/backups`
on the router's own storage — retrievable via the Files list, `scp`, or the
panel's own backup-download endpoint. **Get it off the router after every
backup** — a disk failure on the router is a total loss otherwise.

## Upgrading

1. Take a backup (above) and copy it off the router.
2. Build/obtain the new version's `.tar`.
3. `/container/stop hikrad`
4. `/container/remove hikrad` — **the mounted disk (`/hikrad-data`) is
   untouched by this**, only the container definition itself is removed;
   do not skip step 1 anyway, since a mistake here is exactly the
   destructive scenario this whole section warns about.
5. Re-run step 4 of "Procedure" above with the new tar, then step 5 to
   start it — it finds the existing data on `/hikrad-data` and starts
   normally, applying forward-only migrations the same way a Linux
   install's `hikrad update` does.
