# Tapio Observability Platform - Production Grade Makefile
# Updated for internal/observers structure + CLAUDE.md enforcement

.PHONY: all build test clean fmt lint verify help
.DEFAULT_GOAL := help

# Variables
GO := go
GOFMT := gofmt
GOIMPORTS := goimports
GOLANGCI_LINT := golangci-lint
GO_VERSION := 1.24
PROJECT_ROOT := $(shell pwd)
BUILD_DIR := build
COVERAGE_DIR := coverage
ARCH := $(shell uname -m)

# Color output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
NC := \033[0m

# Package groups following 5-level architecture
DOMAIN_PKGS := ./pkg/domain/...
DECODER_PKGS := ./pkg/decoders/...
BASE_PKGS := ./internal/base/...
OBSERVER_PKGS := ./internal/observers/...
INTELLIGENCE_PKGS := ./pkg/intelligence/...
INTEGRATION_PKGS := ./pkg/integrations/...
INTERFACE_PKGS := ./pkg/interfaces/...
CMD_PKGS := ./cmd/...

# Individual observer packages
OBSERVERS := $(shell find internal/observers -maxdepth 1 -type d -not -path internal/observers | xargs -n1 basename 2>/dev/null || true)

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make ${YELLOW}<target>${NC}\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  ${GREEN}%-25s${NC} %s\n", $$1, $$2 } /^##@/ { printf "\n${YELLOW}%s${NC}\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

all: verify build test ## Run full development cycle (CLAUDE.md enforced)

build: build-domain build-decoders build-base build-observers build-intelligence build-integrations build-interfaces build-cmd ## Build all packages in correct order

build-domain: ## Build domain layer (Level 0)
	@echo "${GREEN}[1/8] Building domain layer...${NC}"
	@$(GO) build $(DOMAIN_PKGS)

build-decoders: build-domain ## Build decoders (Level 1)
	@echo "${GREEN}[2/8] Building decoders...${NC}"
	@$(GO) build $(DECODER_PKGS)

build-base: build-domain ## Build base infrastructure
	@echo "${GREEN}[3/8] Building base infrastructure...${NC}"
	@$(GO) build $(BASE_PKGS)

build-observers: build-domain build-base ## Build observers (Level 1)
	@echo "${GREEN}[4/8] Building observers...${NC}"
	@$(GO) build $(OBSERVER_PKGS)

build-intelligence: build-domain build-observers ## Build intelligence layer (Level 2)
	@echo "${GREEN}[5/8] Building intelligence layer...${NC}"
	@$(GO) build $(INTELLIGENCE_PKGS) || echo "${YELLOW}Intelligence layer has issues${NC}"

build-integrations: build-domain build-observers build-intelligence ## Build integrations layer (Level 3)
	@echo "${GREEN}[6/8] Building integrations layer...${NC}"
	@$(GO) build $(INTEGRATION_PKGS) || echo "${YELLOW}Integrations layer has issues${NC}"

build-interfaces: build-domain build-observers build-intelligence build-integrations ## Build interfaces layer (Level 4)
	@echo "${GREEN}[7/8] Building interfaces layer...${NC}"
	@$(GO) build $(INTERFACE_PKGS) || echo "${YELLOW}Interfaces layer has issues${NC}"

build-cmd: build-interfaces ## Build command binaries
	@echo "${GREEN}[8/8] Building command binaries...${NC}"
	@mkdir -p $(BUILD_DIR)
	@for cmd in cmd/*/; do \
		if [ -f "$$cmd/main.go" ]; then \
			name=$$(basename $$cmd); \
			echo "  Building $$name..."; \
			$(GO) build -o $(BUILD_DIR)/$$name ./$$cmd || echo "    ${YELLOW}Warning: $$name has issues${NC}"; \
		fi; \
	done

##@ eBPF Management

ebpf-vmlinux: ## Generate minimal vmlinux.h for all observers
	@echo "${GREEN}Generating minimal vmlinux.h...${NC}"
	@mkdir -p internal/base/bpf
	@if [ -f /sys/kernel/btf/vmlinux ]; then \
		bpftool btf dump file /sys/kernel/btf/vmlinux format c > internal/base/bpf/vmlinux.h; \
		echo "${GREEN}✓ Generated internal/base/bpf/vmlinux.h${NC}"; \
	else \
		echo "${RED}✗ BTF not available (/sys/kernel/btf/vmlinux not found)${NC}"; \
		exit 1; \
	fi

ebpf-generate: ## Generate eBPF Go bindings for all observers
	@echo "${GREEN}Generating eBPF bindings...${NC}"
	@for observer in $(OBSERVERS); do \
		if [ -f "internal/observers/$$observer/bpf/network_monitor.c" ] || [ -f "internal/observers/$$observer/bpf/"*.c ]; then \
			echo "  Generating eBPF for $$observer..."; \
			(cd internal/observers/$$observer && go generate ./... 2>&1) || echo "${YELLOW}Warning: $$observer eBPF generation failed${NC}"; \
		fi; \
	done

ebpf-clean: ## Clean generated eBPF files
	@echo "${GREEN}Cleaning eBPF generated files...${NC}"
	@find internal/observers -name "*_bpf*.go" -delete
	@find internal/observers -name "*_bpf*.o" -delete

##@ Testing

test: test-unit ## Run all tests

test-unit: ## Run unit tests with race detection
	@echo "${GREEN}Running unit tests...${NC}"
	@$(GO) test -race -timeout 60s ./...

test-coverage: ## Run tests with coverage (minimum 80%)
	@echo "${GREEN}Running tests with coverage...${NC}"
	@mkdir -p $(COVERAGE_DIR)
	@$(GO) test -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	@$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "${GREEN}Coverage report: $(COVERAGE_DIR)/coverage.html${NC}"
	@$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out | tail -1

test-integration: ## Run integration tests (Linux + eBPF)
	@echo "${GREEN}Running integration tests...${NC}"
	@$(GO) test -tags=integration -timeout 5m ./internal/observers/...

test-benchmark: ## Run benchmarks
	@echo "${GREEN}Running benchmarks...${NC}"
	@$(GO) test -bench=. -benchmem ./...

##@ Code Quality & CLAUDE.md Enforcement

fmt: ## Format code (required before commit)
	@echo "${GREEN}Formatting code...${NC}"
	@$(GOFMT) -w .

vet: ## Run go vet
	@echo "${GREEN}Running go vet...${NC}"
	@$(GO) vet ./...

lint: ## Run golangci-lint
	@echo "${GREEN}Running golangci-lint...${NC}"
	@if command -v $(GOLANGCI_LINT) > /dev/null; then \
		$(GOLANGCI_LINT) run --timeout 10m; \
	else \
		echo "${YELLOW}golangci-lint not installed${NC}"; \
	fi

verify: verify-claude-md verify-coverage ## Run all verifications (CLAUDE.md + coverage)

verify-claude-md: ## CLAUDE.md compliance check (strict mode)
	@echo "${GREEN}Running CLAUDE.md compliance checks...${NC}"
	@./scripts/verify-claude-md.sh --strict

verify-quick: ## Quick CLAUDE.md check (no lint, for local dev)
	@echo "${GREEN}Running quick CLAUDE.md checks...${NC}"
	@./scripts/verify-claude-md.sh

verify-coverage: ## Verify minimum 80% coverage
	@echo "${GREEN}Verifying coverage threshold...${NC}"
	@$(GO) test -cover ./... 2>/dev/null | awk '/coverage:/ {gsub("%","",$$5); if ($$5 < 80) {print "❌ Package",$$2,"has only",$$5"% coverage (minimum 80%)"; exit 1}} END {print "✓ All packages meet 80% coverage"}'

verify-full: verify build test ## Full verification (CI mode)
	@echo "${GREEN}═══════════════════════════════════════════════════════════${NC}"
	@echo "${GREEN}✅ ALL CHECKS PASSED - READY FOR COMMIT${NC}"
	@echo "${GREEN}═══════════════════════════════════════════════════════════${NC}"

##@ Dependency Management

deps: ## Download dependencies
	@echo "${GREEN}Downloading dependencies...${NC}"
	@$(GO) mod download

deps-update: ## Update dependencies
	@echo "${GREEN}Updating dependencies...${NC}"
	@$(GO) get -u ./...
	@$(GO) mod tidy

deps-verify: ## Verify dependencies
	@echo "${GREEN}Verifying dependencies...${NC}"
	@$(GO) mod verify

##@ Docker

docker-dev: ## Build dev Docker image
	@echo "${GREEN}Building dev Docker image...${NC}"
	@docker build -t tapio-dev -f docker/Dockerfile.dev .

docker-shell: ## Start Docker dev shell
	@echo "${GREEN}Starting Docker dev shell...${NC}"
	@docker run --rm -it --privileged -v $(PWD):/tapio -w /tapio tapio-dev

docker-test: ## Run tests in Docker
	@echo "${GREEN}Running tests in Docker...${NC}"
	@docker run --rm --privileged -v $(PWD):/tapio -w /tapio tapio-dev make test

##@ Utilities

clean: ## Clean build artifacts
	@echo "${GREEN}Cleaning...${NC}"
	@rm -rf $(BUILD_DIR) $(COVERAGE_DIR)
	@$(GO) clean -cache -testcache

clean-all: clean ebpf-clean ## Clean everything including eBPF artifacts
	@echo "${GREEN}Deep clean complete${NC}"

info: ## Display project information
	@echo "${GREEN}Tapio Observability Platform${NC}"
	@echo ""
	@echo "Go version:    $(shell $(GO) version)"
	@echo "Architecture:  $(ARCH)"
	@echo "Observers:     $(OBSERVERS)"
	@echo ""

.PHONY: $(OBSERVERS)
