#!/bin/bash
set -e

echo "Testing simple eBPF compilation..."

# Just compile k8s_tracker.c
docker run --rm -v $(pwd):/src -w /src ubuntu:22.04 bash -c "
  apt-get update -qq && 
  apt-get install -qq -y clang llvm libbpf-dev 2>/dev/null &&
  echo 'Compiling k8s_tracker.c...' &&
  clang -O2 -g -Wall -target bpf \
    -D__TARGET_ARCH_arm64 \
    -I./bpf -I./bpf/headers \
    -c bpf/k8s_tracker.c -o bpf/k8s_tracker.o 2>&1 || exit 1
  echo 'Success! Created k8s_tracker.o'
"

# Check result
if [ -f "bpf/k8s_tracker.o" ]; then
    echo "✅ eBPF object file created successfully!"
    ls -la bpf/k8s_tracker.o
else
    echo "❌ Failed to create object file"
    exit 1
fi