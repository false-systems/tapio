#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}🧪 Tapio eBPF Complete Test Suite${NC}"
echo "=================================="
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}❌ eBPF tests require root privileges${NC}"
    echo "Please run with sudo"
    exit 1
fi

# Test configuration
REPORT_DIR="test-results-$(date +%Y%m%d-%H%M%S)"
mkdir -p "${REPORT_DIR}"

echo -e "${BLUE}📁 Test results will be saved to: ${REPORT_DIR}${NC}"
echo ""

# Function to run a test and capture results
run_test() {
    local test_name="$1"
    local test_script="$2"
    local description="$3"
    
    echo -e "${MAGENTA}🚀 Running ${test_name}...${NC}"
    echo -e "${BLUE}   ${description}${NC}"
    echo ""
    
    local start_time=$(date +%s)
    local success=true
    
    if timeout 1800 bash "${test_script}" > "${REPORT_DIR}/${test_name}.output" 2>&1; then
        echo -e "${GREEN}✅ ${test_name} completed successfully${NC}"
    else
        echo -e "${RED}❌ ${test_name} failed or timed out${NC}"
        success=false
    fi
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo "   Duration: ${duration} seconds"
    echo ""
    
    # Create summary
    cat >> "${REPORT_DIR}/test-summary.txt" << EOF
=== ${test_name} ===
Description: ${description}
Start time: $(date -d @${start_time})
End time: $(date -d @${end_time})
Duration: ${duration} seconds
Status: $(if $success; then echo "PASSED"; else echo "FAILED"; fi)

EOF

    return $(if $success; then echo 0; else echo 1; fi)
}

# Clean up any existing test apps
echo -e "${YELLOW}🧹 Cleaning up any existing test resources...${NC}"
kubectl delete namespace tapio-ebpf-tests --ignore-not-found=true
kubectl create namespace tapio-ebpf-tests

echo -e "${BLUE}📋 Test Suite Overview:${NC}"
echo "  1. Memory Leak Detection Test (~5-10 minutes)"
echo "  2. OOM Prediction Accuracy Test (~7 minutes)" 
echo "  3. Multi-Container Process Correlation Test (~5 minutes)"
echo ""
echo -e "${YELLOW}⏰ Total estimated time: 15-20 minutes${NC}"
echo ""

read -p "Press Enter to start the test suite, or Ctrl+C to cancel..."
echo ""

# Initialize summary file
cat > "${REPORT_DIR}/test-summary.txt" << EOF
TAPIO eBPF TEST SUITE RESULTS
============================
Started: $(date)
Test environment: $(uname -a)
Kubernetes cluster: $(kubectl config current-context)

EOF

# Test 1: Memory Leak Detection
run_test "memory-leak-detection" \
         "./test-memory-leak.sh" \
         "Tests eBPF's ability to detect memory leaks with confidence scoring"

# Test 2: OOM Prediction Accuracy  
run_test "oom-prediction-accuracy" \
         "./test-oom-prediction.sh" \
         "Validates precision of OOM timing predictions using eBPF data"

# Test 3: Process Correlation (if we have the script)
if [[ -f "./test-process-correlation.sh" ]]; then
    run_test "process-correlation" \
             "./test-process-correlation.sh" \
             "Tests mapping of container processes to Kubernetes pods"
fi

# Generate final report
echo -e "${CYAN}📊 Generating comprehensive test report...${NC}"

cat >> "${REPORT_DIR}/test-summary.txt" << EOF

=== OVERALL RESULTS ===
Test suite completed: $(date)
EOF

# Count passed/failed tests
passed_tests=$(grep -c "Status: PASSED" "${REPORT_DIR}/test-summary.txt" || echo "0")
failed_tests=$(grep -c "Status: FAILED" "${REPORT_DIR}/test-summary.txt" || echo "0")
total_tests=$((passed_tests + failed_tests))

cat >> "${REPORT_DIR}/test-summary.txt" << EOF
Total tests: ${total_tests}
Passed: ${passed_tests}
Failed: ${failed_tests}
Success rate: $(echo "scale=1; $passed_tests * 100 / $total_tests" | bc)%
EOF

# Display final results
echo ""
echo -e "${CYAN}🎉 Test Suite Complete!${NC}"
echo "========================"
echo ""
echo -e "${BLUE}📊 Results Summary:${NC}"
echo "  Total tests: ${total_tests}"
echo "  Passed: ${GREEN}${passed_tests}${NC}"
echo "  Failed: ${RED}${failed_tests}${NC}"

if [[ $failed_tests -eq 0 ]]; then
    echo -e "${GREEN}✅ All tests passed! eBPF integration is working correctly.${NC}"
else
    echo -e "${YELLOW}⚠️  Some tests failed. Check individual test outputs for details.${NC}"
fi

echo ""
echo -e "${BLUE}📁 Detailed results saved to: ${REPORT_DIR}/${NC}"
echo -e "${BLUE}📄 View summary: cat ${REPORT_DIR}/test-summary.txt${NC}"
echo ""

# Show what to check in the results
echo -e "${YELLOW}🔍 Key validation points:${NC}"
echo "  1. Memory leak detection with confidence > 90%"
echo "  2. OOM predictions within ±30 seconds accuracy"
echo "  3. Process-to-pod correlation working correctly"
echo "  4. eBPF syscall pattern analysis functioning"
echo "  5. Graceful fallback when eBPF unavailable"
echo ""

echo -e "${BLUE}🚀 Next steps:${NC}"
echo "  • Review test logs for specific eBPF insights"
echo "  • Validate prediction accuracy meets requirements"
echo "  • Test on different workload patterns"
echo "  • Deploy to production clusters for real-world validation"