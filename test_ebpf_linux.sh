#!/bin/bash
# Test script for eBPF functionality in Linux environment

echo "=== Testing Container Runtime Observer eBPF Integration ==="
echo

# Check if running as root (required for eBPF)
if [ "$EUID" -ne 0 ]; then
   echo "Please run as root (sudo) for eBPF functionality"
   exit 1
fi

# Check kernel version
echo "Kernel version:"
uname -r
echo

# Test compilation
echo "Building Container Runtime Observer..."
go build ./internal/observers/container-runtime/...
if [ $? -eq 0 ]; then
    echo "✓ Build successful"
else
    echo "✗ Build failed"
    exit 1
fi
echo

# Run tests
echo "Running tests..."
go test -v ./internal/observers/container-runtime/... 2>&1 | tee /tmp/container-runtime-test.log
TEST_RESULT=$?

echo
echo "=== Test Summary ==="
if [ $TEST_RESULT -eq 0 ]; then
    echo "✓ All tests passed!"
else
    echo "✗ Some tests failed. Check /tmp/container-runtime-test.log for details"
    echo
    echo "Common issues:"
    echo "- Struct size mismatch: Check BPFContainerExitEvent padding"
    echo "- kprobe attachment errors: Check kernel function availability"
    echo "- Permission errors: Run as root"
fi

# Check for specific eBPF programs
echo
echo "=== eBPF Program Status ==="
echo "Checking for required kernel functions..."

# Check for OOM kill function
if grep -q "oom_kill_process" /proc/kallsyms 2>/dev/null; then
    echo "✓ oom_kill_process found"
else
    echo "✗ oom_kill_process not found (OOM kill monitoring will fail)"
fi

# Check for memory cgroup OOM function
if grep -q "mem_cgroup_out_of_memory" /proc/kallsyms 2>/dev/null; then
    echo "✓ mem_cgroup_out_of_memory found"
else
    echo "✗ mem_cgroup_out_of_memory not found (memory pressure monitoring may fail)"
fi

# Check for tracepoints
if [ -d "/sys/kernel/debug/tracing/events/sched/sched_process_exit" ]; then
    echo "✓ sched:sched_process_exit tracepoint available"
else
    echo "✗ sched:sched_process_exit tracepoint not found"
fi

if [ -d "/sys/kernel/debug/tracing/events/sched/sched_process_fork" ]; then
    echo "✓ sched:sched_process_fork tracepoint available"
else
    echo "✗ sched:sched_process_fork tracepoint not found"
fi

echo
echo "=== Quick eBPF Test ==="
echo "Creating a minimal eBPF test program..."

# Create test directory
mkdir -p /tmp/ebpf-test
cd /tmp/ebpf-test

# Create a simple test program
cat > test_observer.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    cr "github.com/yairfalse/tapio/internal/observers/container-runtime"
)

func main() {
    fmt.Println("Starting Container Runtime Observer with eBPF...")

    config := cr.NewDefaultConfig("ebpf-test")
    config.EnableOOMKill = true
    config.EnableMemoryPressure = true
    config.EnableProcessExit = true
    config.EnableProcessFork = true

    observer, err := cr.NewObserver("ebpf-test", config)
    if err != nil {
        log.Fatalf("Failed to create observer: %v", err)
    }

    ctx := context.Background()
    if err := observer.Start(ctx); err != nil {
        log.Fatalf("Failed to start observer: %v", err)
    }

    fmt.Println("✓ eBPF programs attached successfully!")
    fmt.Println("Observer is running. Press Ctrl+C to stop...")

    // Run for 5 seconds
    time.Sleep(5 * time.Second)

    stats := observer.Statistics()
    fmt.Printf("Events processed: %d\n", stats.EventsProcessed)
    fmt.Printf("Errors: %d\n", stats.ErrorCount)

    if err := observer.Stop(); err != nil {
        log.Printf("Error stopping observer: %v", err)
    }

    fmt.Println("✓ Observer stopped successfully")
}
EOF

echo "Running eBPF test program..."
cd /tapio
go run /tmp/ebpf-test/test_observer.go

echo
echo "=== Test Complete ===""