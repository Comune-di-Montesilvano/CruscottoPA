# ── Stage 1: Builder ────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS builder

WORKDIR /build

# Cache dependencies layer
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# modernc.org/sqlite è pure-Go: CGO disabilitato → binario statico, niente gcc.
# VERSION arriva dal tag git (release.yml); VERSION_SUFFIX="-DEV" per build locali
# (docker-compose.override.yml), così in UI si distingue un'immagine non ufficiale.
ARG VERSION=dev
ARG VERSION_SUFFIX=""
RUN CGO_ENABLED=0 GOOS=linux \
  go build \
  -ldflags="-s -w -X main.AppVersion=${VERSION}${VERSION_SUFFIX}" \
  -trimpath \
  -o cruscottopa \
  ./cmd/server

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.24

ARG VERSION=dev
LABEL org.opencontainers.image.version="${VERSION}"

# UID/GID fissi non root: compatibile con Podman rootless (nessuna porta < 1024,
# nessun privilegio richiesto) e con volumi nominati.
RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -g 1001 cruscottopa \
  && adduser -D -u 1001 -G cruscottopa cruscottopa \
  && mkdir -p /data \
  && chown cruscottopa:cruscottopa /data

WORKDIR /app

ENV DB_PATH=/data/cruscotto.db \
  PORT=8080 \
  TZ=Europe/Rome

COPY --from=builder /build/cruscottopa .
COPY web/ ./web/

# /data va montato come volume NOMINATO (non bind mount, vedi docker-compose.yml)
VOLUME ["/data"]

USER cruscottopa

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/app/cruscottopa", "-healthcheck"]

ENTRYPOINT ["/app/cruscottopa"]
