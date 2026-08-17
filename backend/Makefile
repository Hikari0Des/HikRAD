# HikRAD backend-local entrypoint — the packet-harness smoke target, invoked
# as `make -C backend test-harness-smoke` from two places: docs/ops/
# dev-setup.md (gate step 3) and .github/workflows/ci.yml's harness-smoke
# job. v4-2 (FR-118/119) reworked this off `docker compose` — see
# test/harness/run-smoke.sh for what it actually does: it expects a
# reachable PostgreSQL 16 + TimescaleDB and Redis (local dev: already
# running per docs/ops/dev-setup.md; CI: GitHub Actions service containers)
# and a `freeradius` binary on PATH, and manages its own hikrad-api/
# FreeRADIUS processes for its own lifetime.

.PHONY: test-harness-smoke

test-harness-smoke:
	bash ./test/harness/run-smoke.sh
