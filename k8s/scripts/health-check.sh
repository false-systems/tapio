#!/bin/bash
set -euo pipefail

# Tapio Health Check Script
# Comprehensive health monitoring for Tapio eBPF collectors and OTEL integration

# Configuration
NAMESPACE="${NAMESPACE:-tapio-system}"
OUTPUT_FORMAT="${OUTPUT_FORMAT:-table}"
WATCH="${WATCH:-false}"
INTERVAL="${INTERVAL:-30}"
VERBOSE="${VERBOSE:-false}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Status indicators
OK="✅"
WARN="⚠️"
ERROR="❌"
INFO="ℹ️"

# Logging functions
log() {
    echo -e "${BLUE}[INFO]${NC} $(date '+%H:%M:%S') $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $(date '+%H:%M:%S') $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $1"
}

success() {
    echo -e "${GREEN}[OK]${NC} $(date '+%H:%M:%S') $1"
}

# Health check results
declare -A health_status
declare -A health_messages
declare -A health_details

# Initialize health status
init_health() {
    health_status[namespace]="unknown"
    health_status[collectors]="unknown"
    health_status[otel_collector]="unknown"
    health_status[dependencies]="unknown"
    health_status[network]="unknown"
    health_status[rbac]="unknown"
    health_status[monitoring]="unknown"
    health_status[performance]="unknown"
    health_status[ebpf]="unknown"
}

# Check namespace
check_namespace() {
    if kubectl get namespace "$NAMESPACE" &> /dev/null; then
        health_status[namespace]="ok"
        health_messages[namespace]="Namespace exists"
        
        # Check resource quotas
        local quota_info
        quota_info=$(kubectl get resourcequota -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
        health_details[namespace]="Resource quotas: $quota_info"
    else
        health_status[namespace]="error"
        health_messages[namespace]="Namespace not found"
        health_details[namespace]="Run deployment script to create namespace"
    fi
}

# Check collectors
check_collectors() {
    local collector_desired collector_ready collector_available
    
    if kubectl get daemonset tapio-collector -n "$NAMESPACE" &> /dev/null; then
        collector_desired=$(kubectl get daemonset tapio-collector -n "$NAMESPACE" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
        collector_ready=$(kubectl get daemonset tapio-collector -n "$NAMESPACE" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
        collector_available=$(kubectl get daemonset tapio-collector -n "$NAMESPACE" -o jsonpath='{.status.numberAvailable}' 2>/dev/null || echo "0")
        
        if [[ "$collector_ready" == "$collector_desired" ]] && [[ "$collector_available" == "$collector_desired" ]]; then
            health_status[collectors]="ok"
            health_messages[collectors]="All collectors running"
        elif [[ "$collector_ready" -gt 0 ]]; then
            health_status[collectors]="warn"
            health_messages[collectors]="Some collectors not ready"
        else
            health_status[collectors]="error"
            health_messages[collectors]="No collectors ready"
        fi
        
        health_details[collectors]="Ready: $collector_ready/$collector_desired, Available: $collector_available"
        
        # Check for restart loops
        local restart_count
        restart_count=$(kubectl get pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[*].status.containerStatuses[0].restartCount}' 2>/dev/null | awk '{s+=$1} END {print s+0}')
        if [[ "$restart_count" -gt 10 ]]; then
            health_status[collectors]="warn"
            health_messages[collectors]+=" (High restart count: $restart_count)"
        fi
    else
        health_status[collectors]="error"
        health_messages[collectors]="Collector DaemonSet not found"
        health_details[collectors]="Deploy collectors using deployment script"
    fi
}

# Check OTEL collector
check_otel_collector() {
    local otel_desired otel_ready otel_available
    
    if kubectl get deployment tapio-otel-collector -n "$NAMESPACE" &> /dev/null; then
        otel_desired=$(kubectl get deployment tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
        otel_ready=$(kubectl get deployment tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
        otel_available=$(kubectl get deployment tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || echo "0")
        
        if [[ "$otel_ready" == "$otel_desired" ]] && [[ "$otel_available" == "$otel_desired" ]]; then
            health_status[otel_collector]="ok"
            health_messages[otel_collector]="OTEL collector healthy"
        elif [[ "$otel_ready" -gt 0 ]]; then
            health_status[otel_collector]="warn"
            health_messages[otel_collector]="OTEL collector partially ready"
        else
            health_status[otel_collector]="error"
            health_messages[otel_collector]="OTEL collector not ready"
        fi
        
        health_details[otel_collector]="Ready: $otel_ready/$otel_desired, Available: $otel_available"
        
        # Check HPA status
        if kubectl get hpa tapio-otel-collector-hpa -n "$NAMESPACE" &> /dev/null; then
            local hpa_replicas
            hpa_replicas=$(kubectl get hpa tapio-otel-collector-hpa -n "$NAMESPACE" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo "0")
            health_details[otel_collector]+=" HPA: $hpa_replicas replicas"
        fi
    else
        health_status[otel_collector]="error"
        health_messages[otel_collector]="OTEL collector deployment not found"
        health_details[otel_collector]="Deploy OTEL collector using deployment script"
    fi
}

# Check dependencies
check_dependencies() {
    local nats_status="unknown"
    local neo4j_status="unknown"
    
    # Check NATS
    if kubectl get pods -l app=nats -n "$NAMESPACE" &> /dev/null; then
        local nats_ready
        nats_ready=$(kubectl get pods -l app=nats -n "$NAMESPACE" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "False")
        if [[ "$nats_ready" == "True" ]]; then
            nats_status="ok"
        else
            nats_status="error"
        fi
    else
        nats_status="missing"
    fi
    
    # Check Neo4j
    if kubectl get pods -l app=neo4j -n "$NAMESPACE" &> /dev/null; then
        local neo4j_ready
        neo4j_ready=$(kubectl get pods -l app=neo4j -n "$NAMESPACE" -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "False")
        if [[ "$neo4j_ready" == "True" ]]; then
            neo4j_status="ok"
        else
            neo4j_status="error"
        fi
    else
        neo4j_status="missing"
    fi
    
    # Determine overall dependency status
    if [[ "$nats_status" == "ok" ]] && [[ "$neo4j_status" == "ok" ]]; then
        health_status[dependencies]="ok"
        health_messages[dependencies]="All dependencies healthy"
    elif [[ "$nats_status" == "missing" ]] || [[ "$neo4j_status" == "missing" ]]; then
        health_status[dependencies]="warn"
        health_messages[dependencies]="Some dependencies missing"
    else
        health_status[dependencies]="error"
        health_messages[dependencies]="Dependencies not ready"
    fi
    
    health_details[dependencies]="NATS: $nats_status, Neo4j: $neo4j_status"
}

# Check network connectivity
check_network() {
    local network_issues=0
    local network_details=""
    
    # Check services
    local services
    services=$(kubectl get services -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    network_details="Services: $services"
    
    # Check network policies
    local netpols
    netpols=$(kubectl get networkpolicies -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    network_details+=", NetworkPolicies: $netpols"
    
    # Check for service endpoints
    local collector_endpoints otel_endpoints
    collector_endpoints=$(kubectl get endpoints tapio-collector-metrics -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses}' 2>/dev/null | wc -w)
    otel_endpoints=$(kubectl get endpoints tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses}' 2>/dev/null | wc -w)
    
    network_details+=", Endpoints: collector($collector_endpoints), otel($otel_endpoints)"
    
    if [[ "$collector_endpoints" -eq 0 ]] || [[ "$otel_endpoints" -eq 0 ]]; then
        network_issues=$((network_issues + 1))
    fi
    
    # Check DNS resolution (if possible)
    if kubectl get pod -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[0].metadata.name}' &> /dev/null; then
        local pod_name
        pod_name=$(kubectl get pod -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[0].metadata.name}')
        if ! kubectl exec "$pod_name" -n "$NAMESPACE" -- nslookup tapio-otel-collector &> /dev/null; then
            network_issues=$((network_issues + 1))
            network_details+=", DNS: failed"
        else
            network_details+=", DNS: ok"
        fi
    fi
    
    if [[ $network_issues -eq 0 ]]; then
        health_status[network]="ok"
        health_messages[network]="Network connectivity healthy"
    else
        health_status[network]="warn"
        health_messages[network]="Network connectivity issues detected"
    fi
    
    health_details[network]="$network_details"
}

# Check RBAC
check_rbac() {
    local rbac_issues=0
    local rbac_details=""
    
    # Check service accounts
    local sa_count
    sa_count=$(kubectl get serviceaccounts -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    rbac_details="ServiceAccounts: $sa_count"
    
    # Check cluster roles
    local cr_count
    cr_count=$(kubectl get clusterroles -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)
    rbac_details+=", ClusterRoles: $cr_count"
    
    # Check cluster role bindings
    local crb_count
    crb_count=$(kubectl get clusterrolebindings -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)
    rbac_details+=", ClusterRoleBindings: $crb_count"
    
    # Test permissions
    if ! kubectl auth can-i get nodes --as=system:serviceaccount:"$NAMESPACE":tapio-collector &> /dev/null; then
        rbac_issues=$((rbac_issues + 1))
        rbac_details+=", Permissions: failed"
    else
        rbac_details+=", Permissions: ok"
    fi
    
    if [[ $rbac_issues -eq 0 ]]; then
        health_status[rbac]="ok"
        health_messages[rbac]="RBAC configuration healthy"
    else
        health_status[rbac]="error"
        health_messages[rbac]="RBAC configuration issues"
    fi
    
    health_details[rbac]="$rbac_details"
}

# Check monitoring
check_monitoring() {
    local monitoring_issues=0
    local monitoring_details=""
    
    # Check ServiceMonitors
    local sm_count
    sm_count=$(kubectl get servicemonitors -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    monitoring_details="ServiceMonitors: $sm_count"
    
    # Check PrometheusRules
    local pr_count
    pr_count=$(kubectl get prometheusrules -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    monitoring_details+=", PrometheusRules: $pr_count"
    
    # Check if metrics endpoints are accessible
    if kubectl get pod -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[0].metadata.name}' &> /dev/null; then
        local pod_name
        pod_name=$(kubectl get pod -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[0].metadata.name}')
        if kubectl exec "$pod_name" -n "$NAMESPACE" -- curl -s http://localhost:9090/metrics | head -n 1 &> /dev/null; then
            monitoring_details+=", Metrics: accessible"
        else
            monitoring_issues=$((monitoring_issues + 1))
            monitoring_details+=", Metrics: failed"
        fi
    fi
    
    if [[ $monitoring_issues -eq 0 ]]; then
        health_status[monitoring]="ok"
        health_messages[monitoring]="Monitoring healthy"
    else
        health_status[monitoring]="warn"
        health_messages[monitoring]="Monitoring issues detected"
    fi
    
    health_details[monitoring]="$monitoring_details"
}

# Check performance
check_performance() {
    local performance_issues=0
    local performance_details=""
    
    # Check resource usage
    if command -v kubectl &> /dev/null; then
        local cpu_usage memory_usage
        cpu_usage=$(kubectl top pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" --no-headers 2>/dev/null | awk '{sum+=$2} END {print sum+0}')
        memory_usage=$(kubectl top pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" --no-headers 2>/dev/null | awk '{sum+=$3} END {print sum+0}')
        
        performance_details="CPU: ${cpu_usage}m, Memory: ${memory_usage}Mi"
        
        # Check for high resource usage (assuming limits of 500m CPU, 1Gi memory per pod)
        if [[ $cpu_usage -gt 400 ]]; then
            performance_issues=$((performance_issues + 1))
            performance_details+=", High CPU usage"
        fi
        
        if [[ $memory_usage -gt 800 ]]; then
            performance_issues=$((performance_issues + 1))
            performance_details+=", High memory usage"
        fi
    else
        performance_details="Metrics server not available"
    fi
    
    # Check for OOMKilled containers
    local oom_count
    oom_count=$(kubectl get pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[*].status.containerStatuses[?(@.lastState.terminated.reason=="OOMKilled")].name}' 2>/dev/null | wc -w)
    
    if [[ $oom_count -gt 0 ]]; then
        performance_issues=$((performance_issues + 1))
        performance_details+=", OOMKilled: $oom_count"
    fi
    
    if [[ $performance_issues -eq 0 ]]; then
        health_status[performance]="ok"
        health_messages[performance]="Performance healthy"
    else
        health_status[performance]="warn"
        health_messages[performance]="Performance issues detected"
    fi
    
    health_details[performance]="$performance_details"
}

# Check eBPF programs
check_ebpf() {
    local ebpf_issues=0
    local ebpf_details=""
    local total_programs=0
    local loaded_programs=0
    
    # Get collector pods
    local collector_pods
    collector_pods=$(kubectl get pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [[ -n "$collector_pods" ]]; then
        for pod in $collector_pods; do
            # Check eBPF programs in /sys/fs/bpf
            local programs_count
            programs_count=$(kubectl exec "$pod" -n "$NAMESPACE" -- find /sys/fs/bpf -name "*tapio*" -type f 2>/dev/null | wc -l || echo "0")
            total_programs=$((total_programs + 1))
            
            if [[ $programs_count -gt 0 ]]; then
                loaded_programs=$((loaded_programs + 1))
            else
                ebpf_issues=$((ebpf_issues + 1))
            fi
        done
        
        ebpf_details="Programs loaded: $loaded_programs/$total_programs nodes"
        
        # Check kernel version compatibility
        local incompatible_nodes=0
        local nodes
        nodes=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}')
        
        for node in $nodes; do
            local kernel_version
            kernel_version=$(kubectl get node "$node" -o jsonpath='{.status.nodeInfo.kernelVersion}')
            local major minor
            major=$(echo "$kernel_version" | cut -d. -f1)
            minor=$(echo "$kernel_version" | cut -d. -f2)
            
            if [[ $major -lt 4 ]] || ([[ $major -eq 4 ]] && [[ $minor -lt 18 ]]); then
                incompatible_nodes=$((incompatible_nodes + 1))
            fi
        done
        
        if [[ $incompatible_nodes -gt 0 ]]; then
            ebpf_issues=$((ebpf_issues + 1))
            ebpf_details+=", Incompatible nodes: $incompatible_nodes"
        fi
    else
        ebpf_issues=1
        ebpf_details="No collector pods found"
    fi
    
    if [[ $ebpf_issues -eq 0 ]]; then
        health_status[ebpf]="ok"
        health_messages[ebpf]="eBPF programs healthy"
    else
        health_status[ebpf]="error"
        health_messages[ebpf]="eBPF program issues detected"
    fi
    
    health_details[ebpf]="$ebpf_details"
}

# Get status symbol
get_status_symbol() {
    case "$1" in
        "ok") echo "$OK" ;;
        "warn") echo "$WARN" ;;
        "error") echo "$ERROR" ;;
        *) echo "$INFO" ;;
    esac
}

# Display health summary
display_summary() {
    case "$OUTPUT_FORMAT" in
        "json")
            echo "{"
            local first=true
            for component in namespace collectors otel_collector dependencies network rbac monitoring performance ebpf; do
                if [[ "$first" == "true" ]]; then
                    first=false
                else
                    echo ","
                fi
                echo "  \"$component\": {"
                echo "    \"status\": \"${health_status[$component]}\","
                echo "    \"message\": \"${health_messages[$component]}\","
                echo "    \"details\": \"${health_details[$component]:-}\""
                echo -n "  }"
            done
            echo
            echo "}"
            ;;
        "csv")
            echo "Component,Status,Message,Details"
            for component in namespace collectors otel_collector dependencies network rbac monitoring performance ebpf; do
                echo "$component,${health_status[$component]},\"${health_messages[$component]}\",\"${health_details[$component]:-}\""
            done
            ;;
        *)
            echo
            echo "╔══════════════════════════════════════════════════════════════════════════════╗"
            echo "║                           TAPIO HEALTH CHECK REPORT                         ║"
            echo "╠══════════════════════════════════════════════════════════════════════════════╣"
            printf "║ %-20s │ %-6s │ %-45s ║\n" "COMPONENT" "STATUS" "MESSAGE"
            echo "╠══════════════════════════════════════════════════════════════════════════════╣"
            
            for component in namespace collectors otel_collector dependencies network rbac monitoring performance ebpf; do
                local symbol
                symbol=$(get_status_symbol "${health_status[$component]}")
                local display_name
                case "$component" in
                    "otel_collector") display_name="OTEL Collector" ;;
                    "ebpf") display_name="eBPF Programs" ;;
                    *) display_name=$(echo "$component" | tr '[:lower:]' '[:upper:]' | sed 's/_/ /g') ;;
                esac
                
                printf "║ %-20s │ %-4s │ %-45s ║\n" "$display_name" "$symbol" "${health_messages[$component]}"
                
                if [[ -n "${health_details[$component]:-}" ]] && [[ "$VERBOSE" == "true" ]]; then
                    printf "║ %-20s │ %-6s │ %-45s ║\n" "" "" "${health_details[$component]}"
                fi
            done
            
            echo "╚══════════════════════════════════════════════════════════════════════════════╝"
            ;;
    esac
}

# Display detailed information
display_details() {
    if [[ "$VERBOSE" != "true" ]]; then
        return
    fi
    
    echo
    echo "╔══════════════════════════════════════════════════════════════════════════════╗"
    echo "║                           DETAILED INFORMATION                              ║"
    echo "╚══════════════════════════════════════════════════════════════════════════════╝"
    
    for component in namespace collectors otel_collector dependencies network rbac monitoring performance ebpf; do
        if [[ -n "${health_details[$component]:-}" ]]; then
            echo
            echo "$(echo "$component" | tr '[:lower:]' '[:upper:]' | sed 's/_/ /g'):"
            echo "  ${health_details[$component]}"
        fi
    done
}

# Main health check function
main() {
    init_health
    
    if [[ "$OUTPUT_FORMAT" == "table" ]]; then
        log "Starting Tapio health check for namespace: $NAMESPACE"
    fi
    
    # Run all health checks
    check_namespace
    check_collectors
    check_otel_collector
    check_dependencies
    check_network
    check_rbac
    check_monitoring
    check_performance
    check_ebpf
    
    # Display results
    display_summary
    display_details
    
    # Determine overall status
    local overall_status="ok"
    local error_count=0
    local warning_count=0
    
    for component in namespace collectors otel_collector dependencies network rbac monitoring performance ebpf; do
        case "${health_status[$component]}" in
            "error")
                overall_status="error"
                error_count=$((error_count + 1))
                ;;
            "warn")
                if [[ "$overall_status" != "error" ]]; then
                    overall_status="warn"
                fi
                warning_count=$((warning_count + 1))
                ;;
        esac
    done
    
    # Show summary and exit with appropriate code
    if [[ "$OUTPUT_FORMAT" == "table" ]]; then
        echo
        case "$overall_status" in
            "ok")
                success "All components healthy"
                ;;
            "warn")
                warn "$warning_count components have warnings"
                ;;
            "error")
                error "$error_count components have errors, $warning_count have warnings"
                ;;
        esac
    fi
    
    case "$overall_status" in
        "ok") exit 0 ;;
        "warn") exit 1 ;;
        "error") exit 2 ;;
    esac
}

# Watch mode
watch_mode() {
    if [[ "$OUTPUT_FORMAT" != "table" ]]; then
        error "Watch mode only supported with table output format"
        exit 1
    fi
    
    while true; do
        clear
        echo "$(date '+%Y-%m-%d %H:%M:%S') - Tapio Health Check (refreshing every ${INTERVAL}s)"
        echo "Press Ctrl+C to stop"
        main
        sleep "$INTERVAL"
    done
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_FORMAT="$2"
            if [[ "$OUTPUT_FORMAT" != "table" ]] && [[ "$OUTPUT_FORMAT" != "json" ]] && [[ "$OUTPUT_FORMAT" != "csv" ]]; then
                error "Invalid output format. Use: table, json, csv"
                exit 1
            fi
            shift 2
            ;;
        -w|--watch)
            WATCH="true"
            shift
            ;;
        -i|--interval)
            INTERVAL="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE="true"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -n, --namespace NAME    Kubernetes namespace (default: tapio-system)"
            echo "  -o, --output FORMAT     Output format: table, json, csv (default: table)"
            echo "  -w, --watch             Watch mode - continuous monitoring"
            echo "  -i, --interval SECONDS  Watch interval in seconds (default: 30)"
            echo "  -v, --verbose           Show detailed information"
            echo "  -h, --help              Show this help message"
            echo
            echo "Examples:"
            echo "  $0                      # Basic health check"
            echo "  $0 -v                   # Verbose health check"
            echo "  $0 -w -i 10             # Watch mode, 10 second intervals"
            echo "  $0 -o json              # JSON output"
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Run health check
if [[ "$WATCH" == "true" ]]; then
    watch_mode
else
    main
fi