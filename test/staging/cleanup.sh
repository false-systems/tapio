#!/bin/bash

echo "🧹 Cleaning up NATS staging test files"
echo "======================================"

# Stop NATS if we started it
if [ -f /tmp/nats-test.pid ]; then
    echo "Stopping NATS server..."
    kill $(cat /tmp/nats-test.pid) 2>/dev/null || true
    rm /tmp/nats-test.pid
    echo "✓ NATS server stopped"
fi

# Remove test files
echo "Removing test files..."
rm -f /Users/yair/projects/tapio/test/staging/nats-test.sh
rm -f /Users/yair/projects/tapio/test/staging/nats_integration_test.go
rm -f /Users/yair/projects/tapio/test/staging/nats-monitor.sh
rm -f /Users/yair/projects/tapio/test/staging/cleanup.sh

# Remove the staging directory if empty
rmdir /Users/yair/projects/tapio/test/staging 2>/dev/null || true

echo "✓ Cleanup complete"