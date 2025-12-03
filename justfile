# Tapio Observability Platform - justfile
# Single source of truth for dev and CI commands

set shell := ["bash", "-uc"]

# Default recipe - show help
default:
    @just --list

# ============================================================================
# Core CI Pipeline (run locally before pushing)
# ============================================================================

# Run full CI pipeline: fmt, vet, lint, verify, test, build
ci: fmt-check vet lint verify test build
    @echo "✅ CI passed - ready to push"

# Quick check for local dev (no lint, faster)
check: fmt-check vet verify-quick test

# ============================================================================
# Formatting
# ============================================================================

# Format all Go code
fmt:
    @echo "Formatting code..."
    gofmt -w .

# Check formatting (CI mode - fails if unformatted)
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Checking format..."
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then
        echo "❌ Unformatted files:"
        echo "$unformatted"
        exit 1
    fi
    echo "✓ Format OK"

# ============================================================================
# Static Analysis
# ============================================================================

# Run go vet
vet:
    @echo "Running go vet..."
    go vet ./...
    @echo "✓ Vet OK"

# Run golangci-lint
lint:
    @echo "Running golangci-lint..."
    golangci-lint run --timeout 5m
    @echo "✓ Lint OK"

# ============================================================================
# Testing
# ============================================================================

# Run all tests with race detection
test:
    @echo "Running tests..."
    go test -race -timeout 5m ./...
    @echo "✓ Tests OK"

# Run tests with verbose output
test-v:
    go test -race -v -timeout 5m ./...

# Run specific test
test-one TEST:
    go test -race -v -run {{TEST}} ./...

# Run tests with coverage
test-coverage:
    @mkdir -p coverage
    go test -race -coverprofile=coverage/coverage.out -covermode=atomic ./...
    go tool cover -html=coverage/coverage.out -o coverage/coverage.html
    @echo "Coverage report: coverage/coverage.html"
    go tool cover -func=coverage/coverage.out | tail -1

# ============================================================================
# CLAUDE.md Verification
# ============================================================================

# Full CLAUDE.md compliance check (with lint)
verify:
    @echo "Running CLAUDE.md compliance checks..."
    ./scripts/verify-claude-md.sh --strict

# Quick CLAUDE.md check (no lint, for local dev)
verify-quick:
    @echo "Running quick CLAUDE.md checks..."
    ./scripts/verify-claude-md.sh

# ============================================================================
# Building
# ============================================================================

# Build all binaries
build:
    @echo "Building binaries..."
    @mkdir -p bin
    CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/tapio ./cmd/tapio
    CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/operator ./cmd/operator
    @echo "✓ Build OK"

# Build with debug symbols
build-debug:
    @mkdir -p bin
    go build -o bin/tapio ./cmd/tapio
    go build -o bin/operator ./cmd/operator

# ============================================================================
# eBPF Management
# ============================================================================

# Generate eBPF code (requires Linux or Docker)
ebpf-generate:
    @echo "Generating eBPF bindings..."
    @if [ "$(uname)" = "Linux" ]; then \
        cd internal/observers/network/bpf && go generate; \
        cd ../../node/bpf && go generate; \
    else \
        echo "Not on Linux - use 'just docker-ebpf' instead"; \
        exit 1; \
    fi

# Generate eBPF via Docker (for Mac)
docker-ebpf:
    @echo "Generating eBPF in Docker..."
    docker run --rm -v $(pwd):/tapio -w /tapio tapio-dev \
        bash -c "cd internal/observers/network/bpf && go generate && cd ../../node/bpf && go generate"
    @echo "✓ eBPF generated"

# Clean generated eBPF files
ebpf-clean:
    find internal/observers -name "*_bpf*.go" -delete
    find internal/observers -name "*_bpf*.o" -delete

# ============================================================================
# Docker
# ============================================================================

# Build dev Docker image
docker-build:
    docker build -t tapio-dev -f docker/Dockerfile.dev .

# Start Docker dev shell
docker-shell:
    docker run --rm -it --privileged -v $(pwd):/tapio -w /tapio tapio-dev

# Run tests in Docker
docker-test:
    docker run --rm --privileged -v $(pwd):/tapio -w /tapio tapio-dev just test

# Run full CI in Docker
docker-ci:
    docker run --rm --privileged -v $(pwd):/tapio -w /tapio tapio-dev just ci

# ============================================================================
# Dependencies
# ============================================================================

# Download dependencies
deps:
    go mod download

# Update dependencies
deps-update:
    go get -u ./...
    go mod tidy

# Verify dependencies
deps-verify:
    go mod verify

# ============================================================================
# Utilities
# ============================================================================

# Clean build artifacts
clean:
    rm -rf bin coverage
    go clean -cache -testcache

# Show project info
info:
    @echo "Tapio Observability Platform"
    @echo ""
    @echo "Go version:   $(go version)"
    @echo "Architecture: $(uname -m)"
    @echo ""

# Show tool versions (useful for CI debugging)
versions:
    @echo "Go:           $(go version | cut -d' ' -f3)"
    @echo "golangci-lint: $(golangci-lint --version 2>/dev/null | head -1 || echo 'not installed')"
    @echo "just:         $(just --version)"
