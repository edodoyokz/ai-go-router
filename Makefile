.PHONY: build test lint clean docker-build docker-push docker-run help

# Binary name
BINARY_NAME=9router
# Build directory
BUILD_DIR=bin
# Version (can be overridden)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
# Build time
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
# Git commit
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOLINT=golangci-lint run

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/router
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)"

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "Test coverage:"
	$(GOCMD) tool cover -func=coverage.out

lint: ## Run linter
	@echo "Running linter..."
	$(GOLINT) ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out
	@echo "Cleaned"

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t 9router:$(VERSION) .
	docker tag 9router:$(VERSION) 9router:latest
	@echo "Built 9router:$(VERSION) and 9router:latest"

docker-push: docker-build ## Push Docker image to registry
	@echo "Pushing Docker image..."
	docker push 9router:$(VERSION)
	docker push 9router:latest

docker-run: docker-build ## Run Docker container
	@echo "Running Docker container..."
	docker run -p 20128:20128 -v $(PWD)/config/config.yaml:/app/config/config.yaml:ro -v 9router-data:/app/data 9router:latest

run: build ## Build and run locally
	@echo "Running locally..."
	./$(BUILD_DIR)/$(BINARY_NAME) serve --config ./config/config.yaml

install: build ## Install binary to /usr/local/bin
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	$(GOCMD) mod download
	$(GOCMD) mod tidy

fmt: ## Format code
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOCMD) vet ./...
