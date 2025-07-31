#!/bin/bash
set -e

echo "🚀 Testing K8s eBPF monitoring in Linux container..."

# Build the test container
echo "📦 Building test container..."
docker build -f pkg/collectors/k8s/Dockerfile.test -t tapio-k8s-ebpf-test .

# Run with required privileges for eBPF
echo "🏃 Running K8s eBPF test (requires privileged mode for eBPF)..."
docker run --rm -it \
  --privileged \
  --pid=host \
  --network=host \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  -v /sys/fs/bpf:/sys/fs/bpf:rw \
  -v /proc:/proc:ro \
  tapio-k8s-ebpf-test

echo "✅ Test complete!"