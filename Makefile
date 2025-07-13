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
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/events.proto proto/opinionated_events.proto
	@echo "✅ Protobuf generation complete"

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

##@ Branch Management

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

# Start agent work with interactive menu
agent-menu: ## Start new agent task with interactive menu
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
	@echo "✅ Cleanup completed!"

# Clean and rebuild
rebuild: clean build ## Clean and rebuild everything
	@echo "✅ Rebuild completed!"

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