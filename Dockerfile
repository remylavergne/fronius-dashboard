# syntax=docker/dockerfile:1

# Architecture follows the build daemon, not the host: every stage uses
# multi-arch base images and CGO is off, so an amd64 daemon (a Colima profile
# started with --arch x86_64, for example) produces an amd64 image with the
# classic builder — no --platform flag is needed or accepted here.

# ---- interface --------------------------------------------------------------
# Built separately so the Go layer caches independently of the pnpm install.
FROM node:22-alpine AS web
RUN corepack enable && corepack prepare pnpm@10.12.4 --activate
WORKDIR /src
COPY apps/web/package.json apps/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY apps/web/ ./
RUN pnpm build

# ---- service ----------------------------------------------------------------
FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The interface is embedded in the binary: copy it into the go:embed directory
# before compiling (the directory is tracked as an empty placeholder).
COPY --from=web /src/dist ./internal/webui/assets/
# CGO off: the SQLite driver is pure Go, so the binaries are static.
# No .git in the context, hence -buildvcs=false and a stamped version.
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags="-s -w -X main.buildVersion=${VERSION}" \
        -o /out/ ./cmd/...

# ---- runtime ----------------------------------------------------------------
FROM alpine:3.22
# ca-certificates: a device behind HTTPS; tzdata: FRONIUS_TZ needs a zone
# database on the image (Go reads it from the OS).
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 fronius \
 && adduser  -S -D -H -u 10001 -G fronius fronius \
 && mkdir -p /data \
 && chown fronius:fronius /data
WORKDIR /app
COPY --from=build /out/ /app/
COPY docker-entrypoint.sh /app/entrypoint.sh
RUN chmod 0755 /app/entrypoint.sh
USER 10001:10001

# In a container the boundary is the *published* port, not the bind address:
# compose publishes it on 127.0.0.1 by default because the API has no
# authentication of its own (ADR 0005). FRONIUS_ALLOW_REMOTE only lifts the
# in-container bind guard — it does not expose anything by itself.
ENV FRONIUS_HTTP_ADDR=0.0.0.0:8080 \
    FRONIUS_DB_PATH=/data/fronius.db \
    FRONIUS_TZ=Europe/Brussels \
    FRONIUS_ALLOW_REMOTE=1

VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/v1/health >/dev/null || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/froniusd"]
