# HikRAD backend-local entrypoint (Phase 1, Agent 2 — RADIUS & NAS).
#
# The repo-root Makefile (Agent 1) owns up/down/seed/test/migrate/lint; this
# file exists only for the packet-harness smoke target, invoked as
# `make -C backend test-harness-smoke` from two places: docs/ops/dev-setup.md
# (gate step 3, after the root `make up` + `make seed` already ran) and
# .github/workflows/ci.yml's harness-smoke job (which brings up nothing
# beforehand). Idempotent so both callers get the same result: `up -d --wait`
# no-ops against services that are already healthy, and seeding
# (backend/internal/seed) is safe to rerun.

COMPOSE := docker compose --env-file ../deploy/.env -f ../deploy/compose.yml

.PHONY: test-harness-smoke

test-harness-smoke:
	@test -f ../deploy/.env || ../scripts/gen-env.sh ../deploy/.env
	$(COMPOSE) up -d --build --wait postgres redis hikrad-api freeradius
	$(COMPOSE) exec -T hikrad-api hikrad-api seed
	go run ./test/harness -addr 127.0.0.1:1812 -secret testing123
