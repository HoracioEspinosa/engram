# Throwaway runtime for validating koi-workspace changes against an isolated
# Engram instance. It is never pushed to a registry and it must never be
# pointed at ~/.engram: scripts/dev/*.sh only ever talk to the engram-dev-data
# volume this image starts with, which is empty until scripts/dev/seed.sh
# populates it.
#
# Build (from the repository root):
#   docker compose -f docker-compose.dev.yml build engram-dev
#   # or directly, stamping the same way docker/custom/Dockerfile does:
#   docker build -f docker/dev/Dockerfile \
#     --build-arg ENGRAM_VERSION=dev-$(git rev-parse --short HEAD) \
#     -t engram-dev:latest .

FROM golang:1.25.10-alpine AS builder

# Stamped into main.version with the same ldflags as docker/custom/Dockerfile,
# so `docker exec engram-dev engram version` identifies the build under test.
# Defaults to "dev" so an unparameterised build stays honest about being
# unidentified rather than claiming a real version.
ARG ENGRAM_VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${ENGRAM_VERSION}" -o /out/engram ./cmd/engram

FROM alpine:3.21

ARG ENGRAM_VERSION=dev

LABEL org.opencontainers.image.title="engram-dev" \
      org.opencontainers.image.description="Throwaway validation runtime. Never point this at ~/.engram." \
      org.opencontainers.image.version="${ENGRAM_VERSION}"

# Runtime packages for the throwaway validation image. Each one carries the
# single reason it is here so the list does not silently grow over time.
RUN apk add --no-cache \
    # trusts TLS certificates for any outbound HTTPS call the binary makes
    ca-certificates \
    # resolves named IANA zones; the binary does not embed the tzdata package
    tzdata \
    # captures TUI panes at a fixed, reproducible geometry
    tmux \
    # scripts/dev/*.sh and interactive `docker exec` shells expect bash, not ash
    bash \
    # runs integrity checks and row counts against the mounted data volume
    sqlite \
    # asserts MCP JSON envelopes from inside the container
    jq \
    # project detection execs git; without it, detection degrades to a
    # directory-name guess that write paths reject
    git \
    # without it TERM=xterm-256color renders monochrome
    ncurses-terminfo \
    # the HEALTHCHECK probes /health with it; alpine ships no curl by default
    wget

# Directories created (and chowned) before the volumes are ever mounted, so a
# fresh named volume inherits engram:engram ownership on first `compose up`
# instead of root.
RUN adduser -D -u 10001 engram \
 && mkdir -p /data /vault /out \
 && chown -R engram:engram /data /vault /out

USER engram
WORKDIR /home/engram
COPY --from=builder /out/engram /usr/local/bin/engram

ENV ENGRAM_DATA_DIR=/data \
    ENGRAM_VAULT_ROOT=/vault \
    CD_EVIDENCE_DIR=/vault/evidence \
    TERM=xterm-256color

EXPOSE 7437

# internal/server/server.go binds the HTTP API to 127.0.0.1 inside the
# container, so the probe targets loopback directly rather than the
# container's external address. /health returns 200 with {"status":"ok"} as
# soon as the mux is serving — it does not wait on any migration, unlike the
# cloud runtime's healthcheck in docker/custom/Dockerfile.
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=6 \
  CMD wget -q -O- "http://127.0.0.1:${ENGRAM_PORT:-7437}/health" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["engram"]
CMD ["serve"]
