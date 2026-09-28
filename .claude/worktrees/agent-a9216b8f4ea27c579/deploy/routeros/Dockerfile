# HikRAD RouterOS container image (v4-2, FR-120, Decision 58).
#
# This is NOT the Linux install path — Docker is cancelled as HikRAD's
# delivery mechanism everywhere else (Decision 57, deploy/systemd/ +
# scripts/install.sh). This one image exists ONLY because RouterOS 7.23+'s
# `/container` feature is the sole way to run third-party software on a
# MikroTik router, and that feature consumes an OCI image, not a Docker
# daemon — CI builds exactly this one image for that one device target and
# publishes it as an importable `.tar` (deploy/routeros/build.sh).
#
# All-in-one: PostgreSQL + TimescaleDB, Redis, FreeRADIUS, and all three
# HikRAD Go services in a single image, supervised by entrypoint.sh (RouterOS
# runs one top-level container process). See README.md for the supported
# ceiling — this target is for SMALL deployments, not server parity.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.* ./
RUN go mod download
COPY backend/ ./
ARG HIKRAD_VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/hikrad/hikrad/internal/platform/setupapi.appVersion=${HIKRAD_VERSION}" \
    -o /out/hikrad-api ./cmd/hikrad-api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hikrad-acct ./cmd/hikrad-acct \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hikrad-monitor ./cmd/hikrad-monitor

FROM node:22-alpine AS frontend
WORKDIR /src
COPY frontend/ ./frontend/
RUN cd frontend && npm ci && npm run build --workspaces --if-present

FROM alpine:3.20
RUN apk add --no-cache \
        postgresql16 postgresql16-contrib timescaledb \
        redis \
        freeradius freeradius-perl freeradius-utils \
        nginx \
        perl perl-http-tiny perl-json \
        openssl bash tzdata ca-certificates iputils \
    && adduser -D -H -u 10001 hikrad-api \
    && adduser -D -H -u 10002 hikrad-acct \
    && adduser -D -H -u 10003 hikrad-monitor

COPY --from=build /out/hikrad-api /out/hikrad-acct /out/hikrad-monitor /opt/hikrad/bin/
COPY --from=frontend /src/frontend/panel/dist /opt/hikrad/www/panel
COPY --from=frontend /src/frontend/portal/dist /opt/hikrad/www/portal
COPY backend/migrations /opt/hikrad/migrations
COPY deploy/freeradius /etc/hikrad/freeradius
COPY deploy/nginx/hikrad.conf.template /etc/hikrad/nginx.conf.template
COPY scripts/gen-env.sh /opt/hikrad/gen-env.sh
COPY deploy/routeros/entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh /opt/hikrad/bin/* /opt/hikrad/gen-env.sh

# PGDATA lives on the operator-mounted ext4 disk (README.md's "format and
# mount an ext4 disk" step) — RouterOS containers persist only what's on a
# mounted volume; anything else is lost on container recreation/upgrade,
# which is exactly why "removing the container app destroys the database"
# is the mandatory, twice-stated warning in README.md.
ENV PGDATA=/hikrad-data/postgres
ENV HIKRAD_DATA_DIR=/hikrad-data

EXPOSE 80 1812/udp 1813/udp 3799/udp

ENTRYPOINT ["/entrypoint.sh"]
