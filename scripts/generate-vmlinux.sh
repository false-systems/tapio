#!/bin/bash
# Generate vmlinux.h from BTF for CO-RE support
# Per CLAUDE.md: Complete implementation, no stubs

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check if running on Linux
if [[ "$OSTYPE" != "linux-gnu"* ]]; then
    echo -e "${RED}Error: This script must run on Linux with BTF support${NC}"
    echo "Run this in a Linux VM or container with kernel >= 5.4"
    exit 1
fi

# Check for bpftool
if ! command -v bpftool &> /dev/null; then
    echo -e "${RED}Error: bpftool not found${NC}"
    echo "Install with: sudo apt-get install linux-tools-common linux-tools-generic"
    exit 1
fi

# Check for BTF support
if [ ! -f /sys/kernel/btf/vmlinux ]; then
    echo -e "${RED}Error: BTF not available in kernel${NC}"
    echo "Kernel must be compiled with CONFIG_DEBUG_INFO_BTF=y"
    exit 1
fi

# Output directory
OUTPUT_DIR="pkg/observers/bpf_common"
mkdir -p "$OUTPUT_DIR"

# Generate vmlinux.h
echo -e "${YELLOW}Generating vmlinux.h from BTF...${NC}"
bpftool btf dump file /sys/kernel/btf/vmlinux format c > "$OUTPUT_DIR/vmlinux.h"

# Verify generation
if [ ! -f "$OUTPUT_DIR/vmlinux.h" ] || [ ! -s "$OUTPUT_DIR/vmlinux.h" ]; then
    echo -e "${RED}Error: Failed to generate vmlinux.h${NC}"
    exit 1
fi

# Get file size
SIZE=$(ls -lh "$OUTPUT_DIR/vmlinux.h" | awk '{print $5}')

echo -e "${GREEN}✓ Generated vmlinux.h (${SIZE})${NC}"
echo "Location: $OUTPUT_DIR/vmlinux.h"

# Generate kernel version info
KERNEL_VERSION=$(uname -r)
echo "// Generated from kernel $KERNEL_VERSION on $(date)" > "$OUTPUT_DIR/vmlinux_info.h"
echo "#define VMLINUX_KERNEL_VERSION \"$KERNEL_VERSION\"" >> "$OUTPUT_DIR/vmlinux_info.h"

echo -e "${GREEN}✓ CO-RE vmlinux.h ready for use${NC}"