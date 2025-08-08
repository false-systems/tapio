#!/bin/bash

# Script to rebuild all BPF programs with new minimal vmlinux.h
# This ensures proper CO-RE relocations with the minimal kernel headers

set -e

echo "=== Rebuilding BPF Programs with Minimal vmlinux.h ==="
echo "This will regenerate all BPF objects with proper CO-RE relocations"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if clang is available
if ! command -v clang &> /dev/null; then
    echo -e "${RED}Error: clang is required to build BPF programs${NC}"
    echo "Please install clang: brew install llvm"
    exit 1
fi

# Check if bpf2go is available
if ! go list -m github.com/cilium/ebpf &> /dev/null; then
    echo -e "${YELLOW}Installing cilium/ebpf tools...${NC}"
    go get -u github.com/cilium/ebpf/cmd/bpf2go
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
compile_bpf "pkg/collectors/kernel" "Kernel Monitor"

echo "3. Building etcd Monitor..."
compile_bpf "pkg/collectors/etcd" "etcd Monitor"

echo "4. Building SystemD Monitor..."
compile_bpf "pkg/collectors/systemd" "SystemD Monitor"

echo "5. Building DNS Monitor..."
compile_bpf "pkg/collectors/dns" "DNS Monitor"



echo ""
echo -e "${GREEN}=== BPF Compilation Complete ===${NC}"
echo ""

# Verify the builds
echo "Verifying generated files..."
echo ""

find pkg/collectors -name "*_bpfel.go" -o -name "*_bpfeb.go" | while read -r file; do
    if [ -f "$file" ]; then
        echo -e "${GREEN}✓${NC} Generated: $file"
    fi
done

echo ""
echo -e "${GREEN}All BPF programs have been rebuilt with the minimal vmlinux.h${NC}"
echo ""
echo "Next steps:"
echo "1. Test the collectors with: go test ./pkg/collectors/..."
echo "2. If running on Linux/Colima, you can test with elevated privileges"
echo "3. Check for any runtime BPF verification errors"
