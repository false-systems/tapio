#!/bin/bash
set -e

echo "🔧 Generating eBPF bindings in Linux container..."

# Get the project root (3 levels up from ebpf dir)
PROJECT_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)

# Run generation in container from project root
docker run --rm -v "$PROJECT_ROOT:/app" -w /app \
  golang:latest sh -c "
    apk add --no-cache build-base linux-headers clang llvm git &&
    go install github.com/cilium/ebpf/cmd/bpf2go@latest &&
    cd pkg/collectors/ebpf &&
    go generate ./...
"

echo "✅ eBPF bindings generated!"
echo "📁 Generated files:"
ls -la *.go | grep -E "(unified|k8stracker)" || echo "No files generated yet"