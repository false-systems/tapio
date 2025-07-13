# eBPF Memory Collector Testing Guide

This guide provides step-by-step instructions for testing the eBPF memory collector implementation on Linux systems.

## Prerequisites

### System Requirements
- **Linux kernel 5.8+** (for ring buffer support)
- **Root access** or CAP_BPF capabilities
- **Go 1.19+** installed
- **Development tools** (clang, make, etc.)

### Check Your System
```bash
# Check kernel version (must be 5.8+)
uname -r

# Check for eBPF support
ls /sys/fs/bpf

# Check for BTF support (optional but recommended)
ls /sys/kernel/btf/vmlinux
```

## Installation Steps

### 1. Install Required Packages

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y \
    clang \
    llvm \
    linux-headers-$(uname -r) \
    linux-tools-$(uname -r) \
    linux-tools-common \
    build-essential \
    pkg-config \
    libelf-dev
```

**Fedora/RHEL:**
```bash
sudo dnf install -y \
    clang \
    llvm \
    kernel-devel \
    kernel-headers \
    bpftool \
    elfutils-libelf-devel
```

### 2. Install Go Tools
```bash
# Install bpf2go for generating Go bindings
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Ensure it's in your PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

## Building the eBPF Collector

### 1. Generate eBPF Go Bindings
```bash
cd pkg/collectors/ebpf

# Clean any existing generated files
rm -f *_bpfel.go *_bpfeb.go *.o

# Generate the bindings (this compiles the C code)
go generate ./memory.go
```

**Expected output:**
- `memorytracker_bpfel.go` - Little-endian bindings
- `memorytracker_bpfeb.go` - Big-endian bindings
- `memorytracker_bpfel.o` - Compiled eBPF object file

### 2. Fix Compilation Errors

The code will likely have compilation errors. Here are common fixes:

```go
// In memory.go, you may need to fix:

// 1. The generated type name might be different
// Change: memorytrackerObjects
// To: memorytrackerObjects (or whatever bpf2go generates)

// 2. Import conflicts with protobuf types
// Add alias: protoevents "github.com/yairfalse/tapio/pkg/events"
// Use: protoevents.MemoryEvent for protobuf type

// 3. Missing eBPF program names
// Check generated files for actual program names like:
// - MemorytrackerTrackMemoryAlloc
// - MemorytrackerTrackMemoryFree
```

### 3. Build the Collector
```bash
# Build with eBPF tags
go build -tags ebpf ./...

# If successful, no output means it compiled
```

## Running Tests

### 1. Unit Tests (Requires Root)
```bash
# Run as root or with sudo
sudo go test -tags ebpf -v ./...

# Run specific test
sudo go test -tags ebpf -v -run TestMemoryCollector ./...
```

### 2. Performance Benchmarks
```bash
# Run benchmarks (requires root)
sudo go test -tags ebpf -bench=. -benchtime=5s ./...

# Run specific benchmark
sudo go test -tags ebpf -bench=BenchmarkMemoryEventProcessing ./...

# With memory profiling
sudo go test -tags ebpf -bench=. -memprofile=mem.prof ./...
```

### 3. Performance Target Validation
```bash
# Test against performance requirements
sudo go test -tags ebpf -run="TestPerformanceTargets" -v ./...
```

## Troubleshooting

### Common Issues and Solutions

#### 1. "operation not permitted" errors
```bash
# Run with sudo or add capabilities
sudo setcap cap_sys_admin,cap_bpf+eip $(which go)
```

#### 2. "eBPF is not available on this system"
```bash
# Check if eBPF is enabled
sudo mount -t bpf bpf /sys/fs/bpf
```

#### 3. "failed to load eBPF program"
```bash
# Check kernel logs for details
sudo dmesg | tail -50

# Common causes:
# - Kernel too old
# - Missing kernel headers
# - BPF verifier rejection
```

#### 4. Ring buffer not supported
```bash
# Kernel must be 5.8+
# Fallback to perf buffer implementation needed
```

## Integration Testing

### 1. Create Test Harness
```go
// test_integration.go
//go:build linux && ebpf

package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/yairfalse/tapio/pkg/collectors"
    ebpfcollector "github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

func main() {
    config := collectors.CollectorConfig{
        Name:            "test-collector",
        Enabled:         true,
        EventBufferSize: 1000,
        Debug:           true,
    }
    
    collector, err := ebpfcollector.NewMemoryCollector(config)
    if err != nil {
        log.Fatalf("Failed to create collector: %v", err)
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    if err := collector.Start(ctx); err != nil {
        log.Fatalf("Failed to start collector: %v", err)
    }
    
    // Read events
    go func() {
        for event := range collector.Events() {
            fmt.Printf("Event: %+v\n", event)
        }
    }()
    
    // Let it run
    <-ctx.Done()
    collector.Stop()
}
```

### 2. Run Integration Test
```bash
# Build and run
go build -tags ebpf -o test_collector test_integration.go
sudo ./test_collector

# In another terminal, trigger memory events
stress-ng --vm 1 --vm-bytes 128M --timeout 10s
```

## Validation Checklist

Before submitting a PR, ensure:

- [ ] **Builds successfully** with `go build -tags ebpf`
- [ ] **Unit tests pass** with `sudo go test -tags ebpf`
- [ ] **Benchmarks complete** without panics
- [ ] **Performance targets met**:
  - [ ] 50,000+ events/sec processing
  - [ ] <1ms OOM prediction latency
  - [ ] <100MB memory for 10k processes
- [ ] **Integration test** shows real events being captured
- [ ] **No kernel errors** in `dmesg`
- [ ] **Memory leak free** (run for extended period)

## Creating the PR

### 1. Document Your Testing
```bash
# Save test results
sudo go test -tags ebpf -v ./... > test_results.txt 2>&1
sudo go test -tags ebpf -bench=. ./... > bench_results.txt 2>&1

# Check for memory leaks
sudo go test -tags ebpf -run=TestMemoryCollector -memprofile=mem.prof -v
go tool pprof -text mem.prof > memory_analysis.txt
```

### 2. PR Description Template
```markdown
## eBPF Memory Collector Implementation

### What This PR Does
- Implements high-performance eBPF-based memory tracking
- Adds OOM prediction with ML algorithms
- Provides process lifecycle monitoring
- Integrates with unified message format

### Testing Done
- [ ] Tested on Linux kernel version: [YOUR VERSION]
- [ ] All unit tests passing
- [ ] Performance benchmarks meet targets
- [ ] Integration test captures real events
- [ ] No memory leaks detected
- [ ] Ran for [X] hours without issues

### Performance Results
- Event processing rate: [X] events/sec
- OOM prediction latency: [X]ms
- Memory usage for 10k processes: [X]MB
- CPU overhead: [X]%

### Test Environment
- OS: [Ubuntu 22.04/etc]
- Kernel: [version]
- Go version: [version]
- CPU: [model]
- Memory: [amount]

### Known Issues
- [List any known issues or limitations]

### Test Output
[Attach test_results.txt and bench_results.txt]
```

## Additional Testing Scripts

### 1. Automated Test Runner
```bash
#!/bin/bash
# save as: test_ebpf_collector.sh

set -euo pipefail

echo "=== eBPF Collector Test Suite ==="
echo "Kernel: $(uname -r)"
echo "Go: $(go version)"
echo ""

# Check prerequisites
if [[ $EUID -ne 0 ]]; then 
   echo "This script must be run as root" 
   exit 1
fi

# Build
echo "Building eBPF collector..."
cd pkg/collectors/ebpf
go generate ./memory.go || { echo "Failed to generate eBPF bindings"; exit 1; }
go build -tags ebpf ./... || { echo "Failed to build"; exit 1; }

# Test
echo "Running unit tests..."
go test -tags ebpf -v ./... || { echo "Tests failed"; exit 1; }

# Benchmark
echo "Running benchmarks..."
go test -tags ebpf -bench=. -benchtime=5s ./... || { echo "Benchmarks failed"; exit 1; }

# Performance targets
echo "Validating performance targets..."
go test -tags ebpf -run="TestPerformanceTargets" -v ./...

echo "=== All tests passed! ==="
```

### 2. Stress Test
```bash
#!/bin/bash
# save as: stress_test_ebpf.sh

# Run collector under load
echo "Starting stress test..."

# Start the collector in background
sudo ./test_collector &
COLLECTOR_PID=$!

# Generate load
stress-ng --vm 4 --vm-bytes 256M --fork 10 --timeout 60s &

# Monitor
for i in {1..60}; do
    echo "Time: $i seconds"
    ps aux | grep test_collector | grep -v grep
    sleep 1
done

# Cleanup
kill $COLLECTOR_PID
```

## Questions or Issues?

If you encounter issues during testing:

1. Check kernel logs: `sudo dmesg | tail -50`
2. Enable debug mode in the collector config
3. Use `bpftool` to inspect loaded programs: `sudo bpftool prog list`
4. Check GitHub issues or create a new one with:
   - Kernel version
   - Error messages
   - Test output
   - `dmesg` output

Good luck with testing! 🚀