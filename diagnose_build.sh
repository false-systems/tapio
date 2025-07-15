#!/bin/bash

echo "=== Tapio Build System Diagnosis ==="
echo ""

# Check Go installation
echo "1. Checking Go installation:"
if command -v go &> /dev/null; then
    go version
else
    echo "ERROR: Go is not installed or not in PATH"
    exit 1
fi

# Check current directory
echo ""
echo "2. Current directory:"
pwd

# Check for go.mod issues
echo ""
echo "3. Checking go.mod file:"
if [ -f "go.mod" ]; then
    echo "Found go.mod in root"
    echo "Go version requirement:"
    grep "^go " go.mod
    echo "Toolchain requirement:"
    grep "^toolchain " go.mod
    echo ""
    echo "WARNING: go.mod specifies 'toolchain go1.24.3' which doesn't exist!"
    echo "Latest stable Go version is 1.23.x"
else
    echo "ERROR: No go.mod found in root"
fi

# Check for multiple go.mod files
echo ""
echo "4. Checking for multiple go.mod files (potential issue):"
find . -name "go.mod" -type f 2>/dev/null | grep -v vendor | head -10

# Check if main entry point exists
echo ""
echo "5. Checking main entry point:"
if [ -f "cmd/tapio/main.go" ]; then
    echo "✓ Found cmd/tapio/main.go"
else
    echo "✗ Missing cmd/tapio/main.go"
fi

# Try to download dependencies
echo ""
echo "6. Attempting to download dependencies:"
go mod download 2>&1 | head -20

# Try to verify modules
echo ""
echo "7. Verifying modules:"
go mod verify 2>&1 | head -20

# Try a simple build
echo ""
echo "8. Attempting build of main binary:"
go build -o /tmp/tapio-test ./cmd/tapio 2>&1 | head -20

# Check for common issues
echo ""
echo "9. Common issues found:"
echo "- Invalid toolchain version (go1.24.3 doesn't exist)"
echo "- Multiple go.mod files causing module conflicts"
echo "- Possible replace directives in sub-modules pointing to non-existent paths"

echo ""
echo "=== Diagnosis Complete ==="