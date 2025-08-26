#!/bin/bash
# Build script for Linux-only eBPF collectors using Colima
# 
# Prerequisites:
#   - Colima installed: brew install colima
#   - Start Colima with: colima start --memory 4 --mount /Users/yair/projects/tapio:/workspace:w
#
# Usage:
#   colima ssh
#   cd /workspace && ./scripts/colima-build.sh

set -e

echo "🐧 Building Linux-only eBPF collectors in Colima..."

# Check if we're in Colima
if [[ "$(uname -s)" != "Linux" ]]; then
    echo "❌ This script must be run inside Colima VM"
    echo "   Run: colima ssh"
    echo "   Then: cd /workspace && ./scripts/colima-build.sh"
    exit 1
fi

echo "✅ Running in Linux environment"

# Install required tools for BPF compilation
echo "🔧 Checking for required tools..."
if ! command -v llvm-strip &> /dev/null; then
    echo "  Installing LLVM tools..."
    sudo apt-get update && sudo apt-get install -y llvm clang
fi

# Generate BPF objects
echo "🔄 Generating BPF objects..."

# Find the correct path - could be /workspace or mounted differently
if [ -d "/workspace/pkg/collectors" ]; then
    cd /workspace/pkg/collectors
elif [ -d "$HOME/projects/tapio/pkg/collectors" ]; then
    cd $HOME/projects/tapio/pkg/collectors
else
    echo "❌ Cannot find pkg/collectors directory"
    echo "   Current directory: $(pwd)"
    echo "   Available directories:"
    ls -la
    exit 1
fi

echo "📁 Working in: $(pwd)"

# Generate BPF objects for each collector
for collector in kernel syscall-errors etcd-ebpf cri-ebpf; do
    if [ -d "./$collector" ]; then
        echo -n "  Generating BPF for $collector... "
        cd ./$collector
        if go generate ./... 2>/dev/null; then
            echo "✅"
        else
            echo "❌"
            go generate ./... 2>&1 | head -3
        fi
        cd ..
    fi
done

echo ""
echo "🔨 Building eBPF collectors..."

for collector in kernel systemd namespace-collector oom storage-io syscall-errors cri-ebpf etcd-ebpf; do
    echo -n "  Building $collector... "
    if go build ./$collector 2>/dev/null; then
        echo "✅"
    else
        echo "❌"
        go build ./$collector 2>&1 | head -3
    fi
done

echo ""
echo "🧪 Running tests for eBPF collectors..."
go test -v ./kernel ./systemd ./namespace-collector ./oom ./storage-io ./syscall-errors ./cri-ebpf ./etcd-ebpf

echo ""
echo "✅ Build complete!"