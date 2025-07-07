.PHONY: build test lint clean install dev-setup

# Build variables
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -X github.com/falseyair/tapio/internal/cli.version=$(VERSION) \
          -X github.com/falseyair/tapio/internal/cli.gitCommit=$(GIT_COMMIT) \
          -X github.com/falseyair/tapio/internal/cli.buildDate=$(BUILD_DATE)

# Build
build:
	@echo "🔨 Building Tapio..."
	go build -ldflags "$(LDFLAGS)" -o bin/tapio ./cmd/tapio

build-all:
	@echo "🔨 Building for all platforms..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/tapio-linux-amd64 ./cmd/tapio
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/tapio-darwin-amd64 ./cmd/tapio
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/tapio-darwin-arm64 ./cmd/tapio
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/tapio-windows-amd64.exe ./cmd/tapio

# Testing
test:
	@echo "🧪 Running tests..."
	go test ./...

test-unit:
	@echo "🧪 Running unit tests..."
	go test -short ./test/unit/...

test-coverage:
	@echo "📊 Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Code quality
lint:
	@echo "🔍 Running linter..."
	golangci-lint run

lint-fix:
	@echo "🔧 Fixing linting issues..."
	golangci-lint run --fix

# Development
dev-setup:
	@echo "🛠️  Setting up development environment..."
	go mod download
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

clean:
	@echo "🧹 Cleaning up..."
	rm -rf bin/ dist/ coverage.out coverage.html

install: build
	@echo "📦 Installing Tapio..."
	cp bin/tapio /usr/local/bin/

# Quick development cycle
dev: clean build
	@echo "✅ Development build complete!"
	@echo "Try: ./bin/tapio check"

# CI targets
ci: lint test build
	@echo "✅ CI pipeline complete!"

# Remove old build artifacts and rebuild completely
rebuild: clean build