#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
NC='\033[0m'

echo -e "${BLUE}🧪 Testing Memory Leak Detection with eBPF${NC}"
echo "==============================================="

# Check if running as root (required for eBPF)
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}❌ This test requires root privileges for eBPF access${NC}"
    echo "Please run with sudo"
    exit 1
fi

# Test configuration
TEST_DURATION=300  # 5 minutes
SAMPLE_INTERVAL=30 # Sample every 30 seconds
LOG_FILE="memory-leak-test-$(date +%Y%m%d-%H%M%S).log"

echo -e "${YELLOW}📋 Test Configuration:${NC}"
echo "  Duration: ${TEST_DURATION} seconds (5 minutes)"
echo "  Sample interval: ${SAMPLE_INTERVAL} seconds"
echo "  Log file: ${LOG_FILE}"
echo ""

# Start the memory leak test app
echo -e "${BLUE}🚀 Deploying memory leak test application...${NC}"
kubectl apply -f test-apps/memory-leak-app.yaml

# Wait for pod to be ready
echo -e "${BLUE}⏳ Waiting for test pod to be ready...${NC}"
kubectl wait --for=condition=Ready pods -l app=memory-leak-test -n tapio-ebpf-tests --timeout=120s

POD_NAME=$(kubectl get pods -n tapio-ebpf-tests -l app=memory-leak-test -o jsonpath='{.items[0].metadata.name}')
echo -e "${GREEN}✅ Test pod ready: ${POD_NAME}${NC}"

# Function to run tapio and capture results
run_tapio_analysis() {
    local iteration=$1
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    echo -e "${MAGENTA}🔍 Analysis #${iteration} at ${timestamp}${NC}"
    
    # Run tapio why command
    echo "=== Tapio Why Analysis #${iteration} - ${timestamp} ===" >> "${LOG_FILE}"
    
    if timeout 30 ../../bin/tapio why "${POD_NAME}" --namespace tapio-ebpf-tests --verbose >> "${LOG_FILE}" 2>&1; then
        echo -e "${GREEN}✅ Analysis #${iteration} completed${NC}"
    else
        echo -e "${YELLOW}⚠️  Analysis #${iteration} timed out or failed${NC}"
    fi
    
    # Also run check command for health status
    echo "=== Tapio Check Analysis #${iteration} - ${timestamp} ===" >> "${LOG_FILE}"
    timeout 30 ../../bin/tapio check --namespace tapio-ebpf-tests --verbose >> "${LOG_FILE}" 2>&1 || true
    
    # Get pod resource usage
    echo "=== Pod Resource Usage #${iteration} - ${timestamp} ===" >> "${LOG_FILE}"
    kubectl top pod "${POD_NAME}" -n tapio-ebpf-tests >> "${LOG_FILE}" 2>&1 || echo "kubectl top not available" >> "${LOG_FILE}"
    
    # Get pod events
    echo "=== Pod Events #${iteration} - ${timestamp} ===" >> "${LOG_FILE}"
    kubectl get events -n tapio-ebpf-tests --field-selector involvedObject.name="${POD_NAME}" >> "${LOG_FILE}" 2>&1
    
    echo "" >> "${LOG_FILE}"
}

# Run continuous monitoring
echo -e "${BLUE}📊 Starting continuous monitoring...${NC}"
echo "Press Ctrl+C to stop early"

start_time=$(date +%s)
iteration=1

# Create initial log entry
echo "=== Memory Leak Detection Test Started ===" > "${LOG_FILE}"
echo "Test started: $(date)" >> "${LOG_FILE}"
echo "Pod name: ${POD_NAME}" >> "${LOG_FILE}"
echo "Expected behavior: Memory leak of ~10MB every 30 seconds" >> "${LOG_FILE}"
echo "Memory limit: 500Mi" >> "${LOG_FILE}"
echo "Expected OOM: After ~25 minutes (50 iterations)" >> "${LOG_FILE}"
echo "" >> "${LOG_FILE}"

while true; do
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    if [[ ${elapsed} -ge ${TEST_DURATION} ]]; then
        echo -e "${YELLOW}⏰ Test duration reached${NC}"
        break
    fi
    
    run_tapio_analysis ${iteration}
    
    # Check if pod is still running
    if ! kubectl get pod "${POD_NAME}" -n tapio-ebpf-tests &>/dev/null; then
        echo -e "${YELLOW}⚠️  Pod no longer exists - may have been OOMKilled${NC}"
        break
    fi
    
    # Check pod status
    POD_STATUS=$(kubectl get pod "${POD_NAME}" -n tapio-ebpf-tests -o jsonpath='{.status.phase}')
    if [[ "${POD_STATUS}" != "Running" ]]; then
        echo -e "${YELLOW}⚠️  Pod status changed to: ${POD_STATUS}${NC}"
        if [[ "${POD_STATUS}" == "Failed" ]]; then
            echo -e "${RED}💥 Pod failed - likely OOMKilled!${NC}"
            break
        fi
    fi
    
    echo -e "${BLUE}⏳ Waiting ${SAMPLE_INTERVAL} seconds until next analysis...${NC}"
    sleep ${SAMPLE_INTERVAL}
    
    iteration=$((iteration + 1))
done

# Final analysis
echo -e "${BLUE}📋 Generating final test report...${NC}"

echo "" >> "${LOG_FILE}"
echo "=== FINAL TEST REPORT ===" >> "${LOG_FILE}"
echo "Test completed: $(date)" >> "${LOG_FILE}"
echo "Total iterations: $((iteration - 1))" >> "${LOG_FILE}"
echo "Total elapsed time: ${elapsed} seconds" >> "${LOG_FILE}"

# Check final pod status
FINAL_STATUS=$(kubectl get pod "${POD_NAME}" -n tapio-ebpf-tests -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
echo "Final pod status: ${FINAL_STATUS}" >> "${LOG_FILE}"

# Get final events
echo "" >> "${LOG_FILE}"
echo "=== FINAL POD EVENTS ===" >> "${LOG_FILE}"
kubectl get events -n tapio-ebpf-tests --field-selector involvedObject.name="${POD_NAME}" --sort-by='.lastTimestamp' >> "${LOG_FILE}" 2>&1

echo -e "${GREEN}✅ Memory leak test completed!${NC}"
echo -e "${BLUE}📄 Full results saved to: ${LOG_FILE}${NC}"
echo ""
echo -e "${YELLOW}🔍 Key things to verify in the log:${NC}"
echo "  1. eBPF detected memory leak pattern"
echo "  2. Confidence scores increased over time"
echo "  3. OOM predictions became more accurate"
echo "  4. Process PID correlation worked correctly"
echo "  5. Syscall patterns showed malloc/free imbalance"
echo ""
echo -e "${BLUE}📊 View results with:${NC}"
echo "  cat ${LOG_FILE} | grep -A 5 'Memory leak detected'"
echo "  cat ${LOG_FILE} | grep -A 5 'PREDICTION'"
echo "  cat ${LOG_FILE} | grep 'eBPF Reality'"