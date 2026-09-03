MODULE := github.com/judeotine/afterword/services/api
BINARY := bin/api
MIGRATIONS_DIR := migrations
DOCKER_IMAGE ?= afterword/api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: all build run test test-integration lint fmt tidy sqlc migrate-up migrate-down docker clean

all: lint test build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/api

run:
	go run ./cmd/api

test:
	go test -race ./...

test-integration:
	go test -race -tags integration ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

tidy:
	go mod tidy

sqlc:
	sqlc generate

migrate-up: require-database-url
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down: require-database-url
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(VERSION) .

clean:
	rm -rf bin

.PHONY: require-database-url
require-database-url:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is not set"; exit 1; }
