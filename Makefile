.PHONY: build test lint run docker-build docker-up docker-down

build:
	go build ./...

test:
	go test ./...

test-postgres:
	RULEKIT_DATABASE_URL=$(RULEKIT_DATABASE_URL) go test ./...

lint:
	go vet ./...

run:
	go run ./cmd/rulekitd

docker-build:
	docker build -t rulekit-registry .

docker-up:
	docker compose up

docker-up-postgres:
	docker compose --profile postgres up

docker-up-postgres-s3:
	docker compose --profile postgres-s3 up

docker-down:
	docker compose down
