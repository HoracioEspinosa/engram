# Container image for the ClaroDrive fork of engram (branch main).
#
# It is separate from docker/cloud/Dockerfile because that one builds without
# ldflags: the binary inside it answers `engram --version` with "dev", which
# makes a deployed image impossible to tell apart from any other build. This
# one stamps the version, so the running container can be identified from the
# outside and a rollback can name what it is rolling back to.
#
# Build (multi-arch, from the repository root):
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -f docker/custom/Dockerfile \
#     --build-arg ENGRAM_VERSION=1.20.0-cd.1 \
#     -t ghcr.io/horacioespinosa/engram:1.20.0-cd.1 .

FROM --platform=$BUILDPLATFORM golang:1.25.10-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

# Stamped into main.version. Defaults to "dev" so an un-parameterised build is
# honest about being unidentified instead of claiming a release version.
ARG ENGRAM_VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w -X main.version=${ENGRAM_VERSION}" -o /out/engram ./cmd/engram

FROM alpine:3.21

ARG ENGRAM_VERSION=dev
ARG ENGRAM_REVISION=unknown

LABEL org.opencontainers.image.title="engram (ClaroDrive fork)" \
      org.opencontainers.image.description="Project-scoped sync and dashboard runtime for the ClaroDrive engram fork." \
      org.opencontainers.image.source="https://github.com/HoracioEspinosa/engram" \
      org.opencontainers.image.version="${ENGRAM_VERSION}" \
      org.opencontainers.image.revision="${ENGRAM_REVISION}" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 10001 engram
USER engram
WORKDIR /home/engram
COPY --from=builder /out/engram /usr/local/bin/engram

EXPOSE 18080

# /health is served by the cloud runtime and answers 200 once the Postgres
# schema migration has run, so an unhealthy container means "do not shift
# traffic yet" rather than "the process died".
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O- "http://127.0.0.1:${ENGRAM_PORT:-18080}/health" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["engram"]
CMD ["cloud", "serve"]
