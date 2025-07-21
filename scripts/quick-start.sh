#!/bin/bash
set -e

# Tapio Quick Start - Minimal setup for local development
# This script sets up the bare minimum to run Tapio collectors locally

echo "🚀 Tapio Quick Start"
echo "=================="
echo
echo "This will set up the minimum requirements to run Tapio locally."
echo "For full development environment, use ./scripts/install.sh"
echo

# Detect OS
OS="unknown"
case "$(uname -s)" in
    Darwin) OS="macos" ;;
    Linux) OS="linux" ;;
    *) echo "Unsupported OS"; exit 1 ;;
esac

echo "Detected OS: $OS"

# Check Go
if ! command -v go >/dev/null 2>&1; then
    echo "❌ Go is not installed. Please install Go 1.21+ first:"
    echo "   macOS: brew install go"
    echo "   Linux: https://go.dev/dl/"
    exit 1
else
    echo "✅ Go is installed: $(go version)"
fi

# Check Docker
if ! command -v docker >/dev/null 2>&1; then
    echo "❌ Docker is not installed. Please install Docker first:"
    echo "   macOS: brew install docker && brew install colima"
    echo "   Linux: https://docs.docker.com/engine/install/"
    exit 1
else
    echo "✅ Docker is installed"
fi

# Check if Docker is running
if ! docker ps >/dev/null 2>&1; then
    echo "⚠️  Docker daemon is not running"
    if [[ "$OS" == "macos" ]]; then
        echo "   Start with: colima start (or Docker Desktop)"
    else
        echo "   Start with: sudo systemctl start docker"
    fi
else
    echo "✅ Docker is running"
fi

# Go to project root
cd "$(dirname "$0")/.."

# Download dependencies
echo
echo "📦 Downloading Go dependencies..."
go mod download

# Format code
echo "🎨 Formatting code..."
if command -v make >/dev/null 2>&1; then
    make fmt 2>/dev/null || gofmt -w .
else
    gofmt -w .
fi

# Build to verify
echo "🔨 Building project..."
if go build ./...; then
    echo "✅ Build successful!"
else
    echo "⚠️  Build had some errors (this might be expected for platform-specific code)"
fi

echo
echo "✨ Quick Start Complete!"
echo
echo "You can now run Tapio collectors:"
echo
echo "  # Run CNI collector (monitors container networks):"
echo "  go run cmd/tapio-collector/main.go --collectors=cni"
echo
echo "  # Run K8s collector (if you have kubectl configured):"
echo "  go run cmd/tapio-collector/main.go --collectors=k8s"
echo
echo "  # Run SystemD collector (Linux only):"
echo "  go run cmd/tapio-collector/main.go --collectors=systemd"
echo
echo "  # Run all available collectors:"
echo "  go run cmd/tapio-collector/main.go"
echo
echo "For full development setup with Kubernetes, monitoring, etc:"
echo "  ./scripts/install.sh"
echo "  ./scripts/dev-up.sh"
echo