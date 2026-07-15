# caddy image (Phase 5 fix). Build context is the REPO ROOT:
#   docker build -f deploy/docker/caddy.Dockerfile .
# (deploy/compose.yml does this via build.context: ..).
#
# Bakes the panel + portal SPAs into the image so Docker stays the only host
# dependency for a fresh install (matching hikrad-api/hikrad-acct's own
# multi-stage builds). Previously deploy/compose.yml bind-mounted
# frontend/{panel,portal}/dist straight from the host, but dist/ is
# gitignored and nothing ever built it — a genuine `git clone` + install.sh
# (exactly what docs/ops/install-guide.md documents) 404'd on the panel with
# no dist to serve. Found live in the Phase-5 M4 gate rehearsal.
FROM node:22-alpine AS build
WORKDIR /src
COPY frontend/ ./frontend/
RUN cd frontend && npm ci && npm run build --workspaces --if-present

FROM caddy:2-alpine
COPY --from=build /src/frontend/panel/dist /srv/panel
COPY --from=build /src/frontend/portal/dist /srv/portal
