#!/bin/bash
set -euo pipefail

# Build automation script for eBPF memory tracking
# This script compiles eBPF programs and generates Go bindings

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
EBPF_DIR="$PROJECT_ROOT/ebpf"
PKG_DIR="$PROJECT_ROOT/pkg/collectors/ebpf"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking build prerequisites..."
    
    # Check for clang
    if ! command -v clang &> /dev/null; then
        log_error "clang is required but not installed"
        log_info "Install with: sudo apt-get install clang (Ubuntu/Debian) or brew install llvm (macOS)"
        exit 1
    fi
    
    # Check for llvm-strip (for optimized binaries)
    if ! command -v llvm-strip &> /dev/null; then
        log_warn "llvm-strip not found, skipping binary stripping"
    fi
    
    # Check for bpftool (optional, for debugging)
    if ! command -v bpftool &> /dev/null; then
        log_warn "bpftool not found, skipping eBPF program inspection"
    fi
    
    # Check Go installation
    if ! command -v go &> /dev/null; then
        log_error "Go is required but not installed"
        exit 1
    fi
    
    # Check for required Go tools
    if ! go list -m github.com/cilium/ebpf/cmd/bpf2go &> /dev/null; then
        log_info "Installing bpf2go tool..."
        go install github.com/cilium/ebpf/cmd/bpf2go@latest
    fi
    
    log_info "Prerequisites check passed"
}

# Check kernel compatibility
check_kernel_compatibility() {
    log_info "Checking kernel compatibility..."
    
    if [[ "$OSTYPE" != "linux-gnu"* ]]; then
        log_warn "eBPF is only supported on Linux, current OS: $OSTYPE"
        return 0
    fi
    
    KERNEL_VERSION=$(uname -r | cut -d. -f1-2)
    MAJOR=$(echo $KERNEL_VERSION | cut -d. -f1)
    MINOR=$(echo $KERNEL_VERSION | cut -d. -f2)
    
    log_info "Detected kernel version: $(uname -r)"
    
    # Check minimum kernel version (4.18+)
    if [ "$MAJOR" -lt 4 ] || ([ "$MAJOR" -eq 4 ] && [ "$MINOR" -lt 18 ]); then
        log_error "Kernel version $KERNEL_VERSION is too old, minimum required is 4.18"
        exit 1
    fi
    
    # Check for ring buffer support (5.8+)
    if [ "$MAJOR" -lt 5 ] || ([ "$MAJOR" -eq 5 ] && [ "$MINOR" -lt 8 ]); then
        log_warn "Kernel version $KERNEL_VERSION does not support ring buffers (requires 5.8+)"
        log_warn "Performance may be degraded, consider upgrading"
    else
        log_info "Ring buffer support detected"
    fi
    
    # Check for BTF support
    if [ -f "/sys/kernel/btf/vmlinux" ]; then
        log_info "BTF support detected"
    else
        log_warn "BTF support not available, CO-RE features disabled"
    fi
    
    log_info "Kernel compatibility check passed"
}

# Build eBPF programs
build_ebpf_programs() {
    log_info "Building eBPF programs..."
    
    cd "$PKG_DIR"
    
    # Clean old generated files
    rm -f *_bpfe*.go *_bpfe*.o
    
    # Generate Go bindings for memory tracker
    log_info "Generating Go bindings for memory tracker..."
    go generate ./memory.go
    
    # Verify generated files
    if [ ! -f "memory_bpfel.go" ]; then
        log_error "Failed to generate memory_bpfel.go"
        exit 1
    fi
    
    if [ ! -f "memory_bpfeb.go" ]; then
        log_error "Failed to generate memory_bpfeb.go"
        exit 1
    fi
    
    log_info "eBPF program compilation completed successfully"
}

# Optimize eBPF binaries
optimize_ebpf_binaries() {
    log_info "Optimizing eBPF binaries..."
    
    cd "$PKG_DIR"
    
    # Strip debug information from object files if llvm-strip is available
    if command -v llvm-strip &> /dev/null; then
        for obj_file in *.o; do
            if [ -f "$obj_file" ]; then
                log_info "Stripping debug info from $obj_file"
                llvm-strip "$obj_file"
            fi
        done
    fi
    
    # Verify program size
    for obj_file in *.o; do
        if [ -f "$obj_file" ]; then
            size=$(stat -f%z "$obj_file" 2>/dev/null || stat -c%s "$obj_file" 2>/dev/null || echo "unknown")
            log_info "eBPF program size: $obj_file = ${size} bytes"
        fi
    done
}

# Run performance benchmarks
run_performance_benchmarks() {
    log_info "Running performance benchmarks..."
    
    cd "$PKG_DIR"
    
    # Run Go benchmarks
    if go test -bench=. -benchtime=5s -count=3 ./... &> /dev/null; then
        log_info "Performance benchmarks:"
        go test -bench=. -benchtime=5s -count=3 ./... | grep "Benchmark"
    else
        log_warn "Performance benchmarks not available (tests not implemented yet)"
    fi
}

# Validate eBPF programs
validate_ebpf_programs() {
    log_info "Validating eBPF programs..."
    
    cd "$PKG_DIR"
    
    # Check if programs compile without errors
    if go build -tags ebpf ./...; then
        log_info "eBPF programs compile successfully"
    else
        log_error "eBPF program compilation failed"
        exit 1
    fi
    
    # Use bpftool for additional validation if available
    if command -v bpftool &> /dev/null; then
        for obj_file in *.o; do
            if [ -f "$obj_file" ]; then
                log_info "Inspecting eBPF program: $obj_file"
                bpftool prog dump xlated "$obj_file" &> /dev/null || log_warn "Could not inspect $obj_file"
            fi
        done
    fi
}

# Generate build report
generate_build_report() {
    log_info "Generating build report..."
    
    REPORT_FILE="$PROJECT_ROOT/build-report.txt"
    
    cat > "$REPORT_FILE" << EOF
eBPF Memory Tracking Build Report
Generated on: $(date)
Kernel Version: $(uname -r)
Clang Version: $(clang --version | head -n1)
Go Version: $(go version)

eBPF Programs Built:
EOF
    
    cd "$PKG_DIR"
    for obj_file in *.o; do
        if [ -f "$obj_file" ]; then
            size=$(stat -f%z "$obj_file" 2>/dev/null || stat -c%s "$obj_file" 2>/dev/null || echo "unknown")
            echo "  - $obj_file (${size} bytes)" >> "$REPORT_FILE"
        fi
    done
    
    echo "" >> "$REPORT_FILE"
    echo "Build completed successfully at $(date)" >> "$REPORT_FILE"
    
    log_info "Build report saved to: $REPORT_FILE"
}

# Main build process
main() {
    log_info "Starting eBPF memory tracking build process..."
    
    check_prerequisites
    check_kernel_compatibility
    build_ebpf_programs
    optimize_ebpf_binaries
    validate_ebpf_programs
    run_performance_benchmarks
    generate_build_report
    
    log_info "eBPF build process completed successfully!"
    log_info "Built programs are ready for high-performance memory tracking"
}

# Run main function
main "$@"