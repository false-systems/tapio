#!/bin/bash
set -e

echo "🔧 Testing eBPF compilation in Linux container..."

# Compile the K8s tracker
docker run --rm -v $(pwd):/src -w /src \
  ubuntu:22.04 sh -c "
    apt-get update && apt-get install -y clang llvm libbpf-dev &&
    clang -O2 -g -Wall -target bpf \
      -D__TARGET_ARCH_arm64 \
      -I./bpf -I./bpf/headers \
      -c bpf/k8s_tracker.c -o bpf/k8s_tracker.o &&
    echo '✅ k8s_tracker.c compiled successfully!' &&
    llvm-objdump -h bpf/k8s_tracker.o
"

echo ""
echo "🎉 eBPF program compiled successfully!"