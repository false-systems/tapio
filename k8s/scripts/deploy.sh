#!/bin/bash
set -euo pipefail

# Tapio Deployment Script
# This script deploys the Tapio eBPF observability platform with OTEL integration

# Configuration
NAMESPACE="${NAMESPACE:-tapio-system}"
CLUSTER_NAME="${CLUSTER_NAME:-production-cluster}"
ENVIRONMENT="${ENVIRONMENT:-production}"
DRY_RUN="${DRY_RUN:-false}"
VERBOSE="${VERBOSE:-false}"
TIMEOUT="${TIMEOUT:-600}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log() {
    echo -e "${BLUE}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $(date '+%Y-%m-%d %H:%M:%S') $1"
}

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        error "kubectl is required but not installed"
        exit 1
    fi
    
    # Check helm if using helm deployment
    if [[ "${USE_HELM:-false}" == "true" ]]; then
        if ! command -v helm &> /dev/null; then
            error "helm is required but not installed"
            exit 1
        fi
    fi
    
    # Check cluster connectivity
    if ! kubectl cluster-info &> /dev/null; then
        error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check cluster version
    local k8s_version
    k8s_version=$(kubectl version --short --client-only -o json | jq -r '.clientVersion.gitVersion' | sed 's/v//')
    local required_version="1.20.0"
    
    if ! printf '%s\n%s\n' "$required_version" "$k8s_version" | sort -V -C; then
        warn "Kubernetes version $k8s_version may not be supported. Minimum version is $required_version"
    fi
    
    success "Prerequisites check passed"
}

# Check kernel compatibility on nodes
check_kernel_compatibility() {
    log "Checking kernel compatibility across nodes..."
    
    local nodes
    nodes=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}')
    
    for node in $nodes; do
        log "Checking node: $node"
        
        local kernel_version
        kernel_version=$(kubectl get node "$node" -o jsonpath='{.status.nodeInfo.kernelVersion}')
        
        local major minor
        major=$(echo "$kernel_version" | cut -d. -f1)
        minor=$(echo "$kernel_version" | cut -d. -f2)
        
        if [[ $major -lt 4 ]] || ([[ $major -eq 4 ]] && [[ $minor -lt 18 ]]); then
            error "Node $node has kernel version $kernel_version, which is not supported (minimum: 4.18)"
            exit 1
        else
            log "Node $node kernel version $kernel_version is compatible"
        fi
    done
    
    success "Kernel compatibility check passed"
}

# Create namespace
create_namespace() {
    log "Creating namespace $NAMESPACE..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl create namespace "$NAMESPACE" $dry_run_flag --save-config=true || true
    kubectl label namespace "$NAMESPACE" name="$NAMESPACE" --overwrite=true $dry_run_flag || true
    
    success "Namespace $NAMESPACE ready"
}

# Deploy secrets
deploy_secrets() {
    log "Deploying secrets..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    # Check if secrets already exist
    if kubectl get secret tapio-credentials -n "$NAMESPACE" &> /dev/null; then
        warn "Secrets already exist, skipping creation"
        return
    fi
    
    # Apply secrets
    kubectl apply -f ../secrets.yaml -n "$NAMESPACE" $dry_run_flag
    
    success "Secrets deployed"
}

# Deploy RBAC
deploy_rbac() {
    log "Deploying RBAC configurations..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl apply -f ../rbac.yaml $dry_run_flag
    
    success "RBAC configurations deployed"
}

# Deploy ConfigMaps
deploy_configmaps() {
    log "Deploying ConfigMaps..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    # Substitute environment variables in config
    envsubst < ../configmaps.yaml | kubectl apply -f - $dry_run_flag
    
    success "ConfigMaps deployed"
}

# Deploy Network Policies
deploy_network_policies() {
    log "Deploying Network Policies..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl apply -f ../network-policies.yaml $dry_run_flag
    
    success "Network Policies deployed"
}

# Deploy Priority Classes
deploy_priority_classes() {
    log "Deploying Priority Classes..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    cat <<EOF | kubectl apply -f - $dry_run_flag
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: tapio-high-priority
value: 1000000
globalDefault: false
description: "High priority class for Tapio eBPF collectors"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: tapio-normal-priority
value: 100000
globalDefault: false
description: "Normal priority class for Tapio services"
EOF
    
    success "Priority Classes deployed"
}

# Deploy dependencies (NATS, Neo4j)
deploy_dependencies() {
    log "Deploying dependencies..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    # Deploy NATS
    if [[ -f ../nats-fixed.yaml ]]; then
        kubectl apply -f ../nats-fixed.yaml -n "$NAMESPACE" $dry_run_flag
    fi
    
    # Deploy Neo4j
    if [[ -f ../neo4j.yaml ]]; then
        kubectl apply -f ../neo4j.yaml -n "$NAMESPACE" $dry_run_flag
    fi
    
    # Wait for dependencies to be ready
    if [[ "$DRY_RUN" != "true" ]]; then
        log "Waiting for dependencies to be ready..."
        kubectl wait --for=condition=ready pod -l app=nats -n "$NAMESPACE" --timeout=300s || warn "NATS not ready within timeout"
        kubectl wait --for=condition=ready pod -l app=neo4j -n "$NAMESPACE" --timeout=300s || warn "Neo4j not ready within timeout"
    fi
    
    success "Dependencies deployed"
}

# Deploy OTEL Collector
deploy_otel_collector() {
    log "Deploying OTEL Collector..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl apply -f ../otel-collector.yaml $dry_run_flag
    
    if [[ "$DRY_RUN" != "true" ]]; then
        log "Waiting for OTEL Collector to be ready..."
        kubectl wait --for=condition=available deployment/tapio-otel-collector -n "$NAMESPACE" --timeout=300s
    fi
    
    success "OTEL Collector deployed"
}

# Deploy Collectors
deploy_collectors() {
    log "Deploying Tapio Collectors..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl apply -f ../collector-daemonset.yaml $dry_run_flag
    
    if [[ "$DRY_RUN" != "true" ]]; then
        log "Waiting for Collectors to be ready..."
        kubectl rollout status daemonset/tapio-collector -n "$NAMESPACE" --timeout=600s
    fi
    
    success "Collectors deployed"
}

# Deploy monitoring
deploy_monitoring() {
    log "Deploying monitoring configurations..."
    
    local dry_run_flag=""
    if [[ "$DRY_RUN" == "true" ]]; then
        dry_run_flag="--dry-run=client"
    fi
    
    kubectl apply -f ../monitoring.yaml $dry_run_flag
    
    success "Monitoring configurations deployed"
}

# Verify deployment
verify_deployment() {
    log "Verifying deployment..."
    
    # Check namespace
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        error "Namespace $NAMESPACE not found"
        return 1
    fi
    
    # Check collector DaemonSet
    local collector_ready
    collector_ready=$(kubectl get daemonset tapio-collector -n "$NAMESPACE" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
    local collector_desired
    collector_desired=$(kubectl get daemonset tapio-collector -n "$NAMESPACE" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
    
    log "Collector DaemonSet: $collector_ready/$collector_desired ready"
    
    # Check OTEL Collector
    local otel_ready
    otel_ready=$(kubectl get deployment tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    local otel_desired
    otel_desired=$(kubectl get deployment tapio-otel-collector -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
    
    log "OTEL Collector: $otel_ready/$otel_desired ready"
    
    # Check services
    local services
    services=$(kubectl get services -n "$NAMESPACE" --no-headers | wc -l)
    log "Services deployed: $services"
    
    # Check ConfigMaps
    local configmaps
    configmaps=$(kubectl get configmaps -n "$NAMESPACE" --no-headers | wc -l)
    log "ConfigMaps deployed: $configmaps"
    
    # Check secrets
    local secrets
    secrets=$(kubectl get secrets -n "$NAMESPACE" --no-headers | wc -l)
    log "Secrets deployed: $secrets"
    
    success "Deployment verification completed"
}

# Show status
show_status() {
    log "Deployment Status Summary:"
    echo "================================"
    
    echo "Namespace: $NAMESPACE"
    echo "Cluster: $CLUSTER_NAME"
    echo "Environment: $ENVIRONMENT"
    echo
    
    echo "Components:"
    kubectl get all -n "$NAMESPACE" -o wide
    echo
    
    echo "ConfigMaps:"
    kubectl get configmaps -n "$NAMESPACE"
    echo
    
    echo "Secrets:"
    kubectl get secrets -n "$NAMESPACE"
    echo
    
    echo "Network Policies:"
    kubectl get networkpolicies -n "$NAMESPACE"
    echo
    
    if command -v kubectl >/dev/null 2>&1; then
        echo "Top Pods by CPU:"
        kubectl top pods -n "$NAMESPACE" --sort-by=cpu 2>/dev/null || echo "Metrics server not available"
        echo
        
        echo "Top Pods by Memory:"
        kubectl top pods -n "$NAMESPACE" --sort-by=memory 2>/dev/null || echo "Metrics server not available"
    fi
}

# Cleanup function
cleanup() {
    if [[ "${CLEANUP_ON_ERROR:-false}" == "true" ]]; then
        warn "Cleaning up due to error..."
        ./cleanup.sh -n "$NAMESPACE" -f
    fi
}

# Main deployment function
main() {
    log "Starting Tapio deployment..."
    log "Namespace: $NAMESPACE"
    log "Cluster: $CLUSTER_NAME"
    log "Environment: $ENVIRONMENT"
    log "Dry run: $DRY_RUN"
    
    # Set trap for cleanup on error
    trap cleanup ERR
    
    # Export environment variables for envsubst
    export CLUSTER_NAME ENVIRONMENT NAMESPACE
    export NODE_NAME="${NODE_NAME:-}"
    export POD_NAME="${POD_NAME:-}"
    export POD_NAMESPACE="${POD_NAMESPACE:-$NAMESPACE}"
    
    # Run deployment steps
    check_prerequisites
    check_kernel_compatibility
    create_namespace
    deploy_secrets
    deploy_rbac
    deploy_configmaps
    deploy_network_policies
    deploy_priority_classes
    deploy_dependencies
    deploy_otel_collector
    deploy_collectors
    deploy_monitoring
    
    if [[ "$DRY_RUN" != "true" ]]; then
        verify_deployment
        show_status
    fi
    
    success "Tapio deployment completed successfully!"
    
    if [[ "$DRY_RUN" != "true" ]]; then
        echo
        echo "Next steps:"
        echo "1. Access collector metrics: kubectl port-forward -n $NAMESPACE daemonset/tapio-collector 9090:9090"
        echo "2. Access OTEL collector UI: kubectl port-forward -n $NAMESPACE deployment/tapio-otel-collector 55679:55679"
        echo "3. Check collector logs: kubectl logs -f -n $NAMESPACE daemonset/tapio-collector"
        echo "4. Monitor alerts: kubectl get prometheusrules -n $NAMESPACE"
        echo
        echo "For more information, run: kubectl get configmap tapio-production-checklist -n $NAMESPACE -o jsonpath='{.data.checklist\.md}'"
    fi
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -c|--cluster)
            CLUSTER_NAME="$2"
            shift 2
            ;;
        -e|--environment)
            ENVIRONMENT="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN="true"
            shift
            ;;
        -v|--verbose)
            VERBOSE="true"
            set -x
            shift
            ;;
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --cleanup-on-error)
            CLEANUP_ON_ERROR="true"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -n, --namespace NAME    Kubernetes namespace (default: tapio-system)"
            echo "  -c, --cluster NAME      Cluster name (default: production-cluster)"
            echo "  -e, --environment ENV   Environment (default: production)"
            echo "      --dry-run           Perform a dry run"
            echo "  -v, --verbose           Enable verbose output"
            echo "      --timeout SECONDS   Timeout for operations (default: 600)"
            echo "      --cleanup-on-error  Cleanup on deployment error"
            echo "  -h, --help              Show this help message"
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Run main function
main