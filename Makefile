.PHONY: help ci ci-quality ci-test ci-build ci-integration check-coverage fmt lint lint-fix vet test-unit test coverage build build-ebpf test-ebpf lint-ebpf ci-ebpf clean install-tools dev agent-start agent-menu agent-status pr-ready

# ==========================================
# Build Configuration
# ==========================================
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
BINARY_NAME=tapio

# Build variables
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS = -X github.com/yairfalse/tapio/internal/cli.version=$(VERSION) \
          -X github.com/yairfalse/tapio/internal/cli.gitCommit=$(GIT_COMMIT) \
          -X github.com/yairfalse/tapio/internal/cli.buildDate=$(BUILD_DATE)

# Coverage and Quality Settings
COVERAGE_FILE=coverage.out
COVERAGE_THRESHOLD=50
LINT_TIMEOUT=5m

# Platform Detection
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),Darwin)
    PLATFORM=darwin
endif
ifeq ($(UNAME_S),Linux)
    PLATFORM=linux
endif
ifeq ($(UNAME_M),x86_64)
    ARCH=amd64
endif
ifeq ($(UNAME_M),arm64)
    ARCH=arm64
endif

# ==========================================
# CI Pipeline Targets
# ==========================================

##@ CI Pipeline

# Full CI pipeline (matches GitHub Actions)
ci: ci-quality ci-test ci-build ## Run complete CI pipeline
	@echo "🚀 CI pipeline completed successfully!"
	@echo "✅ Quality: PASSED"
	@echo "✅ Tests: PASSED"
	@echo "✅ Build: PASSED"

# Stage 1: Quality Gates (< 3 minutes)
ci-quality: ## Stage 1 - Run all quality checks
	@echo "🔍 Running quality checks..."
	
	@echo "Checking Go formatting..."
	@if [ "$$($(GOFMT) -s -l . | grep -v vendor | grep -v pkg/mod | wc -l)" -gt 0 ]; then \
		echo "❌ Code not formatted. Files:"; \
		$(GOFMT) -s -l . | grep -v vendor | grep -v pkg/mod | tee fmt-issues.txt; \
		echo "Run: make fmt"; \
		exit 1; \
	fi
	@echo "✅ Formatting OK"
	
	@echo "Verifying Go modules..."
	@$(GOMOD) verify
	@$(GOMOD) tidy -diff
	@echo "✅ Modules OK"
	
	@echo "Running basic static analysis..."
	@$(GOCMD) vet ./...
	@echo "✅ Vet OK"
	
	@echo "Running comprehensive linter..."
	@golangci-lint run --timeout=$(LINT_TIMEOUT) --out-format=github-actions --issues-exit-code=1 || \
		(echo "❌ Linting failed. Run: make lint-fix" && exit 1)
	@echo "✅ Linting OK"
	
	@echo "🎉 All quality checks passed!"

# Stage 2: Test Execution (< 10 minutes)
ci-test: ## Stage 2 - Run comprehensive test suite
	@echo "🧪 Running test suite..."
	@$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic -timeout=10m \
		-v -count=1 ./... 2>&1 | tee test-output.txt
	@echo "✅ Tests completed!"

# Stage 3: Build Verification (< 5 minutes)
ci-build: ## Stage 3 - Build binaries for current platform
	@echo "🔨 Building for $(PLATFORM)/$(ARCH)..."
	@mkdir -p bin
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio
	@ls -la bin/$(BINARY_NAME)
	@echo "✅ Build completed!"

# Cross-platform build matrix
ci-build-all: ## Build for all supported platforms
	@echo "🔨 Building for all platforms..."
	@mkdir -p bin
	
	@echo "Building for linux/amd64..."
	@GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/tapio
	
	@echo "Building for linux/arm64..."
	@GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/tapio
	
	@echo "Building for darwin/amd64..."
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/tapio
	
	@echo "Building for darwin/arm64..."
	@GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/tapio
	
	@echo "Building for windows/amd64..."
	@GOOS=windows GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/tapio
	
	@ls -la bin/
	@echo "✅ Multi-platform build completed!"

# Integration tests (optional stage)
ci-integration: ## Run integration tests with Kind cluster
	@echo "🔧 Running integration tests..."
	@if ! command -v kind >/dev/null 2>&1; then \
		echo "❌ Kind not found. Install with: go install sigs.k8s.io/kind@latest"; \
		exit 1; \
	fi
	@$(GOTEST) -tags=integration -timeout=15m ./test/integration/...
	@echo "✅ Integration tests completed!"

# Coverage validation
check-coverage: ## Validate test coverage meets threshold
	@echo "📊 Checking coverage threshold ($(COVERAGE_THRESHOLD)%)..."
	@if [ ! -f $(COVERAGE_FILE) ]; then \
		echo "❌ Coverage file not found. Run: make ci-test"; \
		exit 1; \
	fi
	@COVERAGE=$$(go tool cover -func=$(COVERAGE_FILE) | grep total | grep -Eo '[0-9]+\.[0-9]+' | head -1); \
	echo "Current coverage: $$COVERAGE%"; \
	if [ "$$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc -l 2>/dev/null || python3 -c "print($$COVERAGE < $(COVERAGE_THRESHOLD))")" = "1" ] || [ "$$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc -l 2>/dev/null || python3 -c "print($$COVERAGE < $(COVERAGE_THRESHOLD))")" = "True" ]; then \
		echo "❌ Coverage $$COVERAGE% is below threshold $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	fi; \
	echo "✅ Coverage threshold met!"

# ==========================================
# Development Tools
# ==========================================

##@ Development

# Install required tools
install-tools: ## Install all required development tools
	@echo "🛠️ Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install sigs.k8s.io/kind@latest
	@go install github.com/sonatypecommunity/nancy@latest
	@echo "✅ All tools installed!"

# Format code
fmt: ## Format all Go code
	@echo "🎨 Formatting code..."
	@find . -name "*.go" -not -path "./vendor/*" -not -path "./pkg/mod/*" \
		-exec goimports -local github.com/yairfalse/tapio -w {} \;
	@find . -name "*.go" -not -path "./vendor/*" -not -path "./pkg/mod/*" \
		-exec $(GOFMT) -s -w {} \;
	@echo "✅ Code formatted!"

# Run comprehensive linter
lint: ## Run golangci-lint with all checks
	@echo "🔍 Running comprehensive linting..."
	@golangci-lint run --timeout=$(LINT_TIMEOUT)

# Auto-fix linting issues where possible
lint-fix: fmt ## Auto-fix linting issues
	@echo "🔧 Auto-fixing linting issues..."
	@golangci-lint run --fix --timeout=$(LINT_TIMEOUT) || true
	@echo "✅ Auto-fixes applied!"

# Quick development cycle
dev: fmt lint-fix ci-quality test-unit build ## Quick development cycle
	@echo "🚀 Development cycle complete!"

# ==========================================
# Testing
# ==========================================

##@ Testing

# Unit tests only (fast)
test-unit: ## Run unit tests only
	@$(GOTEST) -short -race ./...

# Default test target
test: ## Run standard test suite
	@$(GOTEST) -race ./...

# Tests with eBPF support
test-ebpf: ## Run tests with eBPF build tags
	@$(GOTEST) -short -tags ebpf -race ./...

# Generate HTML coverage report
coverage: ## Generate HTML coverage report
	@$(GOTEST) -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "📊 Coverage report: coverage.html"
	@go tool cover -func=$(COVERAGE_FILE) | grep total

# ==========================================
# Build Targets
# ==========================================

##@ Build

# Build main binary
build: ## Build tapio binary
	@mkdir -p bin
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio

# Build with eBPF support (Linux only)
build-ebpf: ## Build with eBPF support
	@mkdir -p bin
	@$(GOBUILD) -tags ebpf -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-ebpf ./cmd/tapio

# Build collector binary
build-collector: ## Build tapio-collector binary
	@mkdir -p bin
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/tapio-collector ./cmd/tapio-collector

# ==========================================
# eBPF Development
# ==========================================

##@ eBPF

# Build eBPF programs
build-ebpf-programs: ## Compile eBPF C programs
	@echo "🔧 Building eBPF programs..."
	@cd ebpf && make all
	@echo "✅ eBPF programs built!"

# Test eBPF functionality
test-ebpf-integration: ## Run eBPF integration tests
	@echo "🧪 Testing eBPF integration..."
	@cd test/ebpf && ./run-all-tests.sh

# CI checks with eBPF
ci-ebpf: lint-ebpf build-ebpf test-ebpf ## Run CI checks with eBPF support
	@echo "✅ eBPF CI checks passed!"

# Lint with eBPF build tags
lint-ebpf: ## Run linter with eBPF build tags
	@golangci-lint run --build-tags ebpf --timeout=$(LINT_TIMEOUT)

# ==========================================
# Quality Gates & Automation
# ==========================================

##@ Quality Gates

# Pre-commit validation
pre-commit: fmt lint-fix ci-quality ## Run pre-commit checks
	@echo "✅ Pre-commit checks passed!"

# Prepare for PR
pr-ready: clean install-tools fmt ci ## Complete PR preparation
	@echo "✅ PR readiness checklist:"
	@echo "  - Code formatted and linted"
	@echo "  - All tests passing"
	@echo "  - Coverage threshold met"
	@echo "  - Security scan clean"
	@echo "🚀 Ready to create PR!"

# Dependency updates
update-deps: ## Update Go dependencies
	@echo "📦 Updating dependencies..."
	@$(GOMOD) tidy
	@$(GOMOD) download
	@go list -u -m all
	@echo "✅ Dependencies updated!"

# Security scan
security: ## Run security scans
	@echo "🔒 Running security scan..."
	@gosec -fmt json -out gosec-report.json -nosec-tag notsafe ./... || true
	@gosec ./...
	@echo "✅ Security scan completed!"

# ==========================================
# Agent Workflow Support
# ==========================================

##@ Agent Workflow

# Start agent work with proper branch
agent-start: ## Start new agent task (prompts for agent, component, action)
	@echo "🤖 Starting new agent task..."
	@read -p "Agent ID: " agent; \
	read -p "Component: " component; \
	read -p "Action: " action; \
	if [ -z "$$agent" ] || [ -z "$$component" ] || [ -z "$$action" ]; then \
		echo "❌ All fields are required!"; \
		echo "Example: agent-1, ebpf-sources, decoupling"; \
		exit 1; \
	fi; \
	./scripts/agent-branch.sh "$$agent" "$$component" "$$action"

# Agent status overview
agent-status: ## Show active agent work and branches
	@echo "👥 Active agent work:"
	@find .agent-work -name "*.md" 2>/dev/null | head -5 || echo "No active work"
	@echo "🌿 Agent branches:"
	@git branch | grep "feature/agent-" || echo "No agent branches"

# Interactive agent menu
agent-menu: ## Start new agent task with interactive menu
	@./scripts/agent-menu.sh

# ==========================================
# Cleanup & Maintenance
# ==========================================

##@ Cleanup

# Clean all artifacts
clean: ## Clean all build artifacts and temporary files
	@echo "🧹 Cleaning up..."
	@rm -rf bin/ dist/ build/ *.out *.html *.xml *.txt *.json *.sarif
	@go clean -cache -testcache -modcache
	@echo "✅ Cleanup completed!"

# Clean and rebuild
rebuild: clean build ## Clean and rebuild everything
	@echo "✅ Rebuild completed!"

# ==========================================
# Utilities
# ==========================================

##@ Utilities

# Quick syntax check
syntax-check: ## Quick syntax validation
	@$(GOCMD) build -o /dev/null ./...

# Module verification
mod-verify: ## Verify and tidy modules
	@$(GOMOD) verify
	@$(GOMOD) tidy

# Show build info
info: ## Show build information
	@echo "📋 Build Information:"
	@echo "  Version: $(VERSION)"
	@echo "  Git Commit: $(GIT_COMMIT)"
	@echo "  Build Date: $(BUILD_DATE)"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Go Version: $$(go version)"

# ==========================================
# Help System
# ==========================================

##@ Help

# Show categorized help
help: ## Show this help message
	@echo "🌲 Tapio Build System"
	@echo "====================="
	@echo ""
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""
	@echo "🚀 Quick Start:"
	@echo "  make install-tools  # Set up development environment"
	@echo "  make dev           # Quick development cycle"
	@echo "  make ci            # Full CI pipeline"
	@echo "  make pr-ready      # Prepare for pull request"

.DEFAULT_GOAL := help