#!/bin/bash

echo "=== Fixing Tapio build system ==="
echo ""

# Change to project directory
cd /Users/yair/projects/tapio

echo "1. Cleaning up module cache..."
go clean -modcache 2>/dev/null || true

echo ""
echo "2. Running go mod tidy..."
go mod tidy

echo ""
echo "3. Downloading dependencies..."
go mod download

echo ""
echo "4. Verifying module integrity..."
go mod verify

echo ""
echo "5. Testing basic build..."
go build ./cmd/tapio/main.go

echo ""
echo "6. Running basic tests..."
go test ./pkg/collectors/platform.go ./pkg/collectors/platform_test.go

echo ""
echo "=== Build system fix complete ==="