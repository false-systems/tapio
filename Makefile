.PHONY: ci-check ci-test ci-build ci fmt lint lint-fix vet test-unit coverage help build build-ebpf test-ebpf lint-ebpf ci-ebpf clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
BINARY_NAME=tapio

# Build variables
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS = -X main.version=$(VERSION) \
          -X main.gitCommit=$(GIT_COMMIT) \
          -X main.buildDate=$(BUILD_DATE)

# Coverage settings
COVERAGE_FILE=coverage.out
COVERAGE_THRESHOLD=50

##@ Quality Gates

# FAIL FAST quality check
ci-check:
	@echo "🔍 Running quality checks..."
	@echo "Checking Go formatting..."
	@if [ "$$($(GOFMT) -s -l . | grep -v vendor | wc -l)" -gt 0 ]; then \
		echo "❌ Code not formatted. Run: make fmt"; \
		$(GOFMT) -s -l . | grep -v vendor; \
		exit 1; \
	fi
	@echo "✅ Formatting OK"
	
	@echo "Verifying modules..."
	@$(GOMOD) verify
	@echo "✅ Modules OK"
	
	@echo "Running basic linter..."
	@go vet ./...
	@echo "✅ Basic linting OK"
	
	@echo "🎉 Quality checks passed!"

# Comprehensive testing
ci-test:
	@echo "🧪 Running tests..."
	@$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@echo "✅ Tests passed!"

# Build verification  
ci-build:
	@echo "🔨 Building..."
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio
	@echo "✅ Build OK!"

##@ Complete Pipeline

# Full CI pipeline
ci: ci-check ci-test ci-build
	@echo "🚀 CI pipeline completed!"
	@echo "✅ Quality: PASSED"
	@echo "✅ Tests: PASSED"  
	@echo "✅ Build: PASSED"

##@ Development Tools

# Format code
fmt:
	@echo "🎨 Formatting..."
	@$(GOFMT) -s -w .

# Run linter
lint:
	@gofmt -l . | grep -v vendor | tee fmt-issues.txt && test ! -s fmt-issues.txt
	@go vet ./...

# Auto-fix linting issues
lint-fix:
	@echo "🔧 Auto-fixing linting issues..."
	@$(GOFMT) -s -w .

# Lint with eBPF build tags
lint-ebpf:
	@gofmt -l . | grep -v vendor | tee fmt-issues.txt && test ! -s fmt-issues.txt
	@go vet -tags ebpf ./...

# Quick development cycle
dev: fmt lint-fix ci-check test-unit build
	@echo "🚀 Dev cycle complete!"

##@ Testing

# Unit tests only
test-unit:
	@$(GOTEST) -short ./...

# Default tests without eBPF
test:
	@$(GOTEST) -short ./...

# Tests with eBPF support
test-ebpf:
	@$(GOTEST) -short -tags ebpf ./...

# Generate coverage report
coverage:
	@$(GOTEST) -coverprofile=$(COVERAGE_FILE) ./...
	@go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "📊 Coverage: coverage.html"

##@ Build

# Build binary
build:
	@mkdir -p bin
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio

# Build with eBPF support (Linux only)
build-ebpf:
	@mkdir -p bin
	@$(GOBUILD) -tags ebpf -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-ebpf ./cmd/tapio

# CI checks with eBPF
ci-ebpf: lint-ebpf build-ebpf test-ebpf
	@echo "[OK] CI checks with eBPF passed!"

# Clean artifacts
clean:
	@rm -rf bin/ dist/ *.out *.html fmt-issues.txt

##@ Legacy Support

# Run go vet
vet:
	@go vet ./...

##@ Help

# Show help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help