#!/bin/bash

# Test eBPF DNS monitoring in Linux VM
# Must be run as root or with CAP_SYS_ADMIN

set -e

echo "🔍 Testing DNS eBPF Observer in Linux..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "❌ Please run as root (sudo)"
    exit 1
fi

# Check kernel version
KERNEL_VERSION=$(uname -r)
echo "📦 Kernel version: $KERNEL_VERSION"

# Check for required tools
echo "🔧 Checking required tools..."
for tool in clang llc bpftool; do
    if ! command -v $tool &> /dev/null; then
        echo "❌ Missing $tool - installing..."
        apt-get update && apt-get install -y clang llvm linux-tools-common linux-tools-$(uname -r)
    fi
done

# Build eBPF program
echo "🔨 Building eBPF program..."
cd bpf
make clean
make all

# Check if build succeeded
if [ ! -f dns_monitor.o ]; then
    echo "❌ Failed to build eBPF program"
    exit 1
fi

echo "✅ eBPF program built successfully"

# Load and test
echo "🚀 Loading DNS eBPF monitor..."
cd ..

# Run the DNS observer with eBPF
go test -v -tags=ebpf -run TestEBPFDNSCapture ./... 2>&1 | tee ebpf_test.log

# Check if BPF programs are loaded
echo "📊 Checking loaded BPF programs..."
bpftool prog list | grep dns || echo "No DNS programs loaded"

echo "📈 Checking BPF maps..."
bpftool map list | grep -E "active_queries|dns_events" || echo "No DNS maps found"

# Trigger some DNS queries for testing
echo "🌐 Generating test DNS queries..."
nslookup google.com 8.8.8.8 &
nslookup cloudflare.com 1.1.1.1 &
nslookup nonexistent.domain.local 8.8.8.8 &
dig +short example.com @8.8.8.8 &
wait

# Check for captured events
echo "📋 Checking for captured DNS events..."
if [ -f ebpf_test.log ]; then
    grep -E "DNS|problem|latency" ebpf_test.log || echo "No DNS events captured"
fi

echo "✅ eBPF DNS monitoring test complete!"