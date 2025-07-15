#!/bin/bash
set -e

echo "🔧 Fixing Tapio Build System..."

# Step 1: Fix invalid toolchain in main go.mod
echo "1. Fixing invalid Go toolchain version..."
if grep -q "toolchain go1.24.3" go.mod; then
    sed -i.backup 's/toolchain go1.24.3//g' go.mod
    echo "✅ Removed invalid toolchain directive"
fi

# Step 2: Backup and remove conflicting go.mod files
echo "2. Removing conflicting sub-module go.mod files..."
SUBMODULES=(
    "cmd/tapio-cli/go.mod"
    "cmd/tapio-engine/go.mod" 
    "cmd/tapio-gui/go.mod"
    "cmd/plugins/tapio-otel/go.mod"
    "cmd/plugins/tapio-prometheus/go.mod"
    "gui/tapio-gui/go.mod"
)

mkdir -p .backup/go-mods/
for module in "${SUBMODULES[@]}"; do
    if [ -f "$module" ]; then
        echo "  Backing up $module..."
        cp "$module" ".backup/go-mods/$(basename $(dirname "$module"))-go.mod"
        rm "$module"
        echo "  ✅ Removed $module"
    fi
done

# Step 3: Clean and tidy main module
echo "3. Cleaning and tidying main Go module..."
go clean -cache -modcache -testcache || true
go mod tidy
go mod verify

# Step 4: Attempt to build
echo "4. Testing build..."
if go build -o /tmp/tapio-test ./cmd/tapio; then
    echo "✅ Build test successful!"
    rm -f /tmp/tapio-test
else
    echo "❌ Build test failed. Manual intervention required."
    exit 1
fi

echo "🎉 Build system fixed successfully!"
echo ""
echo "Next steps:"
echo "  make build    # Build the project"
echo "  make test     # Run tests"
echo "  make ci       # Full CI pipeline"