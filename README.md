# rulekit-registry

RuleKit separates decision logic from application code. Teams define rules visually using a decision-table format, version and publish them via the registry, pull specific versions at build-time using the CLI, and evaluate rules locally via the SDK — no runtime network calls required.

## Quickstart

### SQLite (zero dependencies)

```sh
docker run -p 8080:8080 -v $(pwd)/data:/data ghcr.io/rulekit/rulekit-registry:latest
```

### PostgreSQL

```sh
docker compose --profile postgres up
```

### PostgreSQL + S3 blob store

```sh
export RULEKIT_S3_BUCKET=my-bucket
export RULEKIT_S3_ACCESS_KEY_ID=...
export RULEKIT_S3_SECRET_ACCESS_KEY=...
docker compose --profile postgres-s3 up
```

## API Reference

### Rulesets

| Method | Path | Description |
|--------|------|-------------|
| GET | /v1/rulesets?namespace=default | List rulesets |
| POST | /v1/rulesets | Create ruleset |
| GET | /v1/rulesets/{key} | Get ruleset |
| GET | /v1/rulesets/{key}/draft | Get draft DSL |
| PUT | /v1/rulesets/{key}/draft | Update draft DSL (validates) |
| POST | /v1/rulesets/{key}/publish | Publish immutable version |
| GET | /v1/rulesets/{key}/versions | List versions |
| GET | /v1/rulesets/{key}/versions/{version} | Get specific version |
| GET | /v1/rulesets/{key}/versions/latest | Get latest version |
| GET | /v1/rulesets/{key}/versions/{version}/bundle | Download bundle (.zip) |
| GET | /v1/rulesets/{key}/versions/latest/bundle | Download latest bundle (.zip) |

### curl Examples

**1. Create a ruleset**

```sh
curl -s -X POST http://localhost:8080/v1/rulesets \
  -H "Content-Type: application/json" \
  -d '{"namespace":"default","key":"pricing","description":"Pricing rules"}'
```

**2. Save a draft**

```sh
curl -s -X PUT http://localhost:8080/v1/rulesets/pricing/draft?namespace=default \
  -H "Content-Type: application/json" \
  -d '{
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
  }'
```

**3. Publish**

```sh
curl -s -X POST http://localhost:8080/v1/rulesets/pricing/publish?namespace=default
```

**4. Get latest version**

```sh
curl -s http://localhost:8080/v1/rulesets/pricing/versions/latest?namespace=default
```

**5. Download latest bundle**

```sh
curl -s -OJ http://localhost:8080/v1/rulesets/pricing/versions/latest/bundle?namespace=default
```

### Error Format

```json
{ "error": { "code": "NOT_FOUND", "message": "not found" } }
```

## Configuration

| Env var | Flag | Default | Description |
|---------|------|---------|-------------|
| RULEKIT_ADDR | --addr | :8080 | Listen address |
| RULEKIT_STORE | --store | sqlite | Relational backend (`sqlite` \| `postgres`) |
| RULEKIT_DATA_DIR | --data-dir | ./data | SQLite data directory |
| RULEKIT_DATABASE_URL | --database-url | — | PostgreSQL DSN (required when store=postgres) |
| RULEKIT_BLOB_STORE | --blob-store | fs | Blob backend (`fs` \| `s3`) |
| RULEKIT_BLOB_DIR | --blob-dir | {DATA_DIR}/blobs | Directory for fs blob store |
| RULEKIT_S3_BUCKET | --s3-bucket | — | S3 bucket name (required when blob-store=s3) |
| RULEKIT_S3_ENDPOINT | --s3-endpoint | — | Custom S3 endpoint (e.g. Cloudflare R2) |
| RULEKIT_S3_REGION | --s3-region | us-east-1 | S3 region (`auto` for R2) |
| RULEKIT_S3_ACCESS_KEY_ID | --s3-access-key-id | — | S3 access key ID |
| RULEKIT_S3_SECRET_ACCESS_KEY | --s3-secret-access-key | — | S3 secret access key |

## Storage Backends

Storage is split into two independent layers: a **relational store** for metadata and drafts, and a **blob store** for published DSL and bundle files.

### Relational store

**SQLite** (default) requires no external services and stores everything in a single file under `RULEKIT_DATA_DIR`. Suitable for local development and single-node deployments.

**PostgreSQL** is recommended for multi-instance or production deployments. Set `RULEKIT_STORE=postgres` and provide `RULEKIT_DATABASE_URL`.

### Blob store

**fs** (default) writes blobs to the local filesystem under `RULEKIT_BLOB_DIR` (defaults to `{RULEKIT_DATA_DIR}/blobs`). Suitable for single-node deployments where the data directory is on a persistent volume.

**s3** stores blobs in an S3-compatible bucket. Supports AWS S3 and any compatible service (Cloudflare R2, MinIO, etc.). Set `RULEKIT_BLOB_STORE=s3` and provide `RULEKIT_S3_BUCKET`. For Cloudflare R2, set `RULEKIT_S3_ENDPOINT` to your account endpoint and `RULEKIT_S3_REGION=auto`.

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

A bundle is a `.zip` archive containing two files:

- `manifest.json` — metadata: `namespace`, `ruleset_key`, `version`, `checksum`, `created_at`
- `dsl.json` — the published DSL in deterministically serialized form (all object keys sorted), so that the checksum is stable across any environment

Checksums use the format `sha256:<hex>` and are computed over the deterministic bytes of `dsl.json`.

## Project Structure

```
rulekit-registry/
  cmd/rulekitd/         binary entrypoint
  internal/
    api/                HTTP router and handlers
    blobstore/          BlobStore interface and sentinel errors
    blobstore/fs/       filesystem blob backend
    blobstore/s3/       S3-compatible blob backend
    config/             env + flag configuration
    dsl/                DSL parser, validator, deterministic serializer
    model/              Ruleset, Draft, Version structs
    store/              Store interface and sentinel errors
    store/sqlite/       SQLite backend
    store/postgres/     PostgreSQL backend
    store/testhelper/   shared backend test suite
  Dockerfile
  docker-compose.yml
```

## Roadmap

- Authentication & API keys
- RBAC (per-namespace permissions)
- Multi-tenant isolation
- rulekit-dashboard — visual board-style DSL editor
- rulekit-cli — pull versions, manage lockfiles, verify checksums
- rulekit-sdk — local DSL evaluation, no runtime network calls

## Contributing

Standard Go project. Requires Go 1.26+.

```sh
go mod download
go test ./...
```

For PostgreSQL tests, set `RULEKIT_DATABASE_URL` before running tests.
