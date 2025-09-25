#!/bin/bash
# Real eBPF test - trigger actual container events

echo "=== Real eBPF Container Runtime Test ==="
echo "This will create containers and trigger events to test the observer"
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then
   echo "Please run as root (sudo) for eBPF functionality"
   exit 1
fi

# Create a test program that runs the observer
cat > /tmp/test_real_observer.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/exec"
    "time"

    cr "github.com/yairfalse/tapio/internal/observers/container-runtime"
    "github.com/yairfalse/tapio/pkg/domain"
)

func main() {
    fmt.Println("=== Starting Real eBPF Observer Test ===")

    config := cr.NewDefaultConfig("real-test")
    config.EnableOOMKill = true
    config.EnableMemoryPressure = true
    config.EnableProcessExit = true
    config.EnableProcessFork = true

    observer, err := cr.NewObserver("real-test", config)
    if err != nil {
        log.Fatalf("Failed to create observer: %v", err)
    }

    ctx := context.Background()
    if err := observer.Start(ctx); err != nil {
        log.Fatalf("Failed to start observer: %v", err)
    }

    fmt.Println("✓ eBPF programs attached successfully!")

    // Listen for events
    events := observer.Events()
    go func() {
        for event := range events {
            fmt.Printf("\n🔔 EVENT CAPTURED!\n")
            fmt.Printf("   Type: %s\n", event.Type)
            fmt.Printf("   Source: %s\n", event.Source)
            fmt.Printf("   Severity: %s\n", event.Severity)
            if event.CorrelationHints != nil {
                fmt.Printf("   Container ID: %s\n", event.CorrelationHints.ContainerID)
                fmt.Printf("   Cgroup Path: %s\n", event.CorrelationHints.CgroupPath)
            }
            if containerData, ok := event.EventData.(domain.EventDataContainer); ok {
                if containerData.Process != nil {
                    fmt.Printf("   PID: %d\n", containerData.Process.PID)
                    fmt.Printf("   Command: %s\n", containerData.Process.Command)
                }
                if containerData.Container != nil && containerData.Container.ExitCode != nil {
                    fmt.Printf("   Exit Code: %d\n", *containerData.Container.ExitCode)
                }
            }
        }
    }()

    fmt.Println("\nObserver is running. Waiting for events...")
    fmt.Println("To test, run these in another terminal:")
    fmt.Println("  1. docker run --rm alpine echo 'test'")
    fmt.Println("  2. docker run --rm -m 10m alpine sh -c 'dd if=/dev/zero of=/dev/null bs=1M'")
    fmt.Println("  3. Kill a process: kill -9 <PID>")
    fmt.Println("\nPress Ctrl+C to stop...")

    // Run for 60 seconds or until interrupted
    time.Sleep(60 * time.Second)

    stats := observer.Statistics()
    fmt.Printf("\n=== Statistics ===\n")
    fmt.Printf("Events processed: %d\n", stats.EventsProcessed)
    fmt.Printf("Events dropped: %d\n", stats.EventsDropped)
    fmt.Printf("Errors: %d\n", stats.ErrorCount)

    if err := observer.Stop(); err != nil {
        log.Printf("Error stopping observer: %v", err)
    }

    fmt.Println("✓ Observer stopped successfully")
}
EOF

echo "=== Step 1: Check Docker/Containerd ==="
if command -v docker &> /dev/null; then
    echo "✓ Docker found"
    RUNTIME="docker"
elif command -v containerd &> /dev/null; then
    echo "✓ Containerd found"
    RUNTIME="containerd"
else
    echo "⚠️  No container runtime found. Install Docker or Containerd to test real events"
    RUNTIME="none"
fi

echo
echo "=== Step 2: Compile and Run Observer ==="
cd /tapio || cd /Users/yair/projects/tapio || exit 1

echo "Building observer test..."
go build -o /tmp/test_real_observer /tmp/test_real_observer.go
if [ $? -ne 0 ]; then
    echo "❌ Build failed"
    exit 1
fi

echo
echo "=== Step 3: Start Observer with Event Monitoring ==="
echo "Starting observer in background..."
/tmp/test_real_observer &
OBSERVER_PID=$!

sleep 3

echo
echo "=== Step 4: Trigger Test Events ==="

# Test 1: Simple process exit
echo "Test 1: Creating a process that exits normally..."
sleep 1 &
TEST_PID=$!
sleep 0.5
kill $TEST_PID 2>/dev/null

# Test 2: Force kill a process
echo "Test 2: Force killing a process..."
sleep 100 &
TEST_PID=$!
sleep 0.5
kill -9 $TEST_PID 2>/dev/null

# Test 3: Container test if Docker is available
if [ "$RUNTIME" = "docker" ]; then
    echo "Test 3: Running a container..."
    docker run --rm alpine echo "Hello from container" 2>/dev/null

    echo "Test 4: Running a container with memory limit..."
    docker run --rm -m 10m alpine sh -c 'echo "Memory test"' 2>/dev/null
fi

# Test 4: Fork bomb protection test (safe version)
echo "Test 5: Creating multiple child processes..."
for i in {1..5}; do
    (sleep 0.1) &
done
wait

echo
echo "=== Waiting for events to be processed ==="
sleep 5

# Stop the observer
echo "Stopping observer..."
kill -TERM $OBSERVER_PID 2>/dev/null
wait $OBSERVER_PID 2>/dev/null

echo
echo "=== Test Complete ==="
echo "Check the output above for captured events!"