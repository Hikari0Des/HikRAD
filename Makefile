# HikRAD developer entrypoints (Phase 1, Agent A — contract: these target names
# are frozen; CI and other agents call them).

COMPOSE      := docker compose --env-file deploy/.env -f deploy/compose.yml
MIGRATE_IMG  := migrate/migrate:v4.17.1
MODE         ?= smoke

.PHONY: help up down seed test migrate lint env evidence

help:
	@echo "make up       - generate deploy/.env if missing, build and start the stack"
	@echo "make down     - stop the stack (data under deploy/data/ is kept)"
	@echo "make seed     - load demo data (admin manager, testuser subscriber, one profile)"
	@echo "make test     - backend unit tests + script self-tests"
	@echo "make migrate  - apply pending DB migrations manually (they also run on api boot)"
	@echo "make lint     - go vet + frontend lint"
	@echo "make evidence - run the chaos/perf/sizing evidence pack (Phase 5, contract C6);"
	@echo "                MODE=full for the 5k/2k reference scale (default: smoke)"

deploy/.env:
	./scripts/gen-env.sh deploy/.env

env: deploy/.env

up: deploy/.env
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

# Convention exposed to Agent D: the hikrad-api binary accepts a `seed`
# subcommand that loads the Phase-1 demo data (idempotent).
seed:
	$(COMPOSE) exec hikrad-api hikrad-api seed

# -p 1 (backend go test, below): HIKRAD_TEST_DB_URL/HIKRAD_TEST_REDIS_URL point
# every package at ONE shared external Postgres/Redis; whole-table aggregate
# reconciliation tests (e.g. internal/reports' revenue property test) sum
# across the whole payments table and will intermittently pick up rows another
# package's test committed in the same wall-clock window if packages run
# concurrently (found live in the Phase-5 gate run — passes reliably under
# -p 1, flakes without it). Per-test rows are already uniq()-prefixed for
# row-level isolation; -p 1 only serializes package execution.
test:
	@if [ -f backend/go.mod ]; then \
		echo "== backend: go test =="; \
		cd backend && go test -p 1 ./...; \
	else \
		echo "backend/go.mod not present yet (Agent D) — skipping Go tests"; \
	fi
	@echo "== scripts: gen-env self-test =="
	./scripts/gen-env.test.sh

# Manual migration run against the compose stack's postgres. Migrations are
# forward-only (FR-51.4); hikrad-api also applies them automatically on boot.
migrate: deploy/.env
	docker run --rm --network hikrad_default \
		-v "$(CURDIR)/backend/migrations:/migrations:ro" \
		$(MIGRATE_IMG) \
		-path=/migrations \
		-database "$$(grep '^HIKRAD_DB_URL=' deploy/.env | cut -d= -f2-)" \
		up

# Phase 5 (Agent 2) — chaos/perf/sizing evidence pack (contract C6). Requires
# Docker (self-provisions throwaway Postgres/Redis); MODE=full for the 5k/2k
# reference scale. See docs/evidence/README.md.
evidence:
	MODE=$(MODE) sh docs/evidence/generate.sh

lint:
	@if [ -f backend/go.mod ]; then \
		echo "== backend: go vet =="; \
		cd backend && go vet ./...; \
	else \
		echo "backend/go.mod not present yet (Agent D) — skipping go vet"; \
	fi
	@if [ -f frontend/package.json ]; then \
		echo "== frontend: lint =="; \
		cd frontend && npm run lint --workspaces --if-present; \
	else \
		echo "frontend/package.json not present yet (Agents E/F) — skipping frontend lint"; \
	fi
