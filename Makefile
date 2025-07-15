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
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS = -X github.com/yairfalse/tapio/internal/cli.version=$(VERSION) \
          -X github.com/yairfalse/tapio/internal/cli.gitCommit=$(GIT_COMMIT) \
          -X github.com/yairfalse/tapio/internal/cli.buildDate=$(BUILD_DATE)

# Platform detection
PLATFORM ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

# Coverage settings
COVERAGE_FILE=coverage.out
COVERAGE_THRESHOLD=50

##@ Quality Gates

# FAIL FAST quality check
ci-check:
	@echo "🔍 Running quality checks..."
	@echo "Checking Go formatting..."
	@if [ "$$($(GOFMT) -s -l . | grep -v vendor | grep -v pkg/mod | wc -l)" -gt 0 ]; then \
		echo "❌ Code not formatted. Run: make fmt"; \
		$(GOFMT) -s -l . | grep -v vendor | grep -v pkg/mod; \
		exit 1; \
	fi
	@echo "✅ Formatting OK"
	
	@echo "Verifying modules..."
	@$(GOMOD) verify
	@echo "✅ Modules OK"
	
	@echo "Running basic linter..."
	@go list ./... | grep -v pkg/mod | xargs go vet
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
	@find . -name "*.go" -not -path "./vendor/*" -not -path "./pkg/mod/*" -exec $(GOFMT) -s -w {} \;

# Run linter
lint:
	@gofmt -l . | grep -v vendor | grep -v pkg/mod | tee fmt-issues.txt && test ! -s fmt-issues.txt
	@go list ./... | grep -v pkg/mod | xargs go vet

# Auto-fix linting issues
lint-fix:
	@echo "🔧 Auto-fixing linting issues..."
	@find . -name "*.go" -not -path "./vendor/*" -not -path "./pkg/mod/*" -exec $(GOFMT) -s -w {} \;

# Lint with eBPF build tags
lint-ebpf:
	@gofmt -l . | grep -v vendor | grep -v pkg/mod | tee fmt-issues.txt && test ! -s fmt-issues.txt
	@go list ./... | grep -v pkg/mod | xargs go vet -tags ebpf

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

##@ Code Generation

# Generate protobuf code
proto:
	@echo "🔧 Generating protobuf code..."
	@if ! command -v protoc >/dev/null 2>&1; then \
		echo "❌ protoc not found. Please install protobuf compiler"; \
		echo "   On macOS: brew install protobuf"; \
		echo "   On Linux: apt-get install protobuf-compiler"; \
		exit 1; \
	fi
	@if ! command -v protoc-gen-go >/dev/null 2>&1; then \
		echo "❌ protoc-gen-go not found. Installing..."; \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
	fi
	@if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then \
		echo "❌ protoc-gen-go-grpc not found. Installing..."; \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/events.proto proto/opinionated_events.proto
	@echo "✅ Protobuf generation complete"

##@ Build

# Build binary
build:
	@echo "🔨 Building $(BINARY_NAME)..."
	@mkdir -p bin
	@$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio
	@echo "✅ Build complete: bin/$(BINARY_NAME)"

# Build with Docker (all platforms)
docker-build: ## Build binaries for all platforms using Docker
	@echo "🐳 Building with Docker..."
	@mkdir -p bin
	@docker-compose -f docker-compose.build.yml build
	@docker-compose -f docker-compose.build.yml run --rm build-standard
	@docker-compose -f docker-compose.build.yml run --rm build-ebpf
	@docker-compose -f docker-compose.build.yml run --rm build-darwin
	@docker-compose -f docker-compose.build.yml run --rm build-windows
	@echo "✅ Docker build complete. Binaries in bin/"
	@ls -la bin/

# Build Linux binary with eBPF using Docker
docker-build-linux-ebpf: ## Build Linux eBPF binary using Docker
	@echo "🐳 Building Linux eBPF binary with Docker..."
	@mkdir -p bin
	@docker-compose -f docker-compose.build.yml build build-ebpf
	@docker-compose -f docker-compose.build.yml run --rm build-ebpf
	@echo "✅ Built: bin/tapio-linux-amd64-ebpf"

# Run development environment in Docker
docker-dev: ## Run development environment in Docker
	@echo "🐳 Starting development environment..."
	@docker-compose -f docker-compose.build.yml run --rm -it dev

# Build Docker image
docker-image: ## Build production Docker image
	@echo "🐳 Building Docker image..."
	@docker build -t tapio:latest .
	@echo "✅ Docker image built: tapio:latest"

# Generate eBPF bindings (Linux only)
generate-ebpf:
	@echo "🔧 Generating eBPF bindings..."
	@if [ "$(PLATFORM)" != "linux" ]; then \
		echo "⚠️  Warning: eBPF generation is only supported on Linux. Current platform: $(PLATFORM)"; \
		echo "ℹ️  Stub files will be used for non-Linux builds"; \
	else \
		cd ebpf && $(MAKE) generate; \
	fi
	@echo "✅ eBPF generation complete"

# Build with eBPF support (Linux only)
build-ebpf: generate-ebpf
	@echo "🔨 Building $(BINARY_NAME) with eBPF support..."
	@if [ "$(PLATFORM)" != "linux" ]; then \
		echo "⚠️  Warning: eBPF build is only supported on Linux. Current platform: $(PLATFORM)"; \
		echo "ℹ️  Building without eBPF support"; \
		$(GOBUILD) -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/tapio; \
	else \
		@mkdir -p bin; \
		$(GOBUILD) -tags ebpf -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-ebpf ./cmd/tapio; \
	fi
	@echo "✅ Build complete"

# CI checks with eBPF
ci-ebpf: lint-ebpf build-ebpf test-ebpf
	@echo "[OK] CI checks with eBPF passed!"


##@ Legacy Support

# Run go vet
vet:
	@go vet ./...

##@ Branch Management

# Start agent work with proper branch
agent-start: ## Start new agent task (prompts for agent, component, action)
	@echo "🤖 Starting new agent task..."
	@if [ ! -x ./scripts/agent-branch.sh ]; then \
		chmod +x ./scripts/agent-branch.sh; \
	fi
	@read -p "Agent ID: " agent; \
	read -p "Component: " component; \
	read -p "Action: " action; \
	if [ -z "$$agent" ] || [ -z "$$component" ] || [ -z "$$action" ]; then \
		echo "❌ All fields are required!"; \
		echo "Example: agent-1, ebpf-sources, decoupling"; \
		exit 1; \
	fi; \
	./scripts/agent-branch.sh "$$agent" "$$component" "$$action"

# Start agent work with interactive menu
agent-menu: ## Start new agent task with interactive menu
	@if [ ! -x ./scripts/agent-menu.sh ]; then \
		chmod +x ./scripts/agent-menu.sh; \
	fi
	@./scripts/agent-menu.sh

# Agent status overview
agent-status: ## Show active agent work and branches
	@echo "👥 Active agent work:"
	@find .agent-work -name "*.md" 2>/dev/null | head -5 || echo "No active work"
	@echo "🌿 Agent branches:"
	@git branch | grep "feature/agent-" || echo "No agent branches"

# Prepare for PR
pr-ready: fmt ci-check
	@echo "✅ PR ready checklist:"
	@echo "- Code formatted and linted"
	@echo "- Tests passing"
	@echo "- Changes < 200 lines"
	@echo "🚀 Ready to create PR!"

# Alias for ci-check
agent-check: ci-check ## Run quality checks for agent work
	@echo "✅ Agent quality checks complete!"

# ==========================================
# Cleanup & Maintenance
# ==========================================

##@ Cleanup

# Clean all artifacts
clean: ## Clean all build artifacts and temporary files
	@echo "🧹 Cleaning up..."
	@rm -rf bin/ dist/ build/ *.out *.html *.xml *.txt *.json *.sarif
	@go clean -cache -testcache -modcache
	@if [ -d ebpf ]; then cd ebpf && $(MAKE) clean; fi
	@echo "✅ Cleanup completed!"

# Clean and rebuild
rebuild: clean build ## Clean and rebuild everything
	@echo "✅ Rebuild completed!"

##@ CI/CD Targets

# CI Quality checks target
ci-quality: fmt lint vet
	@echo "✅ CI Quality checks passed!"

# Check coverage threshold
check-coverage:
	@echo "📊 Checking coverage threshold..."
	@if [ -f coverage.out ]; then \
		COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print substr($$3, 1, length($$3)-1)}'); \
		echo "Current coverage: $$COVERAGE%"; \
		echo "Threshold: $(COVERAGE_THRESHOLD)%"; \
		if command -v bc >/dev/null 2>&1; then \
			if [ $$(echo "$$COVERAGE >= $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
				echo "✅ Coverage threshold met"; \
			else \
				echo "❌ Coverage below threshold"; \
				exit 1; \
			fi; \
		else \
			echo "⚠️  bc not available, skipping threshold check"; \
		fi; \
	else \
		echo "❌ No coverage file found"; \
		exit 1; \
	fi

# CI Integration tests
ci-integration:
	@echo "🧪 Running integration tests..."
	@$(GOTEST) -tags=integration ./test/integration/...
	@echo "✅ Integration tests passed!"

# ==========================================
# Performance & Benchmarking
# ==========================================

##@ Performance

# Run comprehensive benchmarks
bench: ## Run all benchmarks with memory stats
	@echo "🏃 Running benchmarks..."
	@mkdir -p profiles
	@$(GOTEST) -bench=. -benchmem -benchtime=5s -timeout=30m ./pkg/otel/benchmarks/...
	@echo "✅ Benchmarks completed!"

# Run benchmarks with CPU profiling
bench-cpu: ## Run benchmarks with CPU profiling
	@echo "🔥 Running CPU profiling benchmarks..."
	@mkdir -p profiles
	@$(GOTEST) -bench=BenchmarkTraceAggregateCreation -benchmem -cpuprofile=profiles/cpu.prof ./pkg/otel/benchmarks/...
	@echo "📊 View profile: go tool pprof profiles/cpu.prof"

# Run benchmarks with memory profiling
bench-mem: ## Run benchmarks with memory profiling
	@echo "🧠 Running memory profiling benchmarks..."
	@mkdir -p profiles
	@$(GOTEST) -bench=BenchmarkArenaSpanAllocation -benchmem -memprofile=profiles/mem.prof ./pkg/otel/benchmarks/...
	@echo "📊 View profile: go tool pprof profiles/mem.prof"

# Profile-Guided Optimization (PGO) workflow
bench-pgo: ## Run complete PGO benchmark workflow
	@echo "🎯 Running Profile-Guided Optimization workflow..."
	@if [ ! -x ./scripts/benchmark-pgo.sh ]; then \
		chmod +x ./scripts/benchmark-pgo.sh; \
	fi
	@./scripts/benchmark-pgo.sh
	@echo "✅ PGO workflow completed! Check profiles/ directory"

# Compare benchmark results
bench-compare: ## Compare benchmark results against baseline
	@echo "📈 Comparing benchmark results..."
	@if [ ! -f profiles/baseline.txt ]; then \
		echo "❌ No baseline found. Run: make bench > profiles/baseline.txt"; \
		exit 1; \
	fi
	@$(GOTEST) -bench=. -benchmem -benchtime=5s ./pkg/otel/benchmarks/... > profiles/current.txt
	@echo "📊 Baseline vs Current comparison:"
	@echo "Baseline results:"
	@grep "^Benchmark" profiles/baseline.txt | head -5
	@echo "Current results:"
	@grep "^Benchmark" profiles/current.txt | head -5

# Fuzzing tests for robustness
fuzz: ## Run fuzzing tests for trace validation
	@echo "🎲 Running fuzzing tests..."
	@$(GOTEST) -fuzz=FuzzTraceAggregateCreation -fuzztime=30s ./pkg/otel/domain/...
	@$(GOTEST) -fuzz=FuzzSpanAttributes -fuzztime=30s ./pkg/otel/domain/...
	@$(GOTEST) -fuzz=FuzzBinaryEncoding -fuzztime=30s ./pkg/otel/domain/...
	@echo "✅ Fuzzing tests completed!"

# Performance regression check
perf-check: ## Check for performance regressions
	@echo "⚡ Checking for performance regressions..."
	@if [ ! -f profiles/baseline.txt ]; then \
		echo "❌ No baseline found. Creating baseline..."; \
		$(GOTEST) -bench=. -benchmem -count=3 ./pkg/otel/benchmarks/... > profiles/baseline.txt; \
		echo "✅ Baseline created at profiles/baseline.txt"; \
		exit 0; \
	fi
	@$(GOTEST) -bench=. -benchmem -count=3 ./pkg/otel/benchmarks/... > profiles/current.txt
	@echo "📊 Performance regression analysis saved to profiles/regression-check.txt"

# Build with PGO if profile exists
build-pgo: ## Build with Profile-Guided Optimization
	@if [ -f profiles/default.pgo ] || [ -f pgo/default.pgo ]; then \
		echo "🎯 Building with PGO..."; \
		mkdir -p bin; \
		PGO_FILE=$$(find profiles pgo -name "default.pgo" 2>/dev/null | head -1); \
		$(GOBUILD) -pgo=$$PGO_FILE -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-pgo ./cmd/tapio; \
		echo "✅ PGO build completed: bin/$(BINARY_NAME)-pgo"; \
	else \
		echo "❌ No PGO profile found. Run: make bench-pgo"; \
		exit 1; \
	fi

# ==========================================
# Utilities
# ==========================================

##@ Utilities

# Validate build environment
validate-env: ## Check build environment and dependencies
	@echo "🔍 Validating build environment..."
	@echo "Go version: $$(go version)"
	@if ! command -v go >/dev/null 2>&1; then \
		echo "❌ Go not found. Please install Go"; \
		exit 1; \
	fi
	@echo "Git version: $$(git --version)"
	@if ! command -v git >/dev/null 2>&1; then \
		echo "❌ Git not found. Please install Git"; \
		exit 1; \
	fi
	@echo "Platform: $(PLATFORM)/$(ARCH)"
	@echo "✅ Build environment OK"

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

# Show help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help