#!/bin/bash
# Build script for eBPF programs with BTF generation

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Directories
BPF_DIR="pkg/collectors"
BUILD_DIR="build/bpf"

# Ensure build directory exists
mkdir -p "$BUILD_DIR"

echo -e "${GREEN}Building eBPF programs with BTF...${NC}"

# Function to compile eBPF program
compile_bpf() {
    local src_file=$1
    local target_name=$(basename "$src_file" .c)
    local dir_name=$(dirname "$src_file")
    
    echo -e "  Compiling ${YELLOW}$target_name${NC}..."
    
    # Compile with BTF generation
    clang -O2 -g -target bpf \
        -D__TARGET_ARCH_x86 \
        -I/usr/include/x86_64-linux-gnu \
        -I/usr/include \
        -Wall \
        -Wno-unused-value \
        -Wno-pointer-sign \
        -Wno-compare-distinct-pointer-types \
        -c "$src_file" \
        -o "$BUILD_DIR/${target_name}.o"
    
    # Generate BTF if possible
    if command -v bpftool &> /dev/null; then
        bpftool btf dump file "$BUILD_DIR/${target_name}.o" format c > "$BUILD_DIR/${target_name}.btf.h" 2>/dev/null || true
    fi
    
    # Generate Go bindings with bpf2go if available
    if command -v bpf2go &> /dev/null; then
        echo -e "    Generating Go bindings for $target_name..."
        cd "$dir_name" && bpf2go -cc clang -cflags "-O2 -g" "$target_name" "$(basename $src_file)" 2>/dev/null || true
        cd - > /dev/null
    fi
}

# Find all eBPF C files
BPF_FILES=$(find "$BPF_DIR" -name "*.c" -path "*/bpf*/*" | sort)

if [ -z "$BPF_FILES" ]; then
    echo -e "${YELLOW}No eBPF programs found${NC}"
    exit 0
fi

# Compile each eBPF program
for bpf_file in $BPF_FILES; do
    compile_bpf "$bpf_file"
done

echo -e "${GREEN}eBPF build complete!${NC}"
echo -e "  Object files: ${BUILD_DIR}/*.o"
echo -e "  BTF headers: ${BUILD_DIR}/*.btf.h"

# Verify the compiled objects
echo -e "\n${GREEN}Verifying eBPF objects:${NC}"
for obj in "$BUILD_DIR"/*.o; do
    if [ -f "$obj" ]; then
        name=$(basename "$obj")
        size=$(stat -c%s "$obj")
        echo -e "  ✓ $name (${size} bytes)"
    fi
done