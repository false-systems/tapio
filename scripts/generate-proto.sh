#!/bin/bash
# Script to generate protobuf code for Tapio

set -e

echo "🔧 Generating protobuf code..."

# Ensure we're in the project root
cd "$(dirname "$0")/.."

# Create output directory
mkdir -p proto/gen/tapio/v1

# Generate Go code
echo "📝 Generating Go code from proto files..."
cd proto

# Check if buf is installed
if ! which buf > /dev/null; then
    echo "❌ buf is not installed. Please run 'make proto-install' first."
    exit 1
fi

# Run buf generate
buf generate

echo "✅ Protobuf code generation complete!"
echo ""
echo "Generated files:"
find gen -name "*.go" -type f | sort

# Create a simple go.mod for the generated code if it doesn't exist
if [ ! -f "gen/go.mod" ]; then
    echo ""
    echo "📦 Creating go.mod for generated code..."
    cd gen
    go mod init github.com/yairfalse/tapio/proto/gen
    go mod tidy
fi

echo ""
echo "✅ All done! Proto files have been generated."