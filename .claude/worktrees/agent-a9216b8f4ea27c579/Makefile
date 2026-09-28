# HikRAD developer entrypoints (v4-2, FR-118: reworked off Docker Compose —
# the native dev loop assumes a local PostgreSQL 16 + TimescaleDB and Redis,
# same as production minus systemd; see docs/ops/dev-setup.md for how to get
# those running, including inside WSL2 on Windows).

MODE ?= smoke

.PHONY: help dev-env dev-up dev-down seed test lint evidence

help:
	@echo "make dev-env  - generate deploy/hikrad.env against your local Postgres/Redis (if missing)"
	@echo "make dev-up   - run hikrad-api/hikrad-acct/hikrad-monitor via 'go run' (foreground; Ctrl+C stops all)"
	@echo "make dev-down - reset the dev database to empty (drops + recreates it; see docs/ops/dev-setup.md)"
	@echo "make seed     - load demo data (admin manager, testuser subscriber, one profile)"
	@echo "make test     - backend unit tests + script self-tests"
	@echo "make lint     - go vet + frontend lint"
	@echo "make evidence - run the chaos/perf/sizing evidence pack; MODE=full for the 5k/2k reference scale"

deploy/hikrad.env:
	./scripts/gen-env.sh deploy/hikrad.env

dev-env: deploy/hikrad.env

dev-up: deploy/hikrad.env
	./scripts/dev-run.sh

dev-down:
	./scripts/dev-run.sh --reset-db-only

seed: deploy/hikrad.env
	set -a && . ./deploy/hikrad.env && set +a && cd backend && go run ./cmd/hikrad-api seed

# -p 1: every package shares one real Postgres/Redis here; whole-table
# aggregate reconciliation tests need packages serialized to stay
# deterministic (see backend/Makefile's own note on this).
test:
	@if [ -f backend/go.mod ]; then \
		echo "== backend: go test =="; \
		cd backend && go test -p 1 ./...; \
	else \
		echo "backend/go.mod not present yet — skipping Go tests"; \
	fi
	@echo "== scripts: gen-env self-test =="
	./scripts/gen-env.test.sh

evidence:
	MODE=$(MODE) sh docs/evidence/generate.sh

lint:
	@if [ -f backend/go.mod ]; then \
		echo "== backend: go vet =="; \
		cd backend && go vet ./...; \
	else \
		echo "backend/go.mod not present yet — skipping go vet"; \
	fi
	@if [ -f frontend/package.json ]; then \
		echo "== frontend: lint =="; \
		cd frontend && npm run lint --workspaces --if-present; \
	else \
		echo "frontend/package.json not present yet — skipping frontend lint"; \
	fi
