#!/bin/bash
set -euo pipefail

# Tapio Cleanup Script
# This script safely removes the Tapio eBPF observability platform

# Configuration
NAMESPACE="${NAMESPACE:-tapio-system}"
FORCE="${FORCE:-false}"
PRESERVE_DATA="${PRESERVE_DATA:-false}"
VERBOSE="${VERBOSE:-false}"
TIMEOUT="${TIMEOUT:-300}"

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

# Check if namespace exists
check_namespace() {
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        warn "Namespace $NAMESPACE does not exist"
        return 1
    fi
    return 0
}

# Confirm cleanup
confirm_cleanup() {
    if [[ "$FORCE" != "true" ]]; then
        echo -e "${YELLOW}WARNING: This will remove all Tapio components from namespace '$NAMESPACE'${NC}"
        echo -e "${YELLOW}This action cannot be undone!${NC}"
        echo
        echo "Components that will be removed:"
        echo "- DaemonSet: tapio-collector"
        echo "- Deployment: tapio-otel-collector"
        echo "- Services and ConfigMaps"
        echo "- Network Policies"
        echo "- RBAC resources"
        if [[ "$PRESERVE_DATA" != "true" ]]; then
            echo "- Persistent data (NATS, Neo4j)"
        fi
        echo
        
        read -p "Are you sure you want to continue? [y/N]: " -r
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log "Cleanup cancelled"
            exit 0
        fi
    fi
}

# Graceful shutdown of collectors
graceful_shutdown() {
    log "Initiating graceful shutdown of collectors..."
    
    # Scale down OTEL collector first to stop receiving data
    if kubectl get deployment tapio-otel-collector -n "$NAMESPACE" &> /dev/null; then
        log "Scaling down OTEL collector..."
        kubectl scale deployment tapio-otel-collector --replicas=0 -n "$NAMESPACE" || warn "Failed to scale down OTEL collector"
        
        # Wait for pods to terminate
        kubectl wait --for=delete pod -l app.kubernetes.io/name=tapio-otel-collector -n "$NAMESPACE" --timeout=60s || warn "OTEL collector pods did not terminate within timeout"
    fi
    
    # Send SIGTERM to collector pods for graceful shutdown
    if kubectl get daemonset tapio-collector -n "$NAMESPACE" &> /dev/null; then
        log "Sending graceful shutdown signal to collectors..."
        local collector_pods
        collector_pods=$(kubectl get pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
        
        for pod in $collector_pods; do
            if [[ -n "$pod" ]]; then
                log "Gracefully shutting down pod: $pod"
                kubectl exec "$pod" -n "$NAMESPACE" -- pkill -TERM tapio-collector || warn "Failed to send SIGTERM to $pod"
            fi
        done
        
        # Wait a bit for graceful shutdown
        sleep 10
    fi
    
    success "Graceful shutdown completed"
}

# Cleanup eBPF resources
cleanup_ebpf() {
    log "Cleaning up eBPF resources..."
    
    # Get collector pods
    local collector_pods
    collector_pods=$(kubectl get pods -l app.kubernetes.io/name=tapio-collector -n "$NAMESPACE" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    for pod in $collector_pods; do
        if [[ -n "$pod" ]]; then
            log "Cleaning eBPF programs from pod: $pod"
            
            # List and remove eBPF programs
            kubectl exec "$pod" -n "$NAMESPACE" -- bash -c '
                if [[ -d /sys/fs/bpf ]]; then
                    find /sys/fs/bpf -name "*tapio*" -type f -delete 2>/dev/null || true
                    find /sys/fs/bpf -name "*tapio*" -type d -delete 2>/dev/null || true
                fi
            ' 2>/dev/null || warn "Failed to clean eBPF resources from $pod"
        fi
    done
    
    success "eBPF resources cleaned up"
}

# Remove collectors
remove_collectors() {
    log "Removing Tapio collectors..."
    
    # Remove DaemonSet
    if kubectl get daemonset tapio-collector -n "$NAMESPACE" &> /dev/null; then
        kubectl delete daemonset tapio-collector -n "$NAMESPACE" --timeout="${TIMEOUT}s" || warn "Failed to delete collector DaemonSet"
    fi
    
    # Remove collector service
    if kubectl get service tapio-collector-metrics -n "$NAMESPACE" &> /dev/null; then
        kubectl delete service tapio-collector-metrics -n "$NAMESPACE" || warn "Failed to delete collector service"
    fi
    
    success "Collectors removed"
}

# Remove OTEL collector
remove_otel_collector() {
    log "Removing OTEL collector..."
    
    # Remove HPA first
    if kubectl get hpa tapio-otel-collector-hpa -n "$NAMESPACE" &> /dev/null; then
        kubectl delete hpa tapio-otel-collector-hpa -n "$NAMESPACE" || warn "Failed to delete HPA"
    fi
    
    # Remove PDB
    if kubectl get pdb tapio-otel-collector-pdb -n "$NAMESPACE" &> /dev/null; then
        kubectl delete pdb tapio-otel-collector-pdb -n "$NAMESPACE" || warn "Failed to delete PDB"
    fi
    
    # Remove deployment
    if kubectl get deployment tapio-otel-collector -n "$NAMESPACE" &> /dev/null; then
        kubectl delete deployment tapio-otel-collector -n "$NAMESPACE" --timeout="${TIMEOUT}s" || warn "Failed to delete OTEL collector deployment"
    fi
    
    # Remove services
    if kubectl get service tapio-otel-collector -n "$NAMESPACE" &> /dev/null; then
        kubectl delete service tapio-otel-collector -n "$NAMESPACE" || warn "Failed to delete OTEL collector service"
    fi
    
    if kubectl get service tapio-otel-collector-headless -n "$NAMESPACE" &> /dev/null; then
        kubectl delete service tapio-otel-collector-headless -n "$NAMESPACE" || warn "Failed to delete OTEL collector headless service"
    fi
    
    success "OTEL collector removed"
}

# Remove monitoring
remove_monitoring() {
    log "Removing monitoring resources..."
    
    # Remove ServiceMonitors
    kubectl delete servicemonitors -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete ServiceMonitors"
    
    # Remove PodMonitors
    kubectl delete podmonitors -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete PodMonitors"
    
    # Remove PrometheusRules
    kubectl delete prometheusrules -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete PrometheusRules"
    
    success "Monitoring resources removed"
}

# Remove network policies
remove_network_policies() {
    log "Removing network policies..."
    
    kubectl delete networkpolicies -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete NetworkPolicies"
    
    success "Network policies removed"
}

# Remove ConfigMaps and Secrets
remove_configs() {
    log "Removing ConfigMaps and Secrets..."
    
    # Remove ConfigMaps
    kubectl delete configmaps -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete ConfigMaps"
    
    # Remove Secrets (with confirmation)
    if [[ "$PRESERVE_DATA" != "true" ]]; then
        kubectl delete secrets -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete Secrets"
    else
        warn "Preserving secrets (PRESERVE_DATA=true)"
    fi
    
    success "ConfigMaps and Secrets removed"
}

# Remove dependencies
remove_dependencies() {
    log "Removing dependencies..."
    
    if [[ "$PRESERVE_DATA" != "true" ]]; then
        # Remove NATS
        if kubectl get statefulset nats -n "$NAMESPACE" &> /dev/null; then
            log "Removing NATS..."
            kubectl delete statefulset nats -n "$NAMESPACE" --timeout="${TIMEOUT}s" || warn "Failed to delete NATS StatefulSet"
            kubectl delete service nats -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete NATS service"
            kubectl delete pvc -l app=nats -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete NATS PVCs"
        fi
        
        # Remove Neo4j
        if kubectl get statefulset neo4j -n "$NAMESPACE" &> /dev/null; then
            log "Removing Neo4j..."
            kubectl delete statefulset neo4j -n "$NAMESPACE" --timeout="${TIMEOUT}s" || warn "Failed to delete Neo4j StatefulSet"
            kubectl delete service neo4j -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete Neo4j service"
            kubectl delete pvc -l app=neo4j -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete Neo4j PVCs"
        fi
    else
        warn "Preserving dependencies data (PRESERVE_DATA=true)"
    fi
    
    success "Dependencies removed"
}

# Remove RBAC
remove_rbac() {
    log "Removing RBAC resources..."
    
    # Remove ClusterRoleBindings
    kubectl delete clusterrolebindings -l app.kubernetes.io/part-of=tapio 2>/dev/null || warn "Failed to delete ClusterRoleBindings"
    
    # Remove ClusterRoles
    kubectl delete clusterroles -l app.kubernetes.io/part-of=tapio 2>/dev/null || warn "Failed to delete ClusterRoles"
    
    # Remove ServiceAccounts
    kubectl delete serviceaccounts -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete ServiceAccounts"
    
    success "RBAC resources removed"
}

# Remove Priority Classes
remove_priority_classes() {
    log "Removing Priority Classes..."
    
    kubectl delete priorityclasses tapio-high-priority 2>/dev/null || warn "Failed to delete tapio-high-priority"
    kubectl delete priorityclasses tapio-normal-priority 2>/dev/null || warn "Failed to delete tapio-normal-priority"
    
    success "Priority Classes removed"
}

# Final cleanup
final_cleanup() {
    log "Performing final cleanup..."
    
    # Remove any remaining resources with tapio label
    kubectl delete all -l app.kubernetes.io/part-of=tapio -n "$NAMESPACE" 2>/dev/null || warn "Failed to delete remaining labeled resources"
    
    # Check for any remaining pods
    local remaining_pods
    remaining_pods=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    
    if [[ $remaining_pods -gt 0 ]]; then
        warn "$remaining_pods pods still running in namespace $NAMESPACE"
        kubectl get pods -n "$NAMESPACE"
    fi
    
    success "Final cleanup completed"
}

# Remove namespace (if empty and not preserving data)
remove_namespace() {
    if [[ "$PRESERVE_DATA" != "true" ]]; then
        log "Checking if namespace can be removed..."
        
        local resource_count
        resource_count=$(kubectl get all -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
        
        if [[ $resource_count -eq 0 ]]; then
            log "Removing empty namespace $NAMESPACE..."
            kubectl delete namespace "$NAMESPACE" --timeout="${TIMEOUT}s" || warn "Failed to delete namespace"
        else
            warn "Namespace $NAMESPACE is not empty, preserving it"
            log "Remaining resources:"
            kubectl get all -n "$NAMESPACE" 2>/dev/null || true
        fi
    else
        warn "Preserving namespace (PRESERVE_DATA=true)"
    fi
}

# Verify cleanup
verify_cleanup() {
    log "Verifying cleanup..."
    
    local remaining_resources=0
    
    # Check for DaemonSet
    if kubectl get daemonset tapio-collector -n "$NAMESPACE" &> /dev/null; then
        warn "DaemonSet tapio-collector still exists"
        remaining_resources=$((remaining_resources + 1))
    fi
    
    # Check for Deployment
    if kubectl get deployment tapio-otel-collector -n "$NAMESPACE" &> /dev/null; then
        warn "Deployment tapio-otel-collector still exists"
        remaining_resources=$((remaining_resources + 1))
    fi
    
    # Check for ClusterRoles
    local cluster_roles
    cluster_roles=$(kubectl get clusterroles -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)
    if [[ $cluster_roles -gt 0 ]]; then
        warn "$cluster_roles ClusterRoles still exist"
        remaining_resources=$((remaining_resources + cluster_roles))
    fi
    
    # Check for ClusterRoleBindings
    local cluster_role_bindings
    cluster_role_bindings=$(kubectl get clusterrolebindings -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)
    if [[ $cluster_role_bindings -gt 0 ]]; then
        warn "$cluster_role_bindings ClusterRoleBindings still exist"
        remaining_resources=$((remaining_resources + cluster_role_bindings))
    fi
    
    if [[ $remaining_resources -eq 0 ]]; then
        success "Cleanup verification passed - no remaining Tapio resources found"
    else
        warn "Cleanup verification found $remaining_resources remaining resources"
        return 1
    fi
}

# Show cleanup status
show_status() {
    log "Cleanup Status Summary:"
    echo "================================"
    
    echo "Namespace: $NAMESPACE"
    echo "Force: $FORCE"
    echo "Preserve Data: $PRESERVE_DATA"
    echo
    
    if kubectl get namespace "$NAMESPACE" &> /dev/null; then
        echo "Namespace still exists:"
        kubectl get all -n "$NAMESPACE" 2>/dev/null || echo "No resources in namespace"
    else
        echo "Namespace removed"
    fi
    
    echo
    echo "Cluster-wide resources:"
    echo "ClusterRoles: $(kubectl get clusterroles -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)"
    echo "ClusterRoleBindings: $(kubectl get clusterrolebindings -l app.kubernetes.io/part-of=tapio --no-headers 2>/dev/null | wc -l)"
    echo "PriorityClasses: $(kubectl get priorityclasses tapio-high-priority,tapio-normal-priority --no-headers 2>/dev/null | wc -l)"
}

# Main cleanup function
main() {
    log "Starting Tapio cleanup..."
    log "Namespace: $NAMESPACE"
    log "Force: $FORCE"
    log "Preserve Data: $PRESERVE_DATA"
    
    # Check if namespace exists
    if ! check_namespace; then
        log "Nothing to cleanup"
        exit 0
    fi
    
    # Confirm cleanup
    confirm_cleanup
    
    # Run cleanup steps
    graceful_shutdown
    cleanup_ebpf
    remove_collectors
    remove_otel_collector
    remove_monitoring
    remove_network_policies
    remove_configs
    remove_dependencies
    remove_rbac
    remove_priority_classes
    final_cleanup
    remove_namespace
    
    # Verify cleanup
    if verify_cleanup; then
        success "Tapio cleanup completed successfully!"
    else
        warn "Cleanup completed with warnings - some resources may still exist"
    fi
    
    show_status
    
    echo
    echo "Cleanup summary:"
    echo "- All Tapio collectors removed"
    echo "- OTEL collector removed"
    echo "- Monitoring resources removed"
    echo "- Network policies removed"
    echo "- RBAC resources removed"
    if [[ "$PRESERVE_DATA" != "true" ]]; then
        echo "- Persistent data removed"
        echo "- Namespace removed (if empty)"
    else
        echo "- Persistent data preserved"
        echo "- Namespace preserved"
    fi
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -f|--force)
            FORCE="true"
            shift
            ;;
        --preserve-data)
            PRESERVE_DATA="true"
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
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -n, --namespace NAME  Kubernetes namespace (default: tapio-system)"
            echo "  -f, --force           Force cleanup without confirmation"
            echo "      --preserve-data   Preserve persistent data (NATS, Neo4j, secrets)"
            echo "  -v, --verbose         Enable verbose output"
            echo "      --timeout SECONDS Timeout for operations (default: 300)"
            echo "  -h, --help            Show this help message"
            echo
            echo "Examples:"
            echo "  $0                    # Interactive cleanup"
            echo "  $0 -f                 # Force cleanup without confirmation"
            echo "  $0 --preserve-data    # Cleanup but preserve data"
            echo "  $0 -n my-namespace    # Cleanup specific namespace"
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