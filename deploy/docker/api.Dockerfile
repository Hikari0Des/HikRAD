# hikrad-api image (Phase 1, Agent A). Build context is the REPO ROOT:
#   docker build -f deploy/docker/api.Dockerfile .
# (deploy/compose.yml does this via build.context: ..)

FROM golang:1.24-alpine AS build
WORKDIR /src
# go.mod/go.sum are owned by Agent D; the glob keeps this buildable the
# moment they land without further edits here.
COPY backend/go.* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hikrad-api ./cmd/hikrad-api
# Compose healthcheck probe (stdlib-only, own throwaway module — it is not
# part of backend/go.mod, which Agent D owns).
COPY deploy/docker/healthcheck.go /healthcheck/main.go
RUN cd /healthcheck && go mod init healthcheck && CGO_ENABLED=0 go build -trimpath -o /out/hikrad-healthcheck .

FROM alpine:3.20
# tzdata: Asia/Baghdad rendering (FR-53 locale defaults); ca-certificates for
# outbound SMTP/Telegram in later phases.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 hikrad
COPY --from=build /out/hikrad-api /usr/local/bin/hikrad-api
COPY --from=build /out/hikrad-healthcheck /usr/local/bin/hikrad-healthcheck
# Migrations ship inside the image so the boot-time runner (backend/internal/
# platform) works offline with no bind mounts (NFR-7).
COPY backend/migrations /app/migrations
ENV HIKRAD_MIGRATIONS_DIR=/app/migrations
USER hikrad
EXPOSE 8080
ENTRYPOINT ["hikrad-api"]
