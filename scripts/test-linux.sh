#!/bin/bash
# Test Linux-specific collectors in Docker container

set -e

echo "🐳 Starting Linux tests in Docker..."

# Check if Colima is running
if ! colima status &>/dev/null; then
    echo "📦 Starting Colima..."
    colima start
fi

# Run tests based on argument
case "$1" in
    "cni")
        echo "🔧 Testing CNI collector..."
        docker run --rm -v $(pwd):/app -w /app golang:1.21-alpine \
            sh -c "apk add --no-cache build-base linux-headers && go test -v ./pkg/collectors/cni/..."
        ;;
    "ebpf")
        echo "🔧 Testing eBPF collector..."
        docker run --rm --privileged -v $(pwd):/app -w /app golang:1.21-alpine \
            sh -c "apk add --no-cache build-base linux-headers && go test -v ./pkg/collectors/ebpf/..."
        ;;
    "systemd")
        echo "🔧 Testing systemd collector..."
        # Systemd requires special container
        docker run --rm -v $(pwd):/app -w /app golang:1.21 \
            sh -c "go test -v ./pkg/collectors/systemd/..."
        ;;
    "all")
        echo "🔧 Testing all collectors..."
        docker run --rm --privileged -v $(pwd):/app -w /app golang:1.21-alpine \
            sh -c "apk add --no-cache build-base linux-headers && go test -v ./pkg/collectors/..."
        ;;
    *)
        echo "Usage: $0 {cni|ebpf|systemd|all}"
        exit 1
        ;;
esac

echo "✅ Linux tests completed!"