#!/bin/bash

# Test script for REST API integration

echo "🔍 Testing REST API Integration..."

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to check command success
check_success() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ $1${NC}"
    else
        echo -e "${RED}✗ $1${NC}"
        exit 1
    fi
}

# Function to test endpoint
test_endpoint() {
    local method=$1
    local endpoint=$2
    local data=$3
    local expected_status=$4
    
    if [ -z "$data" ]; then
        response=$(curl -s -w "\n%{http_code}" -X $method http://localhost:8080$endpoint)
    else
        response=$(curl -s -w "\n%{http_code}" -X $method -H "Content-Type: application/json" -d "$data" http://localhost:8080$endpoint)
    fi
    
    status_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')
    
    if [ "$status_code" = "$expected_status" ]; then
        echo -e "${GREEN}✓ $method $endpoint - Status: $status_code${NC}"
        return 0
    else
        echo -e "${RED}✗ $method $endpoint - Expected: $expected_status, Got: $status_code${NC}"
        echo "Response: $body"
        return 1
    fi
}

# Build server
echo "Building tapio-server..."
go build -o tapio-server ./cmd/tapio-server/
check_success "Server build completed"

# Start server in background
echo -e "\n${YELLOW}Starting REST API server...${NC}"
./tapio-server --rest-enabled=true --rest-port=8080 --grpc-enabled=false &
SERVER_PID=$!

# Wait for server to start
sleep 3

# Test endpoints
echo -e "\n${YELLOW}Testing REST API endpoints...${NC}"

# Health check
test_endpoint "GET" "/health" "" "200"

# API v1 endpoints
test_endpoint "GET" "/api/v1/check" "" "200"
test_endpoint "GET" "/api/v1/check/default" "" "200"
test_endpoint "GET" "/api/v1/check/default/nginx" "" "200"
test_endpoint "GET" "/api/v1/findings" "" "200"
test_endpoint "GET" "/api/v1/status" "" "200"

# Test correlation
correlation_data='{
  "events": [
    {
      "id": "test-1",
      "type": "pod_restart",
      "source": "kubernetes",
      "severity": "warning",
      "timestamp": "2024-01-01T00:00:00Z"
    }
  ]
}'
test_endpoint "POST" "/api/v1/correlate" "$correlation_data" "200"

# Test finding submission
finding_data='{
  "id": "test-finding-1",
  "type": "test",
  "severity": "medium",
  "title": "Test Finding",
  "description": "This is a test",
  "timestamp": "2024-01-01T00:00:00Z"
}'
test_endpoint "POST" "/api/v1/findings" "$finding_data" "201"

# Test CLI in server mode
echo -e "\n${YELLOW}Testing CLI server mode...${NC}"

# Build CLI
go build -o tapio ./cmd/tapio/
check_success "CLI build completed"

# Test CLI health check
./tapio check --server --server-url http://localhost:8080
check_success "CLI server mode check"

# Cleanup
echo -e "\n${YELLOW}Cleaning up...${NC}"
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

echo -e "\n${GREEN}✓ REST API integration test completed successfully!${NC}"