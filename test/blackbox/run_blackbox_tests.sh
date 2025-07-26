#!/bin/bash

# Tapio Black Box Test Runner
# This script runs all black box tests against a running Tapio instance

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
TAPIO_API_URL=${TAPIO_API_URL:-"http://localhost:8080"}
TAPIO_COLLECTOR_URL=${TAPIO_COLLECTOR_URL:-"http://localhost:9090"}
TEST_NAMESPACE=${TEST_NAMESPACE:-"tapio-blackbox-test"}
VERBOSE=${VERBOSE:-false}

echo "=== Tapio Black Box Test Suite ==="
echo "API URL: $TAPIO_API_URL"
echo "Collector URL: $TAPIO_COLLECTOR_URL"
echo ""

# Check if Tapio is running
check_tapio_health() {
    echo -n "Checking Tapio health..."
    if curl -s "$TAPIO_API_URL/health" > /dev/null; then
        echo -e " ${GREEN}OK${NC}"
        return 0
    else
        echo -e " ${RED}FAILED${NC}"
        echo "Tapio is not running at $TAPIO_API_URL"
        exit 1
    fi
}

# Check Kubernetes connectivity
check_k8s() {
    echo -n "Checking Kubernetes connectivity..."
    if kubectl cluster-info > /dev/null 2>&1; then
        echo -e " ${GREEN}OK${NC}"
        return 0
    else
        echo -e " ${YELLOW}WARNING${NC}"
        echo "Kubernetes not available - skipping K8s tests"
        return 1
    fi
}

# Run collector black box tests
run_collector_tests() {
    echo ""
    echo "=== Running Collector Black Box Tests ==="
    
    if check_k8s; then
        echo "Running K8s collector tests..."
        go test -v ./collectors/k8s_blackbox_test.go \
            -tags=blackbox \
            -timeout=10m \
            ${VERBOSE:+-v}
    fi
    
    echo "Running Systemd collector tests..."
    go test -v ./collectors/systemd_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
        
    echo "Running eBPF collector tests..."
    if [[ "$EUID" -eq 0 ]]; then
        go test -v ./collectors/ebpf_blackbox_test.go \
            -tags=blackbox \
            -timeout=10m \
            ${VERBOSE:+-v}
    else
        echo -e "${YELLOW}Skipping eBPF tests (requires root)${NC}"
    fi
}

# Run API black box tests
run_api_tests() {
    echo ""
    echo "=== Running API Black Box Tests ==="
    
    echo "Running correlation API tests..."
    go test -v ./api/correlation_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
        
    echo "Running event ingestion API tests..."
    go test -v ./api/ingestion_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
        
    echo "Running query API tests..."
    go test -v ./api/query_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
}

# Run integration black box tests
run_integration_tests() {
    echo ""
    echo "=== Running Integration Black Box Tests ==="
    
    echo "Running Prometheus integration tests..."
    go test -v ./integrations/prometheus_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
        
    echo "Running SIEM integration tests..."
    go test -v ./integrations/siem_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
}

# Run performance black box tests
run_performance_tests() {
    echo ""
    echo "=== Running Performance Black Box Tests ==="
    
    echo "Running load tests..."
    go test -v ./performance/load_blackbox_test.go \
        -tags=blackbox \
        -timeout=30m \
        -bench=. \
        ${VERBOSE:+-v}
        
    echo "Running stress tests..."
    go test -v ./performance/stress_blackbox_test.go \
        -tags=blackbox \
        -timeout=30m \
        ${VERBOSE:+-v}
}

# Run security black box tests
run_security_tests() {
    echo ""
    echo "=== Running Security Black Box Tests ==="
    
    echo "Running authentication tests..."
    go test -v ./security/auth_blackbox_test.go \
        -tags=blackbox \
        -timeout=10m \
        ${VERBOSE:+-v}
        
    echo "Running penetration tests..."
    if command -v zap-cli &> /dev/null; then
        ./security/run_penetration_tests.sh
    else
        echo -e "${YELLOW}Skipping penetration tests (ZAP not installed)${NC}"
    fi
}

# Run chaos engineering tests
run_chaos_tests() {
    echo ""
    echo "=== Running Chaos Engineering Tests ==="
    
    if [[ "$RUN_CHAOS_TESTS" == "true" ]]; then
        echo "Running failure injection tests..."
        go test -v ./chaos/failure_injection_test.go \
            -tags=blackbox,chaos \
            -timeout=30m \
            ${VERBOSE:+-v}
    else
        echo -e "${YELLOW}Skipping chaos tests (set RUN_CHAOS_TESTS=true to enable)${NC}"
    fi
}

# Generate test report
generate_report() {
    echo ""
    echo "=== Generating Test Report ==="
    
    REPORT_FILE="blackbox_test_report_$(date +%Y%m%d_%H%M%S).html"
    
    # Use go test with JSON output and convert to HTML
    go test -json ./... -tags=blackbox 2>/dev/null | \
        go-test-report -o "$REPORT_FILE" || true
    
    if [[ -f "$REPORT_FILE" ]]; then
        echo "Test report generated: $REPORT_FILE"
    fi
}

# Main execution
main() {
    START_TIME=$(date +%s)
    
    # Pre-flight checks
    check_tapio_health
    
    # Run test suites based on arguments
    if [[ $# -eq 0 ]]; then
        # Run all tests
        run_collector_tests
        run_api_tests
        run_integration_tests
        run_performance_tests
        run_security_tests
        run_chaos_tests
    else
        # Run specific test suites
        for suite in "$@"; do
            case $suite in
                collectors)
                    run_collector_tests
                    ;;
                api)
                    run_api_tests
                    ;;
                integrations)
                    run_integration_tests
                    ;;
                performance)
                    run_performance_tests
                    ;;
                security)
                    run_security_tests
                    ;;
                chaos)
                    run_chaos_tests
                    ;;
                *)
                    echo -e "${RED}Unknown test suite: $suite${NC}"
                    echo "Available suites: collectors, api, integrations, performance, security, chaos"
                    exit 1
                    ;;
            esac
        done
    fi
    
    # Generate report if all tests completed
    generate_report
    
    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))
    
    echo ""
    echo "=== Test Execution Complete ==="
    echo "Duration: ${DURATION}s"
}

# Run main function with all arguments
main "$@"