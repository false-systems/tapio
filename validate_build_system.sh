#!/bin/bash
set -e

echo "🔍 Validating Tapio Build System..."
echo "=================================="

# Test 1: Go Module Validation
echo "1. Testing Go module integrity..."
if go mod verify; then
    echo "✅ Go modules verified successfully"
else
    echo "❌ Go module verification failed"
    exit 1
fi

# Test 2: Go Module Tidy
echo ""
echo "2. Testing go mod tidy..."
go mod tidy
if [ $? -eq 0 ]; then
    echo "✅ Go mod tidy completed successfully"
else
    echo "❌ Go mod tidy failed"
    exit 1
fi

# Test 3: Syntax Check
echo ""
echo "3. Testing syntax..."
if go build -o /dev/null ./cmd/tapio; then
    echo "✅ Syntax check passed"
else
    echo "❌ Syntax check failed"
    exit 1
fi

# Test 4: Makefile Targets
echo ""
echo "4. Testing Makefile targets..."

# Test fmt target
echo "  Testing 'make fmt'..."
if make fmt >/dev/null 2>&1; then
    echo "  ✅ make fmt passed"
else
    echo "  ❌ make fmt failed"
    exit 1
fi

# Test lint target
echo "  Testing 'make lint'..."
if make lint >/dev/null 2>&1; then
    echo "  ✅ make lint passed"
else
    echo "  ⚠️  make lint had issues (continuing...)"
fi

# Test build target
echo "  Testing 'make build'..."
if make build >/dev/null 2>&1; then
    echo "  ✅ make build passed"
    if [ -f bin/tapio ]; then
        echo "  ✅ Binary created successfully"
    else
        echo "  ❌ Binary not found after build"
        exit 1
    fi
else
    echo "  ❌ make build failed"
    exit 1
fi

# Test ci-check target
echo "  Testing 'make ci-check'..."
if make ci-check >/dev/null 2>&1; then
    echo "  ✅ make ci-check passed"
else
    echo "  ❌ make ci-check failed"
    exit 1
fi

# Test 5: Binary Functionality
echo ""
echo "5. Testing binary functionality..."
if [ -f bin/tapio ]; then
    if ./bin/tapio --help >/dev/null 2>&1; then
        echo "✅ Binary executes successfully"
    else
        echo "❌ Binary execution failed"
        exit 1
    fi
else
    echo "❌ Binary not found"
    exit 1
fi

# Test 6: Dependency Check
echo ""
echo "6. Testing dependencies..."
MISSING_DEPS=()

# Check for required Go version
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION="1.23.0"
if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" = "$REQUIRED_VERSION" ]; then
    echo "✅ Go version $GO_VERSION meets requirement (>= $REQUIRED_VERSION)"
else
    echo "❌ Go version $GO_VERSION is below required $REQUIRED_VERSION"
    exit 1
fi

# Check for git
if command -v git >/dev/null 2>&1; then
    echo "✅ Git available"
else
    echo "❌ Git not found"
    MISSING_DEPS+=("git")
fi

# Check for make
if command -v make >/dev/null 2>&1; then
    echo "✅ Make available"
else
    echo "❌ Make not found"
    MISSING_DEPS+=("make")
fi

if [ ${#MISSING_DEPS[@]} -gt 0 ]; then
    echo "❌ Missing dependencies: ${MISSING_DEPS[*]}"
    exit 1
fi

# Test 7: Project Structure Validation
echo ""
echo "7. Testing project structure..."

# Check for single go.mod
GO_MOD_COUNT=$(find . -name "go.mod" -not -path "./cmd/tapio-gui/frontend/node_modules/*" -not -path "./gui/tapio-gui/frontend/node_modules/*" | wc -l)
if [ "$GO_MOD_COUNT" -eq 1 ]; then
    echo "✅ Single go.mod file found"
else
    echo "❌ Found $GO_MOD_COUNT go.mod files (should be 1)"
    echo "Extra go.mod files:"
    find . -name "go.mod" -not -path "./cmd/tapio-gui/frontend/node_modules/*" -not -path "./gui/tapio-gui/frontend/node_modules/*"
    exit 1
fi

# Check for main entry point
if [ -f cmd/tapio/main.go ]; then
    echo "✅ Main entry point found"
else
    echo "❌ Main entry point (cmd/tapio/main.go) not found"
    exit 1
fi

# Test 8: Clean Build Test
echo ""
echo "8. Testing clean build..."
make clean >/dev/null 2>&1
if make build >/dev/null 2>&1; then
    echo "✅ Clean build successful"
else
    echo "❌ Clean build failed"
    exit 1
fi

echo ""
echo "🎉 Build System Validation Complete!"
echo "===================================="
echo "✅ All validation tests passed"
echo "✅ Build system is working correctly"
echo "✅ Ready for development and CI/CD"
echo ""
echo "Next steps:"
echo "  make ci        # Run full CI pipeline"
echo "  make test      # Run tests"
echo "  make dev       # Development cycle"