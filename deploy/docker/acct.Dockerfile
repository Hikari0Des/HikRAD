# hikrad-acct image (Phase 2, Agent 3). Build context is the REPO ROOT:
#   docker build -f deploy/docker/acct.Dockerfile .
# (deploy/compose.yml does this via build.context: ..). Mirrors api.Dockerfile;
# ships only the accounting ingest binary. No migrations are baked in — hikrad-api
# owns the schema and applies them on its boot.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hikrad-acct ./cmd/hikrad-acct

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10002 hikrad \
    && mkdir -p /spill && chown hikrad /spill
COPY --from=build /out/hikrad-acct /usr/local/bin/hikrad-acct
USER hikrad
EXPOSE 8082
ENTRYPOINT ["hikrad-acct"]
