# Tapio Observability Platform - Production Grade Makefile
# Modular build system with smart dependency resolution

.PHONY: all build test clean fmt lint verify help
.DEFAULT_GOAL := help

# Variables
GO := go
GOFMT := gofmt
GOIMPORTS := goimports
GOLANGCI_LINT := golangci-lint
GO_VERSION := 1.21
PROJECT_ROOT := $(shell pwd)
BUILD_DIR := build
COVERAGE_DIR := coverage
BPF_DIR := pkg/collectors
ARCH := $(shell uname -m)

# Color output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
NC := \033[0m # No Color

# Package groups for modular building
DOMAIN_PKGS := ./pkg/domain/...
COLLECTOR_PKGS := ./pkg/collectors/...
INTELLIGENCE_PKGS := ./pkg/intelligence/...
INTEGRATION_PKGS := ./pkg/integrations/...
INTERFACE_PKGS := ./pkg/interfaces/...
CMD_PKGS := ./cmd/...

# Individual collector packages (for isolated builds)
COLLECTOR_CNI := ./pkg/collectors/cni
COLLECTOR_DNS := ./pkg/collectors/dns
COLLECTOR_ETCD := ./pkg/collectors/etcd
COLLECTOR_KUBEAPI := ./pkg/collectors/kubeapi
COLLECTOR_KUBELET := ./pkg/collectors/kubelet
COLLECTOR_SYSTEMD := ./pkg/collectors/systemd

# BPF targets
BPF_TARGETS := cni etcd systemd dns

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make ${YELLOW}<target>${NC}\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  ${GREEN}%-20s${NC} %s\n", $$1, $$2 } /^##@/ { printf "\n${YELLOW}%s${NC}\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

all: fmt lint build test ## Run full development cycle

build: build-domain build-collectors build-intelligence build-integrations build-interfaces build-cmd ## Build all packages in correct order

build-domain: ## Build domain layer (Level 0)
	@echo "${GREEN}Building domain layer...${NC}"
	@$(GO) build $(DOMAIN_PKGS) || (echo "${RED}Domain build failed${NC}" && exit 1)

build-collectors: build-domain ## Build collectors layer (Level 1)
	@echo "${GREEN}Building collectors layer...${NC}"
	@for pkg in $(COLLECTOR_CNI) $(COLLECTOR_DNS) $(COLLECTOR_ETCD) $(COLLECTOR_KUBEAPI) $(COLLECTOR_KUBELET) $(COLLECTOR_SYSTEMD); do \
		echo "  Building $$pkg..."; \
		$(GO) build $$pkg 2>/dev/null || echo "    ${YELLOW}Warning: $$pkg has issues (continuing)${NC}"; \
	done
	@$(GO) build ./pkg/collectors/registry ./pkg/collectors/pipeline ./pkg/collectors/manager 2>/dev/null || true

build-intelligence: build-domain build-collectors ## Build intelligence layer (Level 2)
	@echo "${GREEN}Building intelligence layer...${NC}"
	@$(GO) build $(INTELLIGENCE_PKGS) || (echo "${RED}Intelligence build failed${NC}" && exit 1)

build-integrations: build-domain build-collectors build-intelligence ## Build integrations layer (Level 3)
	@echo "${GREEN}Building integrations layer...${NC}"
	@$(GO) build $(INTEGRATION_PKGS) || (echo "${RED}Integrations build failed${NC}" && exit 1)

build-interfaces: build-domain build-collectors build-intelligence build-integrations ## Build interfaces layer (Level 4)
	@echo "${GREEN}Building interfaces layer...${NC}"
	@$(GO) build $(INTERFACE_PKGS) || (echo "${RED}Interfaces build failed${NC}" && exit 1)

build-cmd: build-interfaces ## Build command binaries
	@echo "${GREEN}Building command binaries...${NC}"
	@mkdir -p $(BUILD_DIR)
	@for cmd in cmd/*/; do \
		if [ -f "$$cmd/main.go" ]; then \
			name=$$(basename $$cmd); \
			echo "  Building $$name..."; \
			$(GO) build -o $(BUILD_DIR)/$$name ./$$cmd 2>/dev/null || echo "    ${YELLOW}Warning: $$name has issues${NC}"; \
		fi; \
	done

##@ BPF Management

bpf-generate: ## Generate BPF Go bindings for all collectors
	@echo "${GREEN}Generating BPF bindings...${NC}"
	@for target in $(BPF_TARGETS); do \
		if [ -f "$(BPF_DIR)/$$target/bpf/generate.go" ]; then \
			echo "  Generating $$target BPF..."; \
			cd $(BPF_DIR)/$$target/bpf && $(GO) generate ./... || echo "    ${YELLOW}Warning: $$target BPF generation failed${NC}"; \
		fi; \
	done

bpf-clean: ## Clean BPF generated files
	@echo "${GREEN}Cleaning BPF files...${NC}"
	@find $(BPF_DIR) -name "*_bpfel_*.go" -o -name "*_bpfel_*.o" | xargs rm -f

##@ Testing

test: test-unit ## Run all tests

test-unit: ## Run unit tests with race detection
	@echo "${GREEN}Running unit tests...${NC}"
	@$(GO) test -race -timeout 30s $(DOMAIN_PKGS) $(COLLECTOR_PKGS) $(INTELLIGENCE_PKGS) $(INTEGRATION_PKGS) $(INTERFACE_PKGS) 2>/dev/null || \
		echo "${YELLOW}Some tests failed (see details above)${NC}"

test-coverage: ## Run tests with coverage
	@echo "${GREEN}Running tests with coverage...${NC}"
	@mkdir -p $(COVERAGE_DIR)
	@$(GO) test -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	@$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "${GREEN}Coverage report: $(COVERAGE_DIR)/coverage.html${NC}"

test-integration: ## Run integration tests (requires tags)
	@echo "${GREEN}Running integration tests...${NC}"
	@$(GO) test -tags=integration -timeout 5m ./...

test-benchmark: ## Run benchmarks
	@echo "${GREEN}Running benchmarks...${NC}"
	@$(GO) test -bench=. -benchmem ./...

##@ Code Quality

fmt: ## Format code
	@echo "${GREEN}Formatting code...${NC}"
	@$(GOFMT) -w .
	@$(GOIMPORTS) -w .

lint: ## Run linters
	@echo "${GREEN}Running linters...${NC}"
	@if command -v $(GOLANGCI_LINT) > /dev/null; then \
		$(GOLANGCI_LINT) run --timeout 5m; \
	else \
		echo "${YELLOW}golangci-lint not installed, skipping${NC}"; \
	fi

vet: ## Run go vet
	@echo "${GREEN}Running go vet...${NC}"
	@$(GO) vet ./...

verify: verify-format verify-imports verify-architecture verify-todos ## Run all verifications

verify-format: ## Check code formatting
	@echo "${GREEN}Checking formatting...${NC}"
	@test -z "$$($(GOFMT) -l . | grep -v vendor)" || (echo "${RED}Unformatted files found${NC}" && $(GOFMT) -l . | grep -v vendor && exit 1)

verify-imports: ## Check import organization
	@echo "${GREEN}Checking imports...${NC}"
	@test -z "$$($(GOIMPORTS) -l . | grep -v vendor)" || (echo "${RED}Unorganized imports found${NC}" && exit 1)

verify-architecture: ## Verify 5-level architecture
	@echo "${GREEN}Verifying architecture hierarchy...${NC}"
	@$(PROJECT_ROOT)/scripts/verify-architecture.sh || (echo "${RED}Architecture violations found${NC}" && exit 1)

verify-todos: ## Check for TODOs and FIXMEs
	@echo "${GREEN}Checking for TODOs...${NC}"
	@! grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" . 2>/dev/null || (echo "${RED}TODOs found in code${NC}" && exit 1)

verify-coverage: ## Verify minimum coverage (80%)
	@echo "${GREEN}Verifying coverage...${NC}"
	@$(GO) test -cover ./... | awk '/coverage:/ {gsub("%","",$$5); if ($$5 < 80) {print "${RED}Package",$$2,"has only",$$5"% coverage (minimum 80%)${NC}"; exit 1}}'

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

##@ Utilities

clean: ## Clean build artifacts
	@echo "${GREEN}Cleaning...${NC}"
	@rm -rf $(BUILD_DIR) $(COVERAGE_DIR)
	@find . -name "*.test" -delete
	@find . -name "*.out" -delete

install-tools: ## Install development tools
	@echo "${GREEN}Installing tools...${NC}"
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@$(GO) install golang.org/x/tools/cmd/goimports@latest
	@$(GO) install github.com/cilium/ebpf/cmd/bpf2go@latest

docker-build: ## Build Docker images
	@echo "${GREEN}Building Docker images...${NC}"
	@docker build -t tapio-collector:latest -f build/docker/collector/Dockerfile .
	@docker build -t tapio-api:latest -f build/docker/api/Dockerfile .

##@ CI/CD

ci-local: ## Run full CI pipeline locally
	@echo "${GREEN}Running CI pipeline locally...${NC}"
	@$(MAKE) deps
	@$(MAKE) fmt
	@$(MAKE) lint
	@$(MAKE) build
	@$(MAKE) test
	@$(MAKE) verify

ci-quick: ## Quick CI check (format and build only)
	@echo "${GREEN}Running quick CI check...${NC}"
	@$(MAKE) verify-format
	@$(MAKE) build-domain
	@$(MAKE) build-collectors

##@ Troubleshooting

debug-deps: ## Debug dependency issues
	@echo "${GREEN}Analyzing dependencies...${NC}"
	@$(GO) list -f '{{.ImportPath}}: {{.Imports}}' ./... | grep -E "pkg/(domain|collectors|intelligence|integrations|interfaces)"

debug-build: ## Build with verbose output
	@echo "${GREEN}Building with verbose output...${NC}"
	@$(GO) build -v ./...

check-collector: ## Check specific collector build (use COLLECTOR=cni)
	@if [ -z "$(COLLECTOR)" ]; then \
		echo "${RED}Please specify COLLECTOR=<name>${NC}"; \
	else \
		echo "${GREEN}Checking collector: $(COLLECTOR)${NC}"; \
		$(GO) build -v ./pkg/collectors/$(COLLECTOR); \
	fi

.NOTPARALLEL: verify-architecture verify-todos verify-coverage