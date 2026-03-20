# Multi-stage build for rulekit-registry.
# The final image defaults to SQLite — zero external dependencies required.
# To use PostgreSQL instead, set RULEKIT_STORE=postgres and supply RULEKIT_DATABASE_URL.

# ── Stage 1: builder ──────────────────────────────────────────────────────────
FROM golang:1.26.1-alpine AS builder

ARG VERSION=dev
ARG BUILD_TIME=unknown

WORKDIR /build

# Download dependencies first so Docker can cache this layer independently.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and compile a static binary with version metadata.
COPY . .
RUN go build \
  -ldflags "-X github.com/rulekit-dev/rulekit-registry/internal/version.Version=${VERSION} \
            -X github.com/rulekit-dev/rulekit-registry/internal/version.BuildTime=${BUILD_TIME}" \
  -o /rulekitd ./cmd/rulekitd

# ── Stage 2: final ────────────────────────────────────────────────────────────
FROM alpine:3.21

# wget is needed for the HEALTHCHECK.
RUN apk add --no-cache wget

COPY --from=builder /rulekitd /rulekitd

EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

USER nobody:nobody

ENTRYPOINT ["/rulekitd"]
