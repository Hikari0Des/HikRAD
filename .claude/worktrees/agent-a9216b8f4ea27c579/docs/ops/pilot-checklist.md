# Pilot go-live checklist

Run this against the actual server the pilot ISP will use, not a dev box.
Every item should be checked by actually doing the thing, not by reading the
code that claims to do it. Check items link to the doc/gate that defines
"done" for that item.

## Install (M4)

- [ ] Fresh Ubuntu 22.04/24.04 server meets or exceeds NFR-3 (4 vCPU / 8 GB /
      200 GB), or the shortfall is an accepted trade-off for this pilot's
      scale.
- [ ] `sudo ./scripts/install.sh` (optionally `--domain ...`) completes
      without error, following [install-guide.md](install-guide.md) with no
      outside help.
- [ ] First-run wizard completed: license (or explicitly deferred), admin
      account, branding, first NAS, first profile.
- [ ] A real PPPoE Access-Accept against the pilot's actual NAS hardware —
      not just the packet harness.
- [ ] Total time from `install.sh` start to that Access-Accept is under 30
      minutes. Record the actual number.
- [ ] Install summary's backup passphrase has been written down somewhere
      other than this server (password manager, printed copy in a safe,
      etc.) — verify with the installer, don't take it on faith.

## License

- [ ] If a paid license exists for this install: uploaded, state shows
      `valid`, fingerprint matches.
- [ ] If not yet licensed: confirmed the system is fully functional
      unlicensed and the fingerprint-request flow is understood
      ([admin-guide.md#license](admin-guide.md#license)).
- [ ] Grace/expired-grace behavior explained to the ISP's admin so a future
      hardware migration doesn't cause a support panic.

## Backups & recovery

- [ ] `hikrad backup now` succeeds; `hikrad backup list` shows it.
- [ ] Nightly cron entry is installed (`crontab -l` as root shows the
      `hikrad backup now` line) and fires overnight — check tomorrow, don't
      just trust the entry exists.
- [ ] A full `hikrad restore <archive>` has been rehearsed **on a second
      VM**, not the production server, per
      [backup-restore.md](backup-restore.md). Verification summary
      (subscriber count, last ledger entry, last accounting record) matches
      expectations and RADIUS auth works immediately after.
- [ ] Backup passphrase loss scenario is understood by the ISP's admin
      (unrecoverable by design — no vendor escrow).

## Updates

- [ ] `hikrad update` has been rehearsed at least once (even a no-op update
      against the same version) so the operator has seen the pre-backup +
      health-check + rollback flow before they need it under pressure.

## Security (ASVS L2)

- [ ] [security-checklist.md](security-checklist.md) fully checked (☑ every
      row) on the release build actually being deployed, not an older
      commit.
- [ ] TLS is real for this install: either a trusted Let's Encrypt cert
      (`--domain` was used and the browser shows no warning) or the ISP's
      admin has explicitly accepted the self-signed default and understands
      the warning it produces for every visitor.
- [ ] Default admin password has been changed from whatever was used during
      the wizard rehearsal, if the wizard was run more than once.

## Evidence pack (M2, NFR-1 — owned by Agent C)

- [ ] `docs/evidence/` chaos suite results attached and green for this
      release build.
- [ ] NFR-1 perf numbers (auth p99, ingest depth, SSE latency) attached and
      within budget for the pilot's expected subscriber count.

## Localization & UX

- [ ] `npm run i18n:check` is clean (zero untranslated strings) — CI-fatal,
      but confirm it actually ran against this exact commit.
- [ ] Arabic and Kurdish render correctly RTL in the panel and portal on a
      real browser, not just visually skimmed in a screenshot.
- [ ] Renew, reset-MAC, and find-subscriber flows are each ≤ 3 clicks from
      the panel's landing screen (Sara's persona budget) — click through
      them, don't estimate.

## Remote access (only if the pilot wants it)

- [ ] Cloudflare tunnel enabled per
      [admin-guide.md](admin-guide.md#remote-access-the-optional-cloudflare-tunnel),
      an Access policy is in front of the panel hostname, and a RADIUS/CoA
      port probe against the tunnel hostname fails to connect (negative
      check, don't skip it).
- [ ] Confirmed the LAN keeps working with the tunnel disabled / internet
      down — pull the network cable or `hikrad tunnel disable` and verify a
      subscriber can still authenticate and the local panel still loads.

## Handoff

- [ ] The ISP's admin has read (not just received) `admin-guide.md`.
- [ ] Support contact / escalation path for this pilot is documented
      somewhere the ISP can find it without asking you.
