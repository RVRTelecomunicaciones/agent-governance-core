# syntax=docker/dockerfile:1.7
# Multi-stage build for agent-governance-core. Production ships only the
# binary, migrations directory, and CA roots — no Go toolchain, no shell,
# no busybox. Mirrors the pattern used by sophia-orchestator and the rest
# of the Sophia ecosystem.

ARG GO_VERSION=1.26.2

# ---- builder ------------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS builder

# Build tools needed only inside the builder.
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache go.sum/go.mod separately to maximise Docker layer reuse.
COPY go.mod go.sum ./
RUN go mod download

# Source.
COPY cmd/        cmd/
COPY internal/   internal/
COPY migrations/ migrations/
COPY api/        api/

# Static, stripped binary. CGO disabled for distroless final stage.
ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0 GOOS=linux

RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/agent-governance-core \
    ./cmd/agent-governance-core

# ---- runner -------------------------------------------------------------
# Distroless/static: ~2 MiB base, no shell, no package manager, nonroot user.
# Migrations are bundled so the container can self-migrate when paired with
# the migration runner in a sidecar / init container (see compose stack).
# CA certs and zoneinfo are copied because pgx datetime conversions depend
# on /usr/share/zoneinfo and outbound TLS uses the system trust store.
FROM gcr.io/distroless/static-debian12:nonroot AS runner

COPY --from=builder /out/agent-governance-core           /usr/local/bin/agent-governance-core
COPY --from=builder /src/migrations                      /var/governance/migrations
COPY --from=builder /etc/ssl/certs/ca-certificates.crt   /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo                  /usr/share/zoneinfo

ENV GOVERNANCE_MIGRATIONS_PATH=/var/governance/migrations/postgres \
    TZ=UTC

USER nonroot:nonroot

EXPOSE 8080

# Distroless static has no shell, so HEALTHCHECK cannot run an inline
# script. Compose / k8s should poll /health from outside the container.

ENTRYPOINT ["/usr/local/bin/agent-governance-core"]
