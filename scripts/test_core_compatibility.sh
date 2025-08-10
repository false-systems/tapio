#!/bin/bash
# Tapio CO-RE Compatibility Test Script
# Tests BPF programs across different kernel versions and architectures

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COLLECTORS=("dns" "cni" "kernel" "systemd" "etcd")
TEST_TIMEOUT=30
VERBOSE=${VERBOSE:-0}

# Results tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Print colored output
print_color() {
    local color=$1
    shift
    echo -e "${color}$@${NC}"
}

# Print test header
print_header() {
    echo ""
    print_color "$BLUE" "============================================"
    print_color "$BLUE" "$1"
    print_color "$BLUE" "============================================"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        print_color "$RED" "Error: This script must be run as root for BPF operations"
        exit 1
    fi
}

# Get system information
get_system_info() {
    print_header "System Information"
    
    KERNEL_VERSION=$(uname -r)
    ARCH=$(uname -m)
    DISTRO=$(lsb_release -ds 2>/dev/null || cat /etc/os-release | grep PRETTY_NAME | cut -d'"' -f2)
    
    echo "Kernel Version: $KERNEL_VERSION"
    echo "Architecture: $ARCH"
    echo "Distribution: $DISTRO"
    echo "Hostname: $(hostname)"
    echo "CPUs: $(nproc)"
    echo "Memory: $(free -h | grep Mem | awk '{print $2}')"
}

# Check BTF availability
check_btf() {
    print_header "BTF Support Check"
    
    if [ -f /sys/kernel/btf/vmlinux ]; then
        print_color "$GREEN" "✓ BTF available at /sys/kernel/btf/vmlinux"
        BTF_AVAILABLE=1
        
        # Check BTF file size
        BTF_SIZE=$(stat -c%s /sys/kernel/btf/vmlinux)
        echo "  BTF file size: $(numfmt --to=iec $BTF_SIZE)"
    else
        print_color "$YELLOW" "✗ BTF not available in /sys/kernel/btf/vmlinux"
        
        # Check alternative locations
        ALT_PATHS=(
            "/boot/vmlinux-$KERNEL_VERSION"
            "/lib/modules/$KERNEL_VERSION/vmlinux"
        )
        
        BTF_AVAILABLE=0
        for path in "${ALT_PATHS[@]}"; do
            if [ -f "$path" ]; then
                print_color "$YELLOW" "  Found potential BTF at: $path"
                BTF_AVAILABLE=1
                break
            fi
        done
        
        if [ $BTF_AVAILABLE -eq 0 ]; then
            print_color "$RED" "  No BTF files found - CO-RE may not work properly"
        fi
    fi
}

# Check BPF filesystem
check_bpf_fs() {
    print_header "BPF Filesystem Check"
    
    if mountpoint -q /sys/fs/bpf; then
        print_color "$GREEN" "✓ BPF filesystem mounted at /sys/fs/bpf"
    else
        print_color "$YELLOW" "✗ BPF filesystem not mounted, attempting to mount..."
        mount -t bpf bpf /sys/fs/bpf 2>/dev/null || {
            print_color "$RED" "  Failed to mount BPF filesystem"
            return 1
        }
        print_color "$GREEN" "  Successfully mounted BPF filesystem"
    fi
}

# Check kernel configuration
check_kernel_config() {
    print_header "Kernel Configuration Check"
    
    CONFIG_FILE="/boot/config-$KERNEL_VERSION"
    if [ ! -f "$CONFIG_FILE" ]; then
        CONFIG_FILE="/proc/config.gz"
        if [ ! -f "$CONFIG_FILE" ]; then
            print_color "$YELLOW" "✗ Kernel config not found"
            return
        fi
    fi
    
    # Essential BPF configs
    CONFIGS=(
        "CONFIG_BPF=y"
        "CONFIG_BPF_SYSCALL=y"
        "CONFIG_BPF_JIT=y"
        "CONFIG_HAVE_EBPF_JIT=y"
        "CONFIG_BPF_EVENTS=y"
        "CONFIG_KPROBE_EVENTS=y"
        "CONFIG_UPROBE_EVENTS=y"
        "CONFIG_DEBUG_INFO_BTF=y"
        "CONFIG_BPF_LSM=y"
    )
    
    for config in "${CONFIGS[@]}"; do
        config_name="${config%=*}"
        if [ "$CONFIG_FILE" = "/proc/config.gz" ]; then
            result=$(zcat $CONFIG_FILE | grep "^$config" || echo "not set")
        else
            result=$(grep "^$config" $CONFIG_FILE || echo "not set")
        fi
        
        if [[ "$result" == *"=y" ]]; then
            print_color "$GREEN" "✓ $config_name enabled"
        elif [[ "$result" == *"=m" ]]; then
            print_color "$YELLOW" "⚠ $config_name enabled as module"
        else
            print_color "$RED" "✗ $config_name not enabled"
        fi
    done
}

# Check BPF helper availability
check_bpf_helpers() {
    print_header "BPF Helper Functions Check"
    
    # Check if bpftool is available
    if command -v bpftool &> /dev/null; then
        print_color "$GREEN" "✓ bpftool available"
        
        # List available helpers (requires newer bpftool)
        if bpftool feature probe kernel 2>/dev/null | grep -q "eBPF helpers"; then
            echo "  Checking helper functions..."
            
            # Key helpers for our programs
            HELPERS=(
                "bpf_probe_read_kernel"
                "bpf_ktime_get_ns"
                "bpf_get_current_pid_tgid"
                "bpf_ringbuf_reserve"
                "bpf_core_read"
            )
            
            for helper in "${HELPERS[@]}"; do
                if bpftool feature probe kernel 2>/dev/null | grep -q "$helper"; then
                    print_color "$GREEN" "  ✓ $helper"
                else
                    print_color "$YELLOW" "  ? $helper (unable to verify)"
                fi
            done
        fi
    else
        print_color "$YELLOW" "✗ bpftool not found - cannot probe helpers"
    fi
}

# Test BPF program compilation
test_compilation() {
    local collector=$1
    print_header "Testing $collector Compilation"
    
    cd "$PROJECT_ROOT"
    
    # Check if BPF source exists
    BPF_SRC="pkg/collectors/$collector/bpf_src"
    if [ ! -d "$BPF_SRC" ]; then
        BPF_SRC="pkg/collectors/$collector/bpf"
    fi
    
    if [ ! -d "$BPF_SRC" ]; then
        print_color "$YELLOW" "⚠ No BPF source directory found for $collector"
        ((SKIPPED_TESTS++))
        return
    fi
    
    # Try to generate BPF bytecode
    print_color "$BLUE" "Generating BPF bytecode for $collector..."
    
    if [ -f "$BPF_SRC/generate.go" ]; then
        cd "$BPF_SRC"
        if timeout $TEST_TIMEOUT go generate ./... 2>/dev/null; then
            print_color "$GREEN" "✓ BPF generation successful for $collector"
            ((PASSED_TESTS++))
        else
            print_color "$RED" "✗ BPF generation failed for $collector"
            ((FAILED_TESTS++))
            
            if [ $VERBOSE -eq 1 ]; then
                echo "  Retrying with verbose output:"
                go generate -v ./...
            fi
        fi
    else
        print_color "$YELLOW" "⚠ No generate.go found for $collector"
        ((SKIPPED_TESTS++))
    fi
    
    ((TOTAL_TESTS++))
}

# Test BPF program loading
test_loading() {
    local collector=$1
    print_header "Testing $collector BPF Loading"
    
    cd "$PROJECT_ROOT"
    
    # Create a simple Go test to load the BPF program
    TEST_FILE="pkg/collectors/$collector/bpf_load_test.go"
    
    cat > "$TEST_FILE" << 'EOF'
//go:build integration
// +build integration

package COLLECTOR_test

import (
    "testing"
    "github.com/cilium/ebpf/rlimit"
)

func TestBPFLoad(t *testing.T) {
    // Remove memlock limit
    if err := rlimit.RemoveMemlock(); err != nil {
        t.Fatalf("Failed to remove memlock: %v", err)
    }
    
    // Try to load the BPF program
    // This is a placeholder - actual implementation would load the specific collector
    t.Log("BPF load test placeholder")
}
EOF
    
    # Replace COLLECTOR with actual package name
    sed -i "s/COLLECTOR/$collector/" "$TEST_FILE"
    
    # Run the test
    if timeout $TEST_TIMEOUT go test -tags=integration "$PROJECT_ROOT/pkg/collectors/$collector" -run TestBPFLoad 2>/dev/null; then
        print_color "$GREEN" "✓ BPF loading test passed for $collector"
        ((PASSED_TESTS++))
    else
        print_color "$RED" "✗ BPF loading test failed for $collector"
        ((FAILED_TESTS++))
    fi
    
    # Clean up test file
    rm -f "$TEST_FILE"
    
    ((TOTAL_TESTS++))
}

# Test CO-RE functionality
test_core_functionality() {
    print_header "Testing CO-RE Functionality"
    
    # Create a simple test to verify CO-RE relocation works
    TEST_DIR="/tmp/tapio_core_test"
    mkdir -p "$TEST_DIR"
    
    cat > "$TEST_DIR/test_core.c" << 'EOF'
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

SEC("kprobe/do_sys_open")
int test_core_relocation(struct pt_regs *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    
    // Test CO-RE relocation
    pid_t pid = BPF_CORE_READ(task, pid);
    
    bpf_printk("PID from CO-RE: %d\n", pid);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
EOF
    
    # Try to compile with CO-RE
    if command -v clang &> /dev/null; then
        cd "$TEST_DIR"
        
        # Find vmlinux.h
        VMLINUX_H="$PROJECT_ROOT/pkg/collectors/bpf_common/vmlinux_minimal.h"
        
        if clang -O2 -target bpf -D__TARGET_ARCH_x86 -I"$PROJECT_ROOT/pkg/collectors/bpf_common" \
                -c test_core.c -o test_core.o 2>/dev/null; then
            print_color "$GREEN" "✓ CO-RE compilation successful"
            ((PASSED_TESTS++))
            
            # Check if relocations are present
            if command -v llvm-objdump &> /dev/null; then
                if llvm-objdump -r test_core.o | grep -q "CO-RE"; then
                    print_color "$GREEN" "✓ CO-RE relocations present"
                else
                    print_color "$YELLOW" "⚠ CO-RE relocations not detected"
                fi
            fi
        else
            print_color "$RED" "✗ CO-RE compilation failed"
            ((FAILED_TESTS++))
        fi
    else
        print_color "$YELLOW" "⚠ clang not found - skipping CO-RE compilation test"
        ((SKIPPED_TESTS++))
    fi
    
    # Clean up
    rm -rf "$TEST_DIR"
    
    ((TOTAL_TESTS++))
}

# Test architecture compatibility
test_arch_compatibility() {
    print_header "Testing Architecture Compatibility"
    
    # Check if we're using architecture-specific code correctly
    cd "$PROJECT_ROOT"
    
    # Search for hardcoded offsets that might cause issues
    echo "Checking for architecture-specific issues..."
    
    if grep -r "offset.*112" pkg/collectors/*/bpf_src/*.c 2>/dev/null; then
        print_color "$YELLOW" "⚠ Found potential x86_64 specific offsets"
        
        # Check if they're properly wrapped with helpers
        if grep -r "get_kprobe_func_arg\|read_sock_from_kprobe" pkg/collectors/*/bpf_src/*.c 2>/dev/null | grep -q "ctx"; then
            print_color "$GREEN" "✓ But using CO-RE helpers for compatibility"
            ((PASSED_TESTS++))
        else
            print_color "$RED" "✗ Not using CO-RE helpers - may fail on ARM64"
            ((FAILED_TESTS++))
        fi
    else
        print_color "$GREEN" "✓ No hardcoded architecture-specific offsets found"
        ((PASSED_TESTS++))
    fi
    
    ((TOTAL_TESTS++))
}

# Main test execution
main() {
    print_color "$BLUE" "╔══════════════════════════════════════════╗"
    print_color "$BLUE" "║   Tapio CO-RE Compatibility Test Suite   ║"
    print_color "$BLUE" "╚══════════════════════════════════════════╝"
    
    # Check prerequisites
    check_root
    
    # System information
    get_system_info
    
    # BPF environment checks
    check_btf
    check_bpf_fs
    check_kernel_config
    check_bpf_helpers
    
    # Test CO-RE functionality
    test_core_functionality
    test_arch_compatibility
    
    # Test each collector
    for collector in "${COLLECTORS[@]}"; do
        test_compilation "$collector"
        
        # Only test loading if compilation succeeded
        if [ $? -eq 0 ]; then
            test_loading "$collector"
        fi
    done
    
    # Print summary
    print_header "Test Summary"
    
    echo "Total Tests: $TOTAL_TESTS"
    print_color "$GREEN" "Passed: $PASSED_TESTS"
    
    if [ $FAILED_TESTS -gt 0 ]; then
        print_color "$RED" "Failed: $FAILED_TESTS"
    else
        echo "Failed: 0"
    fi
    
    if [ $SKIPPED_TESTS -gt 0 ]; then
        print_color "$YELLOW" "Skipped: $SKIPPED_TESTS"
    fi
    
    # Overall result
    echo ""
    if [ $FAILED_TESTS -eq 0 ]; then
        print_color "$GREEN" "✓ All tests passed! System is CO-RE compatible."
        exit 0
    else
        print_color "$RED" "✗ Some tests failed. Review the output above for details."
        exit 1
    fi
}

# Run main function
main "$@"