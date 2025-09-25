#!/bin/bash
# Test real container events with the eBPF observer

echo "=== Real Container Event Test ==="
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then
   echo "Please run as root (sudo) for eBPF functionality"
   exit 1
fi

# Check container runtime
if command -v docker &> /dev/null; then
    echo "✓ Docker found"
    RUNTIME="docker"
elif command -v containerd &> /dev/null; then
    echo "✓ Containerd found"
    RUNTIME="containerd"
else
    echo "⚠️  No container runtime found"
    RUNTIME="none"
fi

echo
echo "=== Building Test Program ==="
cd /tapio 2>/dev/null || cd /Users/yair/projects/tapio || exit 1

# Build the test program
go build -o /tmp/container_observer ./internal/observers/container-runtime/test_real_ebpf.go
if [ $? -ne 0 ]; then
    echo "❌ Build failed"
    exit 1
fi
echo "✓ Build successful"

echo
echo "=== Starting Observer ==="
# Run observer in background and capture its PID
/tmp/container_observer &
OBSERVER_PID=$!

# Wait for observer to initialize
sleep 3

echo
echo "=== Triggering Test Events ==="

# Test 1: Create and kill a simple process
echo "Test 1: Process exit event..."
(sleep 30) &
SLEEP_PID=$!
sleep 0.5
echo "  Killing process $SLEEP_PID"
kill -9 $SLEEP_PID 2>/dev/null
sleep 1

# Test 2: Multiple process creation (fork events)
echo "Test 2: Fork events..."
for i in {1..3}; do
    (echo "Child $i"; sleep 0.1) &
done
wait
sleep 1

# Test 3: Docker container if available
if [ "$RUNTIME" = "docker" ]; then
    echo "Test 3: Docker container events..."

    # Simple container exit
    echo "  Running alpine container..."
    docker run --rm --name test1 alpine echo "Hello from container"
    sleep 1

    # Container with specific exit code
    echo "  Running container with exit code..."
    docker run --rm --name test2 alpine sh -c 'exit 42'
    sleep 1

    # Container with memory limit
    echo "  Running container with memory limit..."
    docker run --rm -m 10m --name test3 alpine sh -c 'echo "Memory test"; sleep 1'
    sleep 2
fi

# Test 4: Stress test - many processes
echo "Test 4: Many process events..."
for i in {1..10}; do
    (sleep 0.05) &
done
wait

echo
echo "=== Waiting for event processing ==="
sleep 5

echo
echo "=== Checking Results ==="

# Send SIGTERM to observer to get final stats
kill -TERM $OBSERVER_PID 2>/dev/null

# Wait for observer to exit and show its output
wait $OBSERVER_PID

echo
echo "=== Test Complete ==="

# Also check kernel logs for any eBPF errors
echo
echo "=== Checking Kernel Logs ==="
dmesg | tail -20 | grep -E "bpf|BPF|cri_monitor" || echo "No eBPF errors in kernel log"