.DEFAULT_GOAL := help
MODULE := github.com/prism-obs/prism
BIN := bin/controlplane

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Resolve and lock dependencies (requires network)
	go mod tidy

.PHONY: build
build: ## Build all service binaries (controlplane, gateway, relay, consumer, query, alerter, metering)
	CGO_ENABLED=0 go build -trimpath -o bin/controlplane ./cmd/controlplane
	CGO_ENABLED=0 go build -trimpath -o bin/gateway ./cmd/gateway
	CGO_ENABLED=0 go build -trimpath -o bin/relay ./cmd/relay
	CGO_ENABLED=0 go build -trimpath -o bin/consumer ./cmd/consumer
	CGO_ENABLED=0 go build -trimpath -o bin/query ./cmd/query
	CGO_ENABLED=0 go build -trimpath -o bin/alerter ./cmd/alerter
	CGO_ENABLED=0 go build -trimpath -o bin/metering ./cmd/metering

.PHONY: run
run: ## Run the control plane locally (expects .env exported)
	go run ./cmd/controlplane

.PHONY: run-gateway
run-gateway: ## Run the ingest gateway locally (expects .env exported)
	go run ./cmd/gateway

.PHONY: run-relay
run-relay: ## Run the outbox relay locally (expects .env exported)
	go run ./cmd/relay

.PHONY: run-consumer
run-consumer: ## Run the stream consumer locally (expects .env exported)
	go run ./cmd/consumer

.PHONY: run-query
run-query: ## Run the query service locally (expects .env exported)
	go run ./cmd/query

.PHONY: run-alerter
run-alerter: ## Run the alerting service locally (expects .env exported)
	go run ./cmd/alerter

.PHONY: run-metering
run-metering: ## Run the metering service locally (expects .env exported)
	go run ./cmd/metering

.PHONY: test
test: ## Run unit tests
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires Docker)
	go test -race -count=1 -tags=integration ./test/integration/...

.PHONY: cover
cover: ## Produce a coverage report
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format the codebase
	gofmt -w .
	goimports -w .

.PHONY: up
up: ## Start the local stack
	docker compose -f deployments/docker-compose.yaml up --build

.PHONY: down
down: ## Stop the local stack
	docker compose -f deployments/docker-compose.yaml down -v

.PHONY: docker-build
docker-build: ## Build the container image
	docker build -f build/Dockerfile -t prism-controlplane:dev .
