#!/bin/bash

# Script to rebuild all BPF programs with new minimal vmlinux.h
# Run this inside your Linux VM

set -e

echo "=== Rebuilding BPF Programs with Minimal vmlinux.h ==="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: Must run from Tapio project root${NC}"
    exit 1
fi

# Install required packages if missing
if ! command -v clang &> /dev/null; then
    echo -e "${YELLOW}Installing required packages...${NC}"
    sudo apt-get update
    sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r) build-essential
fi

echo -e "${GREEN}Starting BPF compilation...${NC}"
echo ""

# Function to compile BPF program
compile_bpf() {
    local dir=$1
    local name=$2
    echo -e "${YELLOW}Building $name in $dir${NC}"
    
    cd "$dir"
    
    # Clean old generated files
    rm -f *_bpfel.go *_bpfeb.go *.o
    
    # Run go generate to compile BPF
    if go generate ./...; then
        echo -e "${GREEN}✓ $name built successfully${NC}"
    else
        echo -e "${RED}✗ Failed to build $name${NC}"
        return 1
    fi
    
    cd - > /dev/null
    echo ""
}

# Build each collector
echo "1. Building CNI Monitor..."
compile_bpf "pkg/collectors/cni" "CNI Monitor"

echo "2. Building Kernel Monitor..."
compile_bpf "pkg/collectors/ebpf" "Kernel Monitor"

echo "3. Building etcd Monitor..."
compile_bpf "pkg/collectors/etcd" "etcd Monitor"

echo "4. Building SystemD Monitor..."
compile_bpf "pkg/collectors/systemd" "SystemD Monitor"

echo ""
echo -e "${GREEN}=== BPF Compilation Complete ===${NC}"
echo ""

# List generated files
echo "Generated CO-RE BPF objects:"
find pkg/collectors -name "*_bpfel.go" -o -name "*_bpfeb.go" | while read -r file; do
    echo -e "${GREEN}✓${NC} $file"
done