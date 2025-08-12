#!/bin/bash
# Test script to verify NATS integration for all collectors

set -e

echo "=== Testing Tapio NATS Integration ==="

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if NATS is running
echo -n "Checking NATS connection... "
if ! nats server check connection 2>/dev/null; then
    echo -e "${RED}FAILED${NC}"
    echo "Please start NATS first with: docker run -d --name nats -p 4222:4222 nats:latest -js"
    exit 1
fi
echo -e "${GREEN}OK${NC}"

# Create streams if they don't exist
echo -n "Creating NATS streams... "
nats stream add RAW_EVENTS \
    --subjects="raw.>" \
    --storage=file \
    --retention=limits \
    --max-age=24h \
    --max-bytes=1GB \
    --replicas=1 \
    --no-deny-delete \
    --no-deny-purge \
    2>/dev/null || true

nats stream add TRACES \
    --subjects="traces.>" \
    --storage=file \
    --retention=limits \
    --max-age=24h \
    --max-bytes=1GB \
    --replicas=1 \
    --no-deny-delete \
    --no-deny-purge \
    2>/dev/null || true
echo -e "${GREEN}OK${NC}"

# Start collectors in background
echo "Starting collectors..."
./tapio-collectors \
    --nats=nats://localhost:4222 \
    --log-level=debug \
    --enable-kubeapi=true \
    --enable-systemd=false \
    --enable-ebpf=false \
    --enable-etcd=false \
    --enable-cni=false \
    &
COLLECTOR_PID=$!

# Give collectors time to start
sleep 5

# Subscribe to raw events
echo -e "\n${YELLOW}Monitoring raw events (press Ctrl+C to stop)...${NC}\n"
echo "Waiting for events from collectors..."

# Create a consumer to monitor events
nats consumer add RAW_EVENTS test-consumer \
    --filter="raw.>" \
    --ack=none \
    --deliver=all \
    --replay=instant \
    --no-headers \
    2>/dev/null || true

# Subscribe and display events
timeout 30s nats consumer sub RAW_EVENTS test-consumer \
    --raw \
    | while read -r line; do
        echo -e "${GREEN}[EVENT]${NC} $line"
        echo "$line" | jq -C '.' 2>/dev/null || echo "$line"
        echo "---"
    done

# Cleanup
echo -e "\n${YELLOW}Cleaning up...${NC}"
kill $COLLECTOR_PID 2>/dev/null || true
nats consumer rm RAW_EVENTS test-consumer -f 2>/dev/null || true

echo -e "\n${GREEN}Test completed!${NC}"
echo "If you saw events above, NATS integration is working correctly."