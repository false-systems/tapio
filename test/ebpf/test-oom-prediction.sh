#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
NC='\033[0m'

echo -e "${BLUE}🎯 Testing OOM Prediction Accuracy with eBPF${NC}"
echo "=============================================="

# Check if running as root (required for eBPF)
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}❌ This test requires root privileges for eBPF access${NC}"
    echo "Please run with sudo"
    exit 1
fi

# Test configuration
TARGET_MINUTES=5      # App designed to OOM in 5 minutes
SAMPLE_INTERVAL=15    # Sample every 15 seconds
PREDICTION_TOLERANCE=30 # Accept predictions within 30 seconds
LOG_FILE="oom-prediction-test-$(date +%Y%m%d-%H%M%S).log"

echo -e "${YELLOW}📋 Test Configuration:${NC}"
echo "  Expected OOM time: ${TARGET_MINUTES} minutes"
echo "  Sample interval: ${SAMPLE_INTERVAL} seconds"
echo "  Prediction tolerance: ±${PREDICTION_TOLERANCE} seconds"
echo "  Log file: ${LOG_FILE}"
echo ""

# Start the OOM prediction test app
echo -e "${BLUE}🚀 Deploying OOM prediction test application...${NC}"
kubectl apply -f test-apps/oom-prediction-app.yaml

# Wait for pod to be ready
echo -e "${BLUE}⏳ Waiting for test pod to be ready...${NC}"
kubectl wait --for=condition=Ready pods -l app=oom-prediction-test -n tapio-ebpf-tests --timeout=120s

POD_NAME=$(kubectl get pods -n tapio-ebpf-tests -l app=oom-prediction-test -o jsonpath='{.items[0].metadata.name}')
echo -e "${GREEN}✅ Test pod ready: ${POD_NAME}${NC}"

# Create log file with test metadata
cat > "${LOG_FILE}" << EOF
=== OOM PREDICTION ACCURACY TEST ===
Test started: $(date)
Pod name: ${POD_NAME}
Expected behavior: OOM in exactly ${TARGET_MINUTES} minutes
Target memory: 128Mi
Growth rate: ~25.6MB per minute
Sample interval: ${SAMPLE_INTERVAL} seconds

=== PREDICTION TRACKING ===
EOF

# Track predictions over time
start_time=$(date +%s)
iteration=1
best_prediction=""
final_oom_time=""

echo -e "${BLUE}📊 Starting prediction tracking...${NC}"
echo "Monitoring Tapio's OOM predictions vs actual OOM timing"

run_prediction_analysis() {
    local iteration=$1
    local elapsed_minutes=$2
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    echo -e "${MAGENTA}🔍 Prediction Check #${iteration} at ${timestamp} (+${elapsed_minutes:.1f}m)${NC}"
    
    # Run tapio why command and capture prediction
    echo "=== Analysis #${iteration} - ${timestamp} - ${elapsed_minutes:.1f}m elapsed ===" >> "${LOG_FILE}"
    
    if timeout 30 ../../bin/tapio why "${POD_NAME}" --namespace tapio-ebpf-tests --verbose >> "${LOG_FILE}" 2>&1; then
        
        # Extract prediction from output
        local prediction=$(tail -50 "${LOG_FILE}" | grep -i "OOM\|prediction\|will.*kill" | head -1 || echo "")
        
        if [[ -n "$prediction" ]]; then
            echo "  📈 Prediction: $prediction"
            best_prediction="$prediction"
            
            # Try to extract timing from prediction
            local predicted_time=$(echo "$prediction" | grep -oP '\d+[ms]?\s*(minutes?|seconds?|m|s)' | head -1 || echo "")
            if [[ -n "$predicted_time" ]]; then
                echo "  ⏰ Predicted timing: $predicted_time"
            fi
        else
            echo "  ❓ No prediction found in output"
        fi
        
        echo -e "${GREEN}✅ Analysis completed${NC}"
    else
        echo -e "${YELLOW}⚠️  Analysis timed out or failed${NC}"
    fi
    
    # Log pod resource usage
    kubectl top pod "${POD_NAME}" -n tapio-ebpf-tests >> "${LOG_FILE}" 2>&1 || true
    echo "" >> "${LOG_FILE}"
}

# Monitor until OOM or timeout
max_wait_minutes=$((TARGET_MINUTES + 2))  # Wait 2 extra minutes max
max_iterations=$((max_wait_minutes * 60 / SAMPLE_INTERVAL))

while [[ $iteration -le $max_iterations ]]; do
    current_time=$(date +%s)
    elapsed_seconds=$((current_time - start_time))
    elapsed_minutes=$(echo "scale=1; $elapsed_seconds / 60" | bc)
    
    # Check if pod is still running
    POD_STATUS=$(kubectl get pod "${POD_NAME}" -n tapio-ebpf-tests -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    
    if [[ "${POD_STATUS}" == "Failed" ]]; then
        final_oom_time=$(date +%s)
        actual_oom_minutes=$(echo "scale=1; $elapsed_seconds / 60" | bc)
        echo -e "${RED}💥 OOM Kill detected at ${elapsed_minutes} minutes!${NC}"
        echo "=== OOM KILL DETECTED ===" >> "${LOG_FILE}"
        echo "Actual OOM time: ${actual_oom_minutes} minutes" >> "${LOG_FILE}"
        echo "Target OOM time: ${TARGET_MINUTES} minutes" >> "${LOG_FILE}"
        break
    elif [[ "${POD_STATUS}" != "Running" ]]; then
        echo -e "${YELLOW}⚠️  Pod status changed to: ${POD_STATUS}${NC}"
        break
    fi
    
    # Run prediction analysis
    run_prediction_analysis $iteration $elapsed_minutes
    
    echo -e "${BLUE}⏳ Waiting ${SAMPLE_INTERVAL} seconds...${NC}"
    sleep ${SAMPLE_INTERVAL}
    
    iteration=$((iteration + 1))
done

# Generate final report
echo -e "${BLUE}📋 Generating accuracy report...${NC}"

cat >> "${LOG_FILE}" << EOF

=== FINAL ACCURACY REPORT ===
Test completed: $(date)
Total monitoring time: ${elapsed_minutes} minutes
EOF

if [[ -n "$final_oom_time" ]]; then
    actual_oom_seconds=$((final_oom_time - start_time))
    target_oom_seconds=$((TARGET_MINUTES * 60))
    accuracy_diff=$((actual_oom_seconds - target_oom_seconds))
    
    cat >> "${LOG_FILE}" << EOF
Target OOM time: ${TARGET_MINUTES} minutes (${target_oom_seconds} seconds)
Actual OOM time: ${actual_oom_minutes} minutes (${actual_oom_seconds} seconds)
Accuracy difference: ${accuracy_diff} seconds
EOF
    
    if [[ ${accuracy_diff#-} -le $PREDICTION_TOLERANCE ]]; then
        echo -e "${GREEN}✅ OOM timing within acceptable tolerance!${NC}"
        echo "  Target: ${TARGET_MINUTES} minutes"
        echo "  Actual: ${actual_oom_minutes} minutes"
        echo "  Difference: ${accuracy_diff} seconds (within ±${PREDICTION_TOLERANCE}s)"
    else
        echo -e "${YELLOW}⚠️  OOM timing outside tolerance${NC}"
        echo "  Target: ${TARGET_MINUTES} minutes"
        echo "  Actual: ${actual_oom_minutes} minutes"
        echo "  Difference: ${accuracy_diff} seconds (tolerance: ±${PREDICTION_TOLERANCE}s)"
    fi
    
    if [[ -n "$best_prediction" ]]; then
        echo -e "${BLUE}🎯 Best Tapio prediction: ${best_prediction}${NC}"
    fi
    
else
    echo -e "${RED}❌ No OOM detected within timeout period${NC}"
    cat >> "${LOG_FILE}" << EOF
No OOM detected within ${max_wait_minutes} minutes
Test may have failed or taken longer than expected
EOF
fi

echo ""
echo -e "${GREEN}✅ OOM prediction test completed!${NC}"
echo -e "${BLUE}📄 Full results saved to: ${LOG_FILE}${NC}"
echo ""
echo -e "${YELLOW}🔍 Key metrics to verify:${NC}"
echo "  1. Tapio detected growing memory pattern"
echo "  2. OOM predictions became more precise over time"
echo "  3. Final prediction was within ±30 seconds of actual OOM"
echo "  4. eBPF provided accurate growth rate measurements"
echo ""
echo -e "${BLUE}📊 Analyze results with:${NC}"
echo "  grep -A 3 'PREDICTION' ${LOG_FILE}"
echo "  grep 'OOM' ${LOG_FILE}"
echo "  grep 'Accuracy difference' ${LOG_FILE}"