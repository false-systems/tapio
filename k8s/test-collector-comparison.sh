#!/bin/bash
set -e

# Tapio Collector Comparison Test Suite
# Compares Python simulator vs Go binary performance and functionality

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

NAMESPACE="tapio-system"
SIMULATOR_LABEL="app.kubernetes.io/component=collector"
GO_LABEL="app.kubernetes.io/component=collector-go"

echo -e "${GREEN}🚀 Tapio Collector Comparison Test Suite${NC}"
echo "======================================================"

# Function to check pod status
check_pod_status() {
    local label=$1
    local name=$2
    
    echo -e "${YELLOW}Checking $name pod status...${NC}"
    kubectl get pods -n $NAMESPACE -l "$label" --no-headers
    
    # Check if pods are running
    local running_pods=$(kubectl get pods -n $NAMESPACE -l "$label" --no-headers | awk '{print $3}' | grep -c "Running" || echo 0)
    echo "Running pods: $running_pods"
    
    if [ "$running_pods" -eq 0 ]; then
        echo -e "${RED}❌ No running pods found for $name${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✓ $name pods are running${NC}"
    return 0
}

# Function to check pod health endpoints
check_health_endpoints() {
    local label=$1
    local name=$2
    local pod_name=$(kubectl get pods -n $NAMESPACE -l "$label" --no-headers | head -1 | awk '{print $1}')
    
    if [ -z "$pod_name" ]; then
        echo -e "${RED}❌ No pod found for $name${NC}"
        return 1
    fi
    
    echo -e "${YELLOW}Testing health endpoints for $name ($pod_name)...${NC}"
    
    # Test health endpoints
    kubectl exec -n $NAMESPACE $pod_name -c tapio-collector -- wget -q -O- http://localhost:8080/healthz 2>/dev/null | grep -q "OK" || {
        echo -e "${RED}❌ Health check failed for $name${NC}"
        return 1
    }
    
    kubectl exec -n $NAMESPACE $pod_name -c tapio-collector -- wget -q -O- http://localhost:8080/readyz 2>/dev/null | grep -q "Ready\|OK" || {
        echo -e "${RED}❌ Ready check failed for $name${NC}"
        return 1
    }
    
    echo -e "${GREEN}✓ Health endpoints working for $name${NC}"
    return 0
}

# Function to check metrics endpoint
check_metrics() {
    local label=$1
    local name=$2
    local pod_name=$(kubectl get pods -n $NAMESPACE -l "$label" --no-headers | head -1 | awk '{print $1}')
    
    if [ -z "$pod_name" ]; then
        echo -e "${RED}❌ No pod found for $name${NC}"
        return 1
    fi
    
    echo -e "${YELLOW}Testing metrics for $name ($pod_name)...${NC}"
    
    # Get metrics
    local metrics_output=$(kubectl exec -n $NAMESPACE $pod_name -c tapio-collector -- wget -q -O- http://localhost:9090/metrics 2>/dev/null || echo "")
    
    if [ -z "$metrics_output" ]; then
        echo -e "${RED}❌ No metrics output from $name${NC}"
        return 1
    fi
    
    local metric_count=$(echo "$metrics_output" | grep -c "^[a-zA-Z]" || echo 0)
    echo "Metrics count: $metric_count"
    
    echo -e "${GREEN}✓ Metrics endpoint working for $name${NC}"
    return 0
}

# Function to check OTLP trace generation
check_otlp_traces() {
    local name=$1
    
    echo -e "${YELLOW}Checking OTLP traces in Jaeger for $name...${NC}"
    
    # Port forward to Jaeger (run in background)
    kubectl port-forward -n monitoring svc/jaeger-ui 16690:16686 > /dev/null 2>&1 &
    local pf_pid=$!
    sleep 3
    
    # Check for traces
    local service_name="tapio-collector"
    if [ "$name" = "Go Binary" ]; then
        service_name="tapio-collector-go"
    fi
    
    local traces=$(curl -s "http://localhost:16690/api/traces?service=$service_name&limit=5" 2>/dev/null | jq -r '.data | length' 2>/dev/null || echo 0)
    
    # Clean up port forward
    kill $pf_pid 2>/dev/null || true
    
    echo "Traces found: $traces"
    
    if [ "$traces" -gt 0 ]; then
        echo -e "${GREEN}✓ OTLP traces found in Jaeger for $name${NC}"
        return 0
    else
        echo -e "${RED}❌ No traces found in Jaeger for $name${NC}"
        return 1
    fi
}

# Function to get resource usage
get_resource_usage() {
    local label=$1
    local name=$2
    local pod_name=$(kubectl get pods -n $NAMESPACE -l "$label" --no-headers | head -1 | awk '{print $1}')
    
    if [ -z "$pod_name" ]; then
        echo -e "${RED}❌ No pod found for $name${NC}"
        return 1
    fi
    
    echo -e "${YELLOW}Resource usage for $name ($pod_name):${NC}"
    
    # Get resource metrics
    kubectl top pod -n $NAMESPACE $pod_name --containers 2>/dev/null || {
        echo "Resource metrics not available (requires metrics-server)"
        return 0
    }
}

# Function to compare performance
compare_performance() {
    echo -e "${GREEN}📊 Performance Comparison${NC}"
    echo "======================================================"
    
    echo -e "${YELLOW}Python Simulator:${NC}"
    get_resource_usage "$SIMULATOR_LABEL" "Python Simulator"
    
    echo ""
    echo -e "${YELLOW}Go Binary:${NC}"
    get_resource_usage "$GO_LABEL" "Go Binary"
}

# Function to show logs
show_logs() {
    local label=$1
    local name=$2
    local pod_name=$(kubectl get pods -n $NAMESPACE -l "$label" --no-headers | head -1 | awk '{print $1}')
    
    if [ -z "$pod_name" ]; then
        echo -e "${RED}❌ No pod found for $name${NC}"
        return 1
    fi
    
    echo -e "${YELLOW}Recent logs for $name ($pod_name):${NC}"
    kubectl logs -n $NAMESPACE $pod_name -c tapio-collector --tail=5 | head -10
    echo ""
}

# Main test execution
main() {
    echo -e "${GREEN}1. Checking Pod Status${NC}"
    echo "======================================================"
    
    # Check Python simulator
    if check_pod_status "$SIMULATOR_LABEL" "Python Simulator"; then
        SIMULATOR_RUNNING=true
    else
        SIMULATOR_RUNNING=false
    fi
    
    # Check Go binary
    if check_pod_status "$GO_LABEL" "Go Binary"; then
        GO_RUNNING=true
    else
        GO_RUNNING=false
    fi
    
    echo ""
    echo -e "${GREEN}2. Testing Health Endpoints${NC}"
    echo "======================================================"
    
    if $SIMULATOR_RUNNING; then
        check_health_endpoints "$SIMULATOR_LABEL" "Python Simulator" || true
    fi
    
    if $GO_RUNNING; then
        check_health_endpoints "$GO_LABEL" "Go Binary" || true
    fi
    
    echo ""
    echo -e "${GREEN}3. Testing Metrics Endpoints${NC}"
    echo "======================================================"
    
    if $SIMULATOR_RUNNING; then
        check_metrics "$SIMULATOR_LABEL" "Python Simulator" || true
    fi
    
    if $GO_RUNNING; then
        check_metrics "$GO_LABEL" "Go Binary" || true
    fi
    
    echo ""
    echo -e "${GREEN}4. Checking OTLP Traces${NC}"
    echo "======================================================"
    
    if $SIMULATOR_RUNNING; then
        check_otlp_traces "Python Simulator" || true
    fi
    
    if $GO_RUNNING; then
        check_otlp_traces "Go Binary" || true
    fi
    
    echo ""
    echo -e "${GREEN}5. Resource Usage Comparison${NC}"
    echo "======================================================"
    compare_performance
    
    echo ""
    echo -e "${GREEN}6. Recent Logs${NC}"
    echo "======================================================"
    
    if $SIMULATOR_RUNNING; then
        show_logs "$SIMULATOR_LABEL" "Python Simulator"
    fi
    
    if $GO_RUNNING; then
        show_logs "$GO_LABEL" "Go Binary"
    fi
    
    echo -e "${GREEN}🏁 Test Suite Complete${NC}"
    echo "======================================================"
    
    # Summary
    echo -e "${YELLOW}Summary:${NC}"
    if $SIMULATOR_RUNNING; then
        echo "✓ Python Simulator: Running"
    else
        echo "✗ Python Simulator: Not running"
    fi
    
    if $GO_RUNNING; then
        echo "✓ Go Binary: Running"
    else
        echo "✗ Go Binary: Not running"
    fi
    
    echo ""
    echo "Use the following commands for detailed monitoring:"
    echo "  kubectl logs -n $NAMESPACE -l $SIMULATOR_LABEL -c tapio-collector -f"
    echo "  kubectl logs -n $NAMESPACE -l $GO_LABEL -c tapio-collector -f"
    echo "  kubectl port-forward -n monitoring svc/jaeger-ui 16686:16686"
}

# Command line options
case "${1:-}" in
    "deploy-go")
        echo -e "${GREEN}Deploying Go Binary Collector...${NC}"
        kubectl apply -f collector-daemonset-go.yaml
        echo "Waiting for deployment..."
        kubectl rollout status daemonset/tapio-collector-go -n $NAMESPACE --timeout=120s
        ;;
    "compare")
        main
        ;;
    "clean")
        echo -e "${YELLOW}Cleaning up Go Binary deployment...${NC}"
        kubectl delete -f collector-daemonset-go.yaml --ignore-not-found=true
        ;;
    *)
        echo "Usage: $0 {deploy-go|compare|clean}"
        echo ""
        echo "Commands:"
        echo "  deploy-go  Deploy the Go binary collector alongside simulator"
        echo "  compare    Run comparison tests between simulator and Go binary"
        echo "  clean      Remove Go binary deployment"
        ;;
esac