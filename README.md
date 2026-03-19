# rulekit-registry

RuleKit separates decision logic from application code. Teams define rules visually using a decision-table format, version and publish them via the registry, pull specific versions at build-time using the CLI, and evaluate rules locally via the SDK — no runtime network calls required.

## Quickstart

```sh
cp .env.example .env   # edit to taste
make up                # build image and start
```

That's all. `make up` uses SQLite + local filesystem by default — zero external dependencies. Everything else is configured in `.env`.

To stop: `make down`. To tail logs: `make logs`.

### Use Postgres

Set these in `.env` and run `make up` as normal:

```ini
RULEKIT_STORE=postgres
RULEKIT_DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=disable
```

Bring your own Postgres — managed cloud, local Docker, bare metal, whatever. The registry only needs a connection string.

### Use S3 blob storage

Add to `.env`, then `make up`:

```ini
RULEKIT_BLOB_STORE=s3
RULEKIT_S3_BUCKET=my-rulekit-bucket
RULEKIT_S3_ACCESS_KEY_ID=...
RULEKIT_S3_SECRET_ACCESS_KEY=...
# Cloudflare R2: also set RULEKIT_S3_ENDPOINT and RULEKIT_S3_REGION=auto
```

### Enable JWT auth

Add to `.env`, then `make up`:

```ini
RULEKIT_AUTH=jwt
RULEKIT_JWT_SECRET=<output of: openssl rand -hex 32>
RULEKIT_ADMIN_EMAIL=admin@example.com
```

OTP codes are printed to stdout until you configure SMTP. All combinations work — any store, any blob backend, any auth mode.

## Authentication

RuleKit supports two auth modes controlled by `RULEKIT_AUTH`.

### `none` (default)

All `/v1/*` routes are open, or protected by a single shared bearer token if `RULEKIT_API_KEY` is set. Suitable for local development or trusted internal networks.

```sh
# Optional: protect all routes with a single key
RULEKIT_API_KEY=my-secret-key
```

Every request must then include:

```
Authorization: Bearer my-secret-key
```

### `jwt` — Email + OTP login with RBAC

Full multi-tenant auth with per-namespace role control.

**Required env vars:**

```sh
RULEKIT_AUTH=jwt
RULEKIT_JWT_SECRET=<64-char random hex>   # openssl rand -hex 32
RULEKIT_ADMIN_EMAIL=admin@example.com     # bootstrapped on first startup
```

**Optional SMTP** (if not set, OTP codes print to stdout):

```sh
RULEKIT_SMTP_HOST=smtp.example.com
RULEKIT_SMTP_PORT=587
RULEKIT_SMTP_USERNAME=...
RULEKIT_SMTP_PASSWORD=...
RULEKIT_SMTP_FROM=noreply@example.com
```

#### Login flow

```sh
# 1. Request OTP — always returns 200 (no email enumeration)
curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com"}'

# 2. Verify OTP → receive access + refresh tokens
curl -s -X POST http://localhost:8080/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","code":"123456"}'
# → { "access_token": "...", "refresh_token": "...", "expires_in": 900 }

# 3. Use the access token on subsequent requests
curl -s http://localhost:8080/v1/rulesets \
  -H "Authorization: Bearer <access_token>"

# 4. Refresh before the 15-minute access token expires
curl -s -X POST http://localhost:8080/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'

# 5. Logout (revokes the refresh token)
curl -s -X POST http://localhost:8080/v1/auth/logout \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'
```

#### Roles

| Role | Bitmask | Can do |
|------|---------|--------|
| `viewer` | `1` | Read published versions and bundles |
| `editor` | `2` | Create, edit, and publish rulesets |
| `admin`  | `4` | Manage users, roles, and API tokens |

Roles are **per namespace**. A role on namespace `*` applies globally (used for admins). Combine with bitwise OR: viewer + editor = `3`.

#### Admin: manage users and roles

```sh
# List users
curl -s http://localhost:8080/v1/admin/users \
  -H "Authorization: Bearer <admin_token>"

# Assign editor role on the "payments" namespace
curl -s -X PUT http://localhost:8080/v1/admin/users/{userID}/roles/payments \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"role_mask":2}'

# Remove role
curl -s -X DELETE http://localhost:8080/v1/admin/users/{userID}/roles/payments \
  -H "Authorization: Bearer <admin_token>"

# Delete user
curl -s -X DELETE http://localhost:8080/v1/admin/users/{userID} \
  -H "Authorization: Bearer <admin_token>"
```

#### Admin: issue API tokens (CLI / CI / SDK)

Long-lived opaque tokens for non-interactive clients. The raw token is only shown once on creation.

```sh
# Issue a CI token — editor on "payments", expires in 90 days
curl -s -X POST http://localhost:8080/v1/admin/tokens \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "<userID>",
    "name": "ci-pipeline",
    "namespace": "payments",
    "role": 2,
    "expires_in_days": 90
  }'
# → { "id": "...", "token": "rk_...", "namespace": "payments", "role": 2, ... }

# List tokens for a user
curl -s "http://localhost:8080/v1/admin/tokens?user_id=<userID>" \
  -H "Authorization: Bearer <admin_token>"

# Revoke a token
curl -s -X DELETE http://localhost:8080/v1/admin/tokens/{tokenID} \
  -H "Authorization: Bearer <admin_token>"
```

Use the issued token exactly like a JWT:

```
Authorization: Bearer rk_...
```

## API Reference

### Auth (`RULEKIT_AUTH=jwt` only)

| Method | Path | Description |
|--------|------|-------------|
| POST | /v1/auth/login | Request OTP for email |
| POST | /v1/auth/verify | Verify OTP → access + refresh tokens |
| POST | /v1/auth/refresh | Rotate refresh token → new access token |
| POST | /v1/auth/logout | Revoke refresh token |

### Rulesets

| Method | Path | Min role | Description |
|--------|------|----------|-------------|
| GET | /v1/rulesets?namespace=default | viewer | List rulesets |
| POST | /v1/rulesets | editor | Create ruleset |
| GET | /v1/rulesets/{key} | viewer | Get ruleset |
| DELETE | /v1/rulesets/{key} | editor | Delete ruleset |
| GET | /v1/rulesets/{key}/draft | editor | Get draft DSL |
| PUT | /v1/rulesets/{key}/draft | editor | Save draft DSL (validates, max 1 MiB) |
| DELETE | /v1/rulesets/{key}/draft | editor | Delete draft |
| POST | /v1/rulesets/{key}/publish | editor | Publish immutable version |
| GET | /v1/rulesets/{key}/versions | viewer | List versions |
| GET | /v1/rulesets/{key}/versions/{version} | viewer | Get specific version |
| GET | /v1/rulesets/{key}/versions/latest | viewer | Get latest version |
| GET | /v1/rulesets/{key}/versions/{version}/bundle | viewer | Download bundle (.zip) |
| GET | /v1/rulesets/{key}/versions/latest/bundle | viewer | Download latest bundle (.zip) |

### Admin (`RULEKIT_AUTH=jwt`, admin role required)

| Method | Path | Description |
|--------|------|-------------|
| GET | /v1/admin/users | List users |
| DELETE | /v1/admin/users/{userID} | Delete user |
| GET | /v1/admin/users/{userID}/roles | List user's namespace roles |
| PUT | /v1/admin/users/{userID}/roles/{namespace} | Set role mask for namespace |
| DELETE | /v1/admin/users/{userID}/roles/{namespace} | Remove namespace role |
| POST | /v1/admin/tokens | Issue long-lived API token |
| GET | /v1/admin/tokens?user_id=... | List API tokens for user |
| DELETE | /v1/admin/tokens/{tokenID} | Revoke API token |

### Healthcheck

```
GET /healthz   → 200 { "status": "ok" }   (never requires auth)
```

### curl Examples

**Create a ruleset**

```sh
curl -s -X POST http://localhost:8080/v1/rulesets \
  -H "Content-Type: application/json" \
  -d '{"namespace":"default","key":"pricing","name":"Pricing Rules"}'
```

**Save a draft**

```sh
curl -s -X PUT 'http://localhost:8080/v1/rulesets/pricing/draft?namespace=default' \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": {
      "dsl_version": "v1",
      "strategy": "first_match",
      "schema": {
        "amount":   { "type": "number" },
        "currency": { "type": "enum", "options": ["IDR", "USD"] },
        "weekend":  { "type": "boolean" }
      },
      "rules": [
        {
          "id": "r1",
          "name": "High amount",
          "when": [{ "field": "amount", "op": "gt", "value": 50000000 }],
          "then": { "decision": "manual_review" }
        }
      ],
      "default": { "decision": "fallback" }
    }
  }'
```

**Publish**

```sh
curl -s -X POST 'http://localhost:8080/v1/rulesets/pricing/publish?namespace=default'
```

**Get latest version**

```sh
curl -s 'http://localhost:8080/v1/rulesets/pricing/versions/latest?namespace=default'
```

**Download latest bundle**

```sh
curl -s -OJ 'http://localhost:8080/v1/rulesets/pricing/versions/latest/bundle?namespace=default'
```

### Error Format

```json
{ "error": { "code": "NOT_FOUND", "message": "ruleset not found" } }
```

Common error codes: `NOT_FOUND`, `ALREADY_EXISTS`, `INVALID_DSL`, `INVALID_KEY`, `INVALID_NAMESPACE`, `NO_CHANGES`, `BAD_REQUEST`, `REQUEST_TOO_LARGE`, `UNAUTHORIZED`, `TOKEN_EXPIRED`, `FORBIDDEN`, `INTERNAL`.

## Configuration

### Server

| Env var | Default | Description |
|---------|---------|-------------|
| `RULEKIT_ADDR` | `:8080` | Listen address |

### Relational store

| Env var | Default | Description |
|---------|---------|-------------|
| `RULEKIT_STORE` | `sqlite` | Backend: `sqlite` or `postgres` |
| `RULEKIT_DATA_DIR` | `./data` | SQLite data directory |
| `RULEKIT_DATABASE_URL` | — | PostgreSQL DSN (required when store=postgres) |

### Blob store

| Env var | Default | Description |
|---------|---------|-------------|
| `RULEKIT_BLOB_STORE` | `fs` | Backend: `fs` or `s3` |
| `RULEKIT_BLOB_DIR` | `{DATA_DIR}/blobs` | Directory for fs blob store |
| `RULEKIT_S3_BUCKET` | — | S3 bucket name (required when blob-store=s3) |
| `RULEKIT_S3_ENDPOINT` | — | Custom S3 endpoint (e.g. Cloudflare R2) |
| `RULEKIT_S3_REGION` | `us-east-1` | S3 region (`auto` for R2) |
| `RULEKIT_S3_ACCESS_KEY_ID` | — | S3 access key ID |
| `RULEKIT_S3_SECRET_ACCESS_KEY` | — | S3 secret access key |

### Authentication

| Env var | Default | Description |
|---------|---------|-------------|
| `RULEKIT_AUTH` | `none` | Auth mode: `none` or `jwt` |
| `RULEKIT_API_KEY` | — | Single shared bearer token (mode=none only) |
| `RULEKIT_JWT_SECRET` | — | JWT signing secret, min 32 bytes (required when auth=jwt) |
| `RULEKIT_ADMIN_EMAIL` | — | Bootstrap admin email (created on first startup) |

### SMTP (optional, for email OTP)

| Env var | Default | Description |
|---------|---------|-------------|
| `RULEKIT_SMTP_HOST` | — | SMTP server hostname. If unset, OTP codes print to stdout |
| `RULEKIT_SMTP_PORT` | `587` | SMTP port |
| `RULEKIT_SMTP_USERNAME` | — | SMTP username |
| `RULEKIT_SMTP_PASSWORD` | — | SMTP password |
| `RULEKIT_SMTP_FROM` | — | From address for OTP emails |
| `RULEKIT_SMTP_USE_TLS` | `false` | Use implicit TLS (port 465) instead of STARTTLS |

## Storage Backends

Storage is split into two independent layers.

### Relational store

**SQLite** (default) — zero config, single file, suitable for local dev and single-node deployments.

**PostgreSQL** — recommended for multi-instance or production. Set `RULEKIT_STORE=postgres` and provide `RULEKIT_DATABASE_URL`.

### Blob store

**fs** (default) — writes blobs to the local filesystem under `RULEKIT_BLOB_DIR`. Suitable for single-node deployments with a persistent volume.

**s3** — stores blobs in an S3-compatible bucket. Supports AWS S3, Cloudflare R2, MinIO, etc. For Cloudflare R2, set `RULEKIT_S3_ENDPOINT` and `RULEKIT_S3_REGION=auto`.

## DSL Format

```json
{
  "dsl_version": "v1",
  "strategy": "first_match",
  "schema": {
    "amount":   { "type": "number" },
    "currency": { "type": "enum", "options": ["IDR", "USD"] },
    "weekend":  { "type": "boolean" }
  },
  "rules": [
    {
      "id": "r1",
      "name": "High amount",
      "when": [{ "field": "amount", "op": "gt", "value": 50000000 }],
      "then": { "decision": "manual_review" }
    }
  ],
  "default": { "decision": "fallback" }
}
```

**Field types:** `number`, `string`, `boolean`, `enum`

**Strategies:** `first_match`, `all_matches`

## Bundle Format

A bundle is a `.zip` archive containing:

- `manifest.json` — metadata: `namespace`, `ruleset_key`, `version`, `checksum`, `created_at`
- `dsl.json` — deterministically serialized DSL (all object keys sorted alphabetically)

Checksums use the format `sha256:<hex>` computed over the deterministic bytes of `dsl.json`. The CLI uses the manifest checksum to verify bundle integrity after download.

## Project Structure

```
rulekit-registry/
  cmd/rulekitd/           binary entrypoint
  internal/
    api/                  HTTP router, handlers, auth, admin, middleware
    blobstore/            BlobStore interface and sentinel errors
    blobstore/fs/         filesystem blob backend
    blobstore/s3/         S3-compatible blob backend
    config/               env + flag configuration
    dsl/                  DSL parser, validator, deterministic serializer
    jwtutil/              JWT sign/parse helpers
    mailer/               SMTP mailer + stdout fallback
    model/                data models (Ruleset, Draft, Version, User, Role, …)
    store/                Store interface and sentinel errors
    store/sqlite/         SQLite backend
    store/postgres/       PostgreSQL backend
    store/testhelper/     shared backend test suite
  .env.example
  Dockerfile
  docker-compose.yml
  Makefile
```

## Development

| Command | Description |
|---------|-------------|
| `make up` | Build image and start (reads `.env`) |
| `make down` | Stop and remove containers |
| `make build` | Build the Docker image without starting |
| `make logs` | Tail registry container logs |
| `make test` | Run tests locally (no Docker, SQLite only) |
| `make test-postgres RULEKIT_DATABASE_URL=...` | Run tests against a live Postgres |

Requires Go 1.26+ for local test runs. Copy `.env.example` to `.env` before running.
