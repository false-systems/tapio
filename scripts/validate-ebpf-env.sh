#!/bin/bash
# eBPF Environment Validation Script
# This script checks if your Linux environment is ready for eBPF development

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
ERRORS=0
WARNINGS=0

log_pass() {
    echo -e "${GREEN}✓${NC} $1"
}

log_fail() {
    echo -e "${RED}✗${NC} $1"
    ERRORS=$((ERRORS + 1))
}

log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    WARNINGS=$((WARNINGS + 1))
}

log_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

header() {
    echo ""
    echo -e "${BLUE}=== $1 ===${NC}"
}

# Check if running on Linux
check_os() {
    header "Operating System Check"
    
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        log_pass "Running on Linux"
        log_info "Distribution: $(lsb_release -d 2>/dev/null | cut -f2 || cat /etc/os-release | grep PRETTY_NAME | cut -d'"' -f2)"
    else
        log_fail "Not running on Linux (detected: $OSTYPE)"
        log_info "eBPF is only supported on Linux"
        exit 1
    fi
}

# Check kernel version
check_kernel() {
    header "Kernel Version Check"
    
    KERNEL_VERSION=$(uname -r)
    log_info "Kernel version: $KERNEL_VERSION"
    
    # Extract major.minor version
    MAJOR=$(echo "$KERNEL_VERSION" | cut -d. -f1)
    MINOR=$(echo "$KERNEL_VERSION" | cut -d. -f2)
    
    # Check minimum version (4.18)
    if [ "$MAJOR" -gt 4 ] || ([ "$MAJOR" -eq 4 ] && [ "$MINOR" -ge 18 ]); then
        log_pass "Kernel version meets minimum requirement (4.18+)"
    else
        log_fail "Kernel version too old (minimum 4.18 required)"
    fi
    
    # Check for ring buffer support (5.8+)
    if [ "$MAJOR" -gt 5 ] || ([ "$MAJOR" -eq 5 ] && [ "$MINOR" -ge 8 ]); then
        log_pass "Ring buffer support available (5.8+)"
    else
        log_warn "Ring buffer not supported (requires 5.8+)"
        log_info "Will need to use perf buffer fallback"
    fi
}

# Check eBPF filesystem
check_bpf_fs() {
    header "BPF Filesystem Check"
    
    if mount | grep -q "type bpf"; then
        log_pass "BPF filesystem is mounted"
        log_info "Mount point: $(mount | grep "type bpf" | awk '{print $3}')"
    else
        log_warn "BPF filesystem not mounted"
        log_info "Run: sudo mount -t bpf bpf /sys/fs/bpf"
    fi
    
    if [ -d "/sys/fs/bpf" ]; then
        log_pass "/sys/fs/bpf directory exists"
    else
        log_fail "/sys/fs/bpf directory not found"
    fi
}

# Check kernel configuration
check_kernel_config() {
    header "Kernel Configuration Check"
    
    CONFIG_FILE="/boot/config-$(uname -r)"
    if [ ! -f "$CONFIG_FILE" ]; then
        CONFIG_FILE="/proc/config.gz"
        if [ ! -f "$CONFIG_FILE" ]; then
            log_warn "Kernel config not found, skipping detailed checks"
            return
        fi
    fi
    
    # Check BPF support
    if grep -q "CONFIG_BPF=y" "$CONFIG_FILE" 2>/dev/null || zgrep -q "CONFIG_BPF=y" "$CONFIG_FILE" 2>/dev/null; then
        log_pass "CONFIG_BPF enabled"
    else
        log_fail "CONFIG_BPF not enabled"
    fi
    
    # Check BPF syscall
    if grep -q "CONFIG_BPF_SYSCALL=y" "$CONFIG_FILE" 2>/dev/null || zgrep -q "CONFIG_BPF_SYSCALL=y" "$CONFIG_FILE" 2>/dev/null; then
        log_pass "CONFIG_BPF_SYSCALL enabled"
    else
        log_fail "CONFIG_BPF_SYSCALL not enabled"
    fi
    
    # Check BTF support
    if [ -f "/sys/kernel/btf/vmlinux" ]; then
        log_pass "BTF (BPF Type Format) available"
    else
        log_warn "BTF not available (affects CO-RE support)"
    fi
}

# Check required tools
check_tools() {
    header "Development Tools Check"
    
    # Check clang
    if command -v clang &> /dev/null; then
        CLANG_VERSION=$(clang --version | head -n1)
        log_pass "clang installed: $CLANG_VERSION"
    else
        log_fail "clang not found (required for compiling eBPF)"
        log_info "Install: sudo apt-get install clang"
    fi
    
    # Check llvm
    if command -v llc &> /dev/null; then
        log_pass "LLVM installed"
    else
        log_warn "LLVM tools not found (optional but recommended)"
    fi
    
    # Check Go
    if command -v go &> /dev/null; then
        GO_VERSION=$(go version)
        log_pass "Go installed: $GO_VERSION"
        
        # Check Go version (need 1.19+)
        GO_MINOR=$(go version | awk '{print $3}' | cut -d. -f2)
        if [ "$GO_MINOR" -ge 19 ]; then
            log_pass "Go version meets requirement (1.19+)"
        else
            log_warn "Go version may be too old (recommend 1.19+)"
        fi
    else
        log_fail "Go not installed"
        log_info "Install from https://golang.org/dl/"
    fi
    
    # Check bpftool (optional)
    if command -v bpftool &> /dev/null; then
        log_pass "bpftool installed (useful for debugging)"
    else
        log_info "bpftool not found (optional debugging tool)"
    fi
}

# Check kernel headers
check_headers() {
    header "Kernel Headers Check"
    
    HEADERS_DIR="/lib/modules/$(uname -r)/build"
    if [ -d "$HEADERS_DIR" ]; then
        log_pass "Kernel headers installed"
    else
        log_fail "Kernel headers not found"
        log_info "Install: sudo apt-get install linux-headers-$(uname -r)"
    fi
}

# Check permissions
check_permissions() {
    header "Permissions Check"
    
    if [ "$EUID" -eq 0 ]; then
        log_pass "Running as root"
    else
        log_warn "Not running as root"
        log_info "eBPF operations require root or CAP_BPF capability"
    fi
    
    # Check if current user can use sudo
    if sudo -n true 2>/dev/null; then
        log_pass "Can use sudo without password"
    else
        log_info "May need to enter password for sudo operations"
    fi
}

# Check Go tools
check_go_tools() {
    header "Go Tools Check"
    
    # Check if bpf2go is installed
    if command -v bpf2go &> /dev/null; then
        log_pass "bpf2go installed"
    else
        log_warn "bpf2go not installed"
        log_info "Install: go install github.com/cilium/ebpf/cmd/bpf2go@latest"
    fi
    
    # Check GOPATH
    if [ -n "${GOPATH:-}" ]; then
        log_info "GOPATH: $GOPATH"
    else
        log_info "GOPATH not set (using Go modules)"
    fi
}

# Test eBPF functionality
test_ebpf() {
    header "eBPF Functionality Test"
    
    if [ "$EUID" -ne 0 ]; then
        log_warn "Skipping eBPF tests (requires root)"
        return
    fi
    
    # Try to load a simple BPF program
    if bpftool prog list &> /dev/null; then
        log_pass "Can list BPF programs"
        PROG_COUNT=$(bpftool prog list | wc -l)
        log_info "Currently loaded BPF programs: $PROG_COUNT"
    else
        log_warn "Cannot list BPF programs"
    fi
}

# Summary
print_summary() {
    header "Summary"
    
    if [ $ERRORS -eq 0 ]; then
        if [ $WARNINGS -eq 0 ]; then
            echo -e "${GREEN}✅ Your environment is ready for eBPF development!${NC}"
        else
            echo -e "${YELLOW}⚠️  Your environment is mostly ready with $WARNINGS warnings${NC}"
            echo "   The warnings above may affect some functionality"
        fi
    else
        echo -e "${RED}❌ Your environment has $ERRORS errors that need to be fixed${NC}"
        echo "   Please address the errors above before proceeding"
    fi
    
    echo ""
    echo "Next steps:"
    echo "1. cd pkg/collectors/ebpf"
    echo "2. go generate ./memory.go"
    echo "3. sudo go test -tags ebpf -v ./..."
}

# Main execution
main() {
    echo "🔍 eBPF Development Environment Validator"
    echo "========================================="
    
    check_os
    check_kernel
    check_bpf_fs
    check_kernel_config
    check_tools
    check_headers
    check_permissions
    check_go_tools
    test_ebpf
    
    print_summary
}

# Run main
main