#!/bin/bash

# Verify OTEL integration is working

echo "=== Verifying OTEL Integration ==="
echo

# Run the test and capture output
output=$(go run ./cmd/test-otel/ 2>&1)

# Check for successful completion
if echo "$output" | grep -q "OTEL shutdown complete"; then
    echo "✓ OTEL shutdown completed successfully"
else
    echo "✗ OTEL shutdown failed"
    exit 1
fi

# Check for trace IDs
trace_count=$(echo "$output" | grep -c "Trace ID:")
if [ "$trace_count" -ge 4 ]; then
    echo "✓ Found $trace_count trace IDs (expected at least 4)"
else
    echo "✗ Only found $trace_count trace IDs (expected at least 4)"
    exit 1
fi

# Check for span IDs
span_count=$(echo "$output" | grep -c "Span ID:")
if [ "$span_count" -ge 5 ]; then
    echo "✓ Found $span_count span IDs (expected at least 5)"
else
    echo "✗ Only found $span_count span IDs (expected at least 5)"
    exit 1
fi

# Check for trace propagation
if echo "$output" | grep -q "Trace propagation demonstrated across collectors"; then
    echo "✓ Trace propagation verified"
else
    echo "✗ Trace propagation not verified"
    exit 1
fi

# Check for error handling
if echo "$output" | grep -q "Error scenario traced"; then
    echo "✓ Error handling with tracing verified"
else
    echo "✗ Error handling not verified"
    exit 1
fi

echo
echo "=== All OTEL Integration Tests Passed ✓ ==="