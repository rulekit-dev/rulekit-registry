# Multi-stage build for rulekit-registry.
# The final image defaults to SQLite — zero external dependencies required.
# To use PostgreSQL instead, set RULEKIT_STORE=postgres and supply RULEKIT_DATABASE_URL.

# ── Stage 1: builder ──────────────────────────────────────────────────────────
FROM golang:1.26.1-alpine AS builder

WORKDIR /build

# Download dependencies first so Docker can cache this layer independently.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and compile a static binary.
COPY . .
RUN go build -o /rulekitd ./cmd/rulekitd

# ── Stage 2: final (distroless, non-root) ─────────────────────────────────────
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /rulekitd /rulekitd

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/rulekitd"]
