# HikRAD evidence pack (Phase 5, Agent 2 — contract C6)

This directory is the scripted, reproducible proof behind HikRAD's flagship
claims: **zero lost accounting records** (M2, FR-37.5/NFR-2) and the NFR-1
performance/NFR-3 sizing budgets. It is shipped with the v1 release and
re-run at the pilot on real hardware.

## Running it

```sh
make evidence                # smoke mode: ~5-10 min, self-provisioning
MODE=full make evidence      # full 5k/2k reference scale: several hours
```

or directly: `sh docs/evidence/generate.sh`. Output: a dated report at
`docs/evidence/reports/<date>-evidence.md` plus the raw JSON per scenario/tool
under `docs/evidence/raw/` (regenerated, not committed — see `.gitignore`
note below).

## What's actually proven vs. what needs the pilot rerun

Everything under `backend/test/chaos/` and `backend/test/perf/{ingest,sizing}`
is **self-contained**: it builds a real `hikrad-acct` binary and stands up
throwaway Postgres/Redis containers with published host ports (no
bind-mounted config trees), so it runs identically on a dev laptop, in CI,
and on the pilot's Linux host. This suite was actually executed in this
sandbox — see the smoke-mode report checked in under `reports/` and
`status-agent-2.md` for the numbers.

`backend/test/perf/{authload,sse,panelapi}` need a full running
FreeRADIUS + hikrad-api + panel stack (`deploy/compose.yml`) plus a manager
bearer token. They were **not** exercised live in this Phase-5 sandbox run:
this dev machine's Docker Desktop + WSL2 integration has a documented,
independent failure mode with `deploy/freeradius/`'s and Caddy's
bind-mounted config trees when the repo is bind-mounted from a raw Windows
path (see memory `hikrad-dev-environment` / Phase-4 Agent-1's own status
note for the same limitation) — unrelated to anything this suite tests, and
already flagged as Phase-1/Agent-A territory (`scripts/install.sh`,
`deploy/compose.yml`). The tools are complete and were built+vet-clean; run
them for real via:

```sh
HIKRAD_EVIDENCE_STACK_UP=1 HIKRAD_EVIDENCE_TOKEN=<manager bearer token> \
  MODE=full make evidence
```

against a compose stack brought up on Linux (the pilot host, or CI's
`harness-smoke`-style runner), which `generate.sh` wires up automatically
when those env vars are set.

## Layout

- `generate.sh` — the `make evidence` entrypoint (orchestrates chaos → perf
  → sizing → render).
- `reports/` — dated Markdown reports, one per run. Committed for the runs
  that ship with a release.
- `raw/` — JSON output per scenario/tool, regenerated every run (gitignored;
  the report embeds what matters).
- `redis-durability-decision.md` — the sub-PRD 03 §7 open question ("does
  the ack path need the disk WAL in front of Redis by default"), closed with
  measured data from the `redis-durability` chaos scenario.

## Sizing math

See `backend/internal/accounting/README.md`'s "Storage sizing" section for
the original back-of-envelope math; `test/perf/sizing` replaces it with a
measurement-backed number (real bytes/row, real compression ratio) each run.
