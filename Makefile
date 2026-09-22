BINARY := relay
PKG    := ./...
IMAGE  := cloudflare-smtp-relay

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile the relay binary into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(BINARY) ./cmd/relay

.PHONY: test
test: ## Run the full unit test suite
	go test -race -count=1 $(PKG)

.PHONY: cover
cover: ## Run tests and open an HTML coverage report
	go test -race -count=1 -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint: ## Run go vet, plus golangci-lint when it is installed
	go vet $(PKG)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed - skipping. Install: https://golangci-lint.run/welcome/install/"; \
	fi

.PHONY: tidy
tidy: ## Sync go.mod and go.sum
	go mod tidy

.PHONY: run
run: ## Run the relay locally using the .env file
	set -a; . ./.env; set +a; go run ./cmd/relay

.PHONY: docker
docker: ## Build the container image
	docker build -t $(IMAGE):latest .

.PHONY: up
up: ## Build and start the stack with docker compose
	docker compose up --build

.PHONY: down
down: ## Stop the stack
	docker compose down
