#!/bin/bash

set -e

echo "🚀 NATS Staging Test Environment Setup"
echo "======================================"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if NATS is running
check_nats() {
    echo -n "Checking NATS server status... "
    if nc -z localhost 4222 2>/dev/null; then
        echo -e "${GREEN}✓ NATS is running on port 4222${NC}"
        return 0
    else
        echo -e "${RED}✗ NATS is not running${NC}"
        return 1
    fi
}

# Start NATS if not running
start_nats() {
    echo "Starting NATS server with JetStream..."
    
    # Check if nats-server is installed
    if ! command -v nats-server &> /dev/null; then
        echo -e "${YELLOW}nats-server not found. Installing...${NC}"
        if [[ "$OSTYPE" == "darwin"* ]]; then
            brew install nats-server
        else
            echo "Please install nats-server manually"
            exit 1
        fi
    fi
    
    # Start NATS with JetStream enabled
    nats-server -js &
    NATS_PID=$!
    echo "NATS PID: $NATS_PID"
    
    # Wait for NATS to be ready
    sleep 2
    
    # Save PID for cleanup
    echo $NATS_PID > /tmp/nats-test.pid
}

# Setup JetStream streams
setup_streams() {
    echo -e "\n${YELLOW}Setting up JetStream streams...${NC}"
    
    # Check if nats CLI is installed
    if ! command -v nats &> /dev/null; then
        echo -e "${YELLOW}nats CLI not found. Installing...${NC}"
        if [[ "$OSTYPE" == "darwin"* ]]; then
            brew install nats-io/nats-tools/nats
        else
            echo "Please install nats CLI manually"
            exit 1
        fi
    fi
    
    # Create OBSERVATIONS stream
    nats stream add OBSERVATIONS \
        --subjects "observations.>" \
        --storage file \
        --retention limits \
        --max-age 24h \
        --max-bytes 1GB \
        --replicas 1 \
        --no-deny-delete \
        --discard old \
        --dupe-window 2m \
        --defaults 2>/dev/null || echo "Stream might already exist"
    
    echo -e "${GREEN}✓ Stream OBSERVATIONS configured${NC}"
    
    # Create consumer for loader
    nats consumer add OBSERVATIONS loader \
        --pull \
        --deliver all \
        --ack-policy explicit \
        --replay instant \
        --filter-subjects "observations.>" \
        --max-deliver 3 \
        --defaults 2>/dev/null || echo "Consumer might already exist"
    
    echo -e "${GREEN}✓ Consumer 'loader' configured${NC}"
}

# Check Neo4j
check_neo4j() {
    echo -e "\n${YELLOW}Checking Neo4j...${NC}"
    if nc -z localhost 7687 2>/dev/null; then
        echo -e "${GREEN}✓ Neo4j is running on port 7687${NC}"
        return 0
    else
        echo -e "${RED}✗ Neo4j is not running${NC}"
        echo "  Start Neo4j with: docker run -d -p 7474:7474 -p 7687:7687 -e NEO4J_AUTH=neo4j/password neo4j:latest"
        return 1
    fi
}

# Main execution
main() {
    echo "Starting NATS staging environment setup..."
    echo ""
    
    # Check and start NATS
    if ! check_nats; then
        start_nats
        sleep 2
        check_nats || exit 1
    fi
    
    # Setup streams
    setup_streams
    
    # Check Neo4j
    check_neo4j
    
    echo -e "\n${GREEN}======================================${NC}"
    echo -e "${GREEN}✓ Staging environment ready!${NC}"
    echo -e "${GREEN}======================================${NC}"
    echo ""
    echo "You can now:"
    echo "1. Run the simple loader: go run cmd/simple-loader/main.go"
    echo "2. Publish test events: go run test/e2e/publish_test_events.go"
    echo "3. Monitor NATS: nats stream view OBSERVATIONS"
    echo ""
    echo "To stop NATS server (if started by this script):"
    echo "  kill \$(cat /tmp/nats-test.pid)"
}

# Trap to cleanup on exit
cleanup() {
    if [ -f /tmp/nats-test.pid ]; then
        echo -e "\n${YELLOW}Cleaning up...${NC}"
        kill $(cat /tmp/nats-test.pid) 2>/dev/null || true
        rm /tmp/nats-test.pid
    fi
}

# Only cleanup if we started NATS
if [ "$1" == "stop" ]; then
    cleanup
    exit 0
fi

# Run main
main