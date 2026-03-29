.PHONY: up down build logs test release

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

# Tag and push a release. Usage: make release VERSION=1.2.3
release:
	@test -n "$(VERSION)" || (echo "VERSION is required. Usage: make release VERSION=1.2.3" && exit 1)
	git tag v$(VERSION)
	git push origin v$(VERSION)
