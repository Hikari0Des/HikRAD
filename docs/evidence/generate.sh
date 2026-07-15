#!/bin/sh
# docs/evidence/generate.sh — Phase 5 contract C6 entrypoint (`make evidence`).
# Runs the chaos suite + perf/sizing tools and renders a dated Markdown
# report under docs/evidence/reports/. This is the artifact shipped with the
# v1 release and re-run at the pilot (M1/M2 proof).
#
# Modes (MODE env var, default smoke):
#   smoke — reduced scale (~200 sessions, short windows), ~5-10 minutes.
#           Safe for CI-nightly; self-provisions throwaway Postgres/Redis
#           containers via Docker, so it needs Docker but NOT a full
#           deploy/compose.yml stack.
#   full  — 5k/2k reference scale (2000 sessions, 50 pkt/s, multi-minute
#           chaos windows, 12-month sizing). Documented runtime: several
#           hours. Intended for the pilot / a pre-release run on real
#           hardware, not routine CI.
#
# authload/sse/panelapi (the RADIUS-auth-path and panel-API legs of NFR-1)
# need a full running FreeRADIUS+hikrad-api+panel stack with a manager
# token; this script runs them only if HIKRAD_EVIDENCE_STACK_UP=1 and the
# relevant addresses/token are provided (see flags below) — otherwise it
# skips them and says so plainly in the report, rather than silently
# omitting NFR-1 numbers. See README.md for why they were not exercised in
# the Phase-5 sandbox run.
#
# Deliberately no `set -e`: a chaos scenario failing is DATA the report must
# still show, not a reason to abort before perf/sizing/render ever run. Each
# step's own exit status is captured into FAILED and the script's own exit
# code reflects the OR of everything at the very end.
set -u

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT/backend"

MODE=${MODE:-smoke}
mkdir -p "$ROOT/docs/evidence/raw"
rm -f "$ROOT/docs/evidence/raw"/*.json

if [ "$MODE" = "full" ]; then
  SESSIONS=2000; RATE=50; DURATION=5m; KILLFOR=10m
  SIZING_MONTHS=12; SIZING_SESSIONS=2000
  SUSTAINED_FOR=5m; BURST_FOR=1m
else
  SESSIONS=200; RATE=50; DURATION=20s; KILLFOR=15s
  SIZING_MONTHS=1; SIZING_SESSIONS=200
  SUSTAINED_FOR=30s; BURST_FOR=15s
fi

echo "== Phase 5 evidence pack (mode=$MODE) =="

FAILED=0

echo "-- chaos suite --"
go run ./test/chaos -scenario all -sessions "$SESSIONS" -rate "$RATE" -duration "$DURATION" -kill-for "$KILLFOR" -out "$ROOT/docs/evidence/raw" || FAILED=1

echo "-- ingest perf --"
go run ./test/perf/ingest -sessions "$SESSIONS" -sustained-for "$SUSTAINED_FOR" -burst-for "$BURST_FOR" -out "$ROOT/docs/evidence/raw/ingest-perf.json" || FAILED=1

echo "-- sizing (NFR-3) --"
go run ./test/perf/sizing -months "$SIZING_MONTHS" -sessions "$SIZING_SESSIONS" -out "$ROOT/docs/evidence/raw/sizing.json" || FAILED=1

if [ "${HIKRAD_EVIDENCE_STACK_UP:-0}" = "1" ]; then
  echo "-- authload/sse/panelapi (live stack) --"
  go run ./test/perf/authload -addr "${HIKRAD_EVIDENCE_RADIUS_ADDR:-127.0.0.1:1812}" -rate 50 -duration 1m \
    -out "$ROOT/docs/evidence/raw/authload-perf.json" || FAILED=1
  if [ -n "${HIKRAD_EVIDENCE_TOKEN:-}" ]; then
    go run ./test/perf/sse -token "$HIKRAD_EVIDENCE_TOKEN" -samples 20 \
      -out "$ROOT/docs/evidence/raw/sse-perf.json" || FAILED=1
    go run ./test/perf/panelapi -token "$HIKRAD_EVIDENCE_TOKEN" -requests 30 \
      -out "$ROOT/docs/evidence/raw/panelapi-perf.json" || FAILED=1
  fi
else
  echo "-- authload/sse/panelapi SKIPPED (set HIKRAD_EVIDENCE_STACK_UP=1 + HIKRAD_EVIDENCE_TOKEN against a running compose stack to include them) --"
fi

echo "-- rendering report --"
go run ./test/perf/evidence -raw "$ROOT/docs/evidence/raw" -out "$ROOT/docs/evidence/reports" -mode "$MODE" || FAILED=1

exit "$FAILED"
