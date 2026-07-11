# hikrad-monitor image (Phase 3, Agent 3). Build context is the REPO ROOT:
#   docker build -f deploy/docker/monitor.Dockerfile .
# (deploy/compose.yml does this via build.context: ..). Mirrors acct.Dockerfile;
# ships only the monitoring binary. No migrations are baked in — hikrad-api owns
# the schema and applies them on its boot.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hikrad-monitor ./cmd/hikrad-monitor

FROM alpine:3.20
# iputils gives a ping that honours CAP_NET_RAW so reachability probes work
# without root; tzdata backs the Asia/Baghdad quiet-hours math.
RUN apk add --no-cache ca-certificates tzdata iputils \
    && adduser -D -H -u 10003 hikrad
COPY --from=build /out/hikrad-monitor /usr/local/bin/hikrad-monitor
USER hikrad
ENTRYPOINT ["hikrad-monitor"]
