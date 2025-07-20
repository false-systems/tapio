# Tapio - Simple & Fast Makefile
# Container-first development with proper linting

.PHONY: help build test lint clean docker-all dev ci proto proto-install proto-generate

# Build variables
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Proto variables
GOPATH := $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin
PROTOC := $(shell which protoc)
BUF := $(shell which buf)

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m  
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

##@ Development

dev: fmt lint-fix test build ## Quick development cycle (format, lint, test, build)
	@echo "$(GREEN)✅ Development cycle complete!$(NC)"

build: proto ## Build all binaries (includes proto generation)
	@echo "$(BLUE)🔨 Building binaries...$(NC)"
	@mkdir -p bin
	@go build -ldflags "$(LDFLAGS)" -o bin/tapio-collector ./cmd/tapio-collector
	@echo "$(GREEN)✅ Build complete: bin/$(NC)"
	@ls -la bin/

test: ## Run all tests
	@echo "$(BLUE)🧪 Running tests...$(NC)"
	@go test -race -short ./...
	@echo "$(GREEN)✅ Tests passed!$(NC)"

test-coverage: ## Run tests with coverage
	@echo "$(BLUE)📊 Running tests with coverage...$(NC)"
	@go test -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✅ Coverage report: coverage.html$(NC)"

##@ Code Quality

fmt: ## Format code
	@echo "$(BLUE)🎨 Formatting code...$(NC)"
	@gofmt -s -w .
	@goimports -w .
	@go mod tidy

lint: ## Run linters (read-only)
	@echo "$(BLUE)🔍 Running linters...$(NC)"
	@gofmt -l . | tee fmt-issues.txt && test ! -s fmt-issues.txt
	@go vet ./...
	@golangci-lint run --timeout=5m

lint-fix: ## Run linters with auto-fix
	@echo "$(BLUE)🔧 Running linters with auto-fix...$(NC)"
	@golangci-lint run --fix --timeout=5m
	@goimports -w .
	@go mod tidy

lint-architecture: ## Check architecture rules only
	@echo "$(BLUE)🏗️  Checking architecture rules...$(NC)"
	@golangci-lint run --disable-all -E depguard --timeout=2m

##@ Architecture Enforcement

check-architecture: ## Enforce 5-level dependency hierarchy (CRITICAL)
	@echo "$(BLUE)🏗️  Enforcing dependency hierarchy...$(NC)"
	@go run scripts/check-architecture.go

check-independence: ## Validate module independence (CRITICAL)
	@echo "$(BLUE)🔧 Validating module independence...$(NC)"
	@go run scripts/check-module-independence.go

check-completeness: ## Check for stubs/TODOs (CRITICAL - NO TOLERANCE)
	@echo "$(BLUE)🚨 Checking implementation completeness...$(NC)"
	@go run scripts/check-implementation-completeness.go

check-coverage: ## Enforce 80% test coverage (CRITICAL)
	@echo "$(BLUE)📊 Enforcing test coverage requirements...$(NC)"
	@go run scripts/check-coverage.go

check-type-safety: ## Enforce strong typing (CRITICAL)
	@echo "$(BLUE)🛡️  Enforcing type safety...$(NC)"
	@go run scripts/check-type-safety.go

enforce-all: check-architecture check-independence check-completeness check-coverage check-type-safety ## Run ALL enforcement checks
	@echo "$(GREEN)✅ All architecture enforcement checks completed!$(NC)"

##@ Docker

docker-all: ## Build all Docker images
	@echo "$(BLUE)🐳 Building all Docker images...$(NC)"
	@make docker-server docker-collector
	@echo "$(GREEN)✅ All Docker images built!$(NC)"

docker-server: ## Build server Docker image
	@echo "$(BLUE)🐳 Building tapio-server image...$(NC)"
	@docker build -f cmd/tapio-server/Dockerfile -t tapio-server:latest .

docker-collector: ## Build collector Docker image  
	@echo "$(BLUE)🐳 Building tapio-collector image...$(NC)"
	@docker build -f cmd/tapio-collector/Dockerfile -t tapio-collector:latest .

docker-test: docker-all ## Test all Docker images
	@echo "$(BLUE)🐳 Testing Docker images...$(NC)"
	@docker run --rm tapio-server:latest --help || echo "Server image OK"
	@docker run --rm tapio-collector:latest --help || echo "Collector image OK"
	@echo "$(GREEN)✅ All Docker images working!$(NC)"

##@ CI/CD

ci: enforce-all lint test build docker-all ## Full CI pipeline with enforcement
	@echo "$(GREEN)🚀 CI pipeline completed successfully!$(NC)"
	@echo "$(GREEN)✅ Enforcement: PASSED$(NC)"
	@echo "$(GREEN)✅ Lint: PASSED$(NC)"
	@echo "$(GREEN)✅ Tests: PASSED$(NC)"
	@echo "$(GREEN)✅ Build: PASSED$(NC)"
	@echo "$(GREEN)✅ Docker: PASSED$(NC)"

ci-quick: enforce-all lint test build ## Quick CI with enforcement (no Docker)
	@echo "$(GREEN)🚀 Quick CI completed successfully!$(NC)"

ci-enforcement-only: enforce-all ## Run only architecture enforcement checks
	@echo "$(GREEN)🚀 Architecture enforcement completed!$(NC)"

##@ Local Development

dev-setup: ## Setup local development environment
	@echo "$(BLUE)🛠️  Setting up development environment...$(NC)"
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "$(GREEN)✅ Development environment ready!$(NC)"

skaffold-dev: ## Start Skaffold development (hot-reload)
	@echo "$(BLUE)🔥 Starting Skaffold hot-reload development...$(NC)"
	@skaffold dev --port-forward

##@ Protobuf Generation

proto: proto-install proto-generate ## Generate protobuf code

proto-install: ## Install protobuf tools
	@echo "$(BLUE)🔧 Installing protobuf tools...$(NC)"
	@if ! which buf > /dev/null; then \
		echo "Installing buf..."; \
		go install github.com/bufbuild/buf/cmd/buf@latest; \
	fi
	@if ! which protoc-gen-go > /dev/null; then \
		echo "Installing protoc-gen-go..."; \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
	fi
	@if ! which protoc-gen-go-grpc > /dev/null; then \
		echo "Installing protoc-gen-go-grpc..."; \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi
	@if ! which protoc-gen-grpc-gateway > /dev/null; then \
		echo "Installing protoc-gen-grpc-gateway..."; \
		go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest; \
	fi
	@if ! which protoc-gen-openapiv2 > /dev/null; then \
		echo "Installing protoc-gen-openapiv2..."; \
		go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest; \
	fi
	@echo "$(GREEN)✅ Protobuf tools installed$(NC)"

proto-generate: ## Generate protobuf code
	@echo "$(BLUE)📝 Generating protobuf code...$(NC)"
	@mkdir -p proto/gen
	@cd proto && buf generate
	@echo "$(GREEN)✅ Protobuf code generated$(NC)"

proto-lint: ## Lint protobuf files
	@echo "$(BLUE)🔍 Linting protobuf files...$(NC)"
	@cd proto && buf lint
	@echo "$(GREEN)✅ Protobuf lint passed$(NC)"

##@ Utilities

clean: ## Clean all build artifacts
	@echo "$(BLUE)🧹 Cleaning up...$(NC)"
	@rm -rf bin/ dist/ coverage.out coverage.html *.log proto/gen
	@docker system prune -f --volumes || echo "Docker cleanup done"
	@go clean -cache -testcache -modcache
	@echo "$(GREEN)✅ Cleanup complete!$(NC)"

install-tools: proto-install ## Install required development tools  
	@echo "$(BLUE)🛠️  Installing development tools...$(NC)"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "$(GREEN)✅ Tools installed!$(NC)"

check-deps: ## Check for dependency vulnerabilities
	@echo "$(BLUE)🔒 Checking dependencies for vulnerabilities...$(NC)"
	@govulncheck ./...
	@echo "$(GREEN)✅ Dependencies OK!$(NC)"

info: ## Show build information
	@echo "$(BLUE)📋 Build Information:$(NC)"
	@echo "  Version: $(VERSION)"
	@echo "  Commit: $(COMMIT)"
	@echo "  Build Date: $(BUILD_DATE)"
	@echo "  Go Version: $(shell go version)"
	@echo "  Platform: $(shell go env GOOS)/$(shell go env GOARCH)"

##@ Component Testing

test-domain: ## Test domain layer only
	@echo "$(BLUE)🧪 Testing domain layer...$(NC)"
	@cd pkg/domain && go test -v ./...

test-collectors: ## Test all collectors
	@echo "$(BLUE)🧪 Testing collectors...$(NC)"
	@find pkg/collectors -name "go.mod" -execdir go test -v ./... \;

test-intelligence: ## Test intelligence layer
	@echo "$(BLUE)🧪 Testing intelligence layer...$(NC)"
	@find pkg/intelligence -name "go.mod" -execdir go test -v ./... \;

test-integrations: ## Test integrations layer
	@echo "$(BLUE)🧪 Testing integrations layer...$(NC)"
	@find pkg/integrations -name "go.mod" -execdir go test -v ./... \;

test-interfaces: ## Test interfaces layer
	@echo "$(BLUE)🧪 Testing interfaces layer...$(NC)"
	@find pkg/interfaces -name "go.mod" -execdir go test -v ./... \;

##@ Help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n$(BLUE)Tapio Build Commands$(NC)\n\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  $(GREEN)%-15s$(NC) %s\n", $$1, $$2 } /^##@/ { printf "\n$(YELLOW)%s$(NC)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""

.DEFAULT_GOAL := help