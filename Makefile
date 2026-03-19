.PHONY: up down build logs test test-postgres

# Start the registry (reads .env for all configuration).
# Copy .env.example to .env and adjust before running.
up:
	docker compose up --build

# Stop and remove containers.
down:
	docker compose down

# Build the Docker image without starting.
build:
	docker compose build

# Tail registry logs.
logs:
	docker compose logs -f registry

# Run tests locally (SQLite only, no Docker required).
test:
	go test ./...

# Run tests against a live PostgreSQL instance.
# Usage: make test-postgres RULEKIT_DATABASE_URL=postgres://...
test-postgres:
	RULEKIT_DATABASE_URL=$(RULEKIT_DATABASE_URL) go test ./...
