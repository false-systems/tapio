#!/bin/bash
# Test script for kernel observer with OTEL instrumentation

echo "========================================"
echo "Running Kernel Observer Tests with OTEL"
echo "========================================"

# Run all kernel tests with extended timeout
echo ""
echo "1. Running all tests with race detection..."
sudo go test -v -race ./internal/observers/kernel -timeout 120s

# Check if we're on Linux for eBPF tests
if [[ "$(uname)" == "Linux" ]]; then
    echo ""
    echo "2. Running E2E tests with real eBPF..."
    sudo go test -v -run TestE2E ./internal/observers/kernel -timeout 120s

    echo ""
    echo "3. Running performance tests..."
    sudo go test -v -run TestE2EHighVolume ./internal/observers/kernel -timeout 120s
else
    echo ""
    echo "2. Running mock mode tests (non-Linux platform)..."
    TAPIO_MOCK_MODE=true go test -v -run TestE2EMock ./internal/observers/kernel -timeout 60s
fi

echo ""
echo "4. Checking test coverage..."
go test -cover ./internal/observers/kernel

echo ""
echo "========================================"
echo "Test Summary Complete"
echo "========================================"