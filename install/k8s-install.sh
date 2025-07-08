#!/bin/bash
# Tapio Kubernetes installer
# Deploys Tapio as a service in your Kubernetes cluster

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Configuration
NAMESPACE=${TAPIO_NAMESPACE:-tapio-system}
METHOD=${INSTALL_METHOD:-auto}
CHART_VERSION=${CHART_VERSION:-latest}
VALUES_FILE=${VALUES_FILE:-""}

log() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

detect_installation_method() {
    log "Detecting best installation method..."
    
    if [[ "$METHOD" != "auto" ]]; then
        log "Using specified method: $METHOD"
        return
    fi
    
    if command_exists helm; then
        METHOD="helm"
        log "Helm detected - using Helm installation"
    elif command_exists kubectl; then
        METHOD="kubectl"
        log "kubectl detected - using manifest installation"
    else
        error "Neither Helm nor kubectl found"
    fi
}

check_cluster_access() {
    log "Checking cluster access..."
    
    if ! kubectl cluster-info >/dev/null 2>&1; then
        error "Cannot access Kubernetes cluster. Please check your kubeconfig"
    fi
    
    local context=$(kubectl config current-context)
    log "Connected to cluster: $context"
}

check_permissions() {
    log "Checking permissions..."
    
    # Test if we can create namespace
    if ! kubectl auth can-i create namespaces >/dev/null 2>&1; then
        warn "Cannot create namespaces - you may need cluster admin privileges"
    fi
    
    # Test if we can create cluster roles
    if ! kubectl auth can-i create clusterroles >/dev/null 2>&1; then
        warn "Cannot create cluster roles - some features may not work"
    fi
}

install_with_helm() {
    log "Installing Tapio with Helm..."
    
    # Add Tapio Helm repository
    if ! helm repo list | grep -q "tapio"; then
        log "Adding Tapio Helm repository..."
        helm repo add tapio https://charts.tapio.io
        helm repo update
    fi
    
    # Prepare Helm command
    local helm_cmd="helm install tapio tapio/tapio --namespace $NAMESPACE --create-namespace"
    
    if [[ "$CHART_VERSION" != "latest" ]]; then
        helm_cmd="$helm_cmd --version $CHART_VERSION"
    fi
    
    if [[ -n "$VALUES_FILE" ]]; then
        helm_cmd="$helm_cmd --values $VALUES_FILE"
    fi
    
    # Add common values based on detected environment
    local kernel_version=$(uname -r 2>/dev/null || echo "unknown")
    if [[ "$kernel_version" != "unknown" ]]; then
        local major=$(echo "$kernel_version" | cut -d. -f1)
        local minor=$(echo "$kernel_version" | cut -d. -f2)
        
        if (( major < 4 || (major == 4 && minor < 18) )); then
            warn "Kernel $kernel_version may not support eBPF - disabling eBPF features"
            helm_cmd="$helm_cmd --set nodeAgent.enabled=false"
        fi
    fi
    
    # Install
    eval "$helm_cmd"
    
    log "Tapio installed successfully with Helm"
}

install_with_kubectl() {
    log "Installing Tapio with kubectl..."
    
    # Create namespace
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
    
    # Generate and apply manifests
    local temp_dir=$(mktemp -d)
    
    # Download or generate manifests
    if command_exists curl; then
        curl -sSL "https://raw.githubusercontent.com/falseyair/tapio/main/deploy/manifests/install.yaml" > "$temp_dir/tapio.yaml"
    elif command_exists wget; then
        wget -qO "$temp_dir/tapio.yaml" "https://raw.githubusercontent.com/falseyair/tapio/main/deploy/manifests/install.yaml"
    else
        error "Neither curl nor wget found"
    fi
    
    # Apply manifests
    kubectl apply -f "$temp_dir/tapio.yaml" -n "$NAMESPACE"
    
    rm -rf "$temp_dir"
    log "Tapio installed successfully with kubectl"
}

wait_for_deployment() {
    log "Waiting for Tapio to be ready..."
    
    # Wait for server deployment
    if kubectl get deployment tapio-server -n "$NAMESPACE" >/dev/null 2>&1; then
        kubectl wait --for=condition=available deployment/tapio-server -n "$NAMESPACE" --timeout=300s
    fi
    
    # Wait for DaemonSet (if enabled)
    if kubectl get daemonset tapio-node-agent -n "$NAMESPACE" >/dev/null 2>&1; then
        kubectl wait --for=condition=ready pod -l app.kubernetes.io/component=node-agent -n "$NAMESPACE" --timeout=300s
    fi
    
    log "Tapio is ready!"
}

show_status() {
    log "Checking Tapio status..."
    echo
    
    # Show pods
    kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=tapio
    echo
    
    # Show services
    kubectl get services -n "$NAMESPACE" -l app.kubernetes.io/name=tapio
    echo
    
    # Get metrics endpoint
    local metrics_service=$(kubectl get service -n "$NAMESPACE" -l app.kubernetes.io/component=server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [[ -n "$metrics_service" ]]; then
        log "Metrics available at:"
        echo "  kubectl port-forward -n $NAMESPACE service/$metrics_service 8080:8080"
        echo "  Then visit: http://localhost:8080/metrics"
        echo
    fi
}

uninstall() {
    log "Uninstalling Tapio..."
    
    if [[ "$METHOD" == "helm" ]] && command_exists helm; then
        helm uninstall tapio -n "$NAMESPACE"
    else
        kubectl delete namespace "$NAMESPACE"
    fi
    
    log "Tapio uninstalled"
}

show_help() {
    echo "Tapio Kubernetes installer"
    echo
    echo "Usage: $0 [command] [options]"
    echo
    echo "Commands:"
    echo "  install     Install Tapio (default)"
    echo "  uninstall   Remove Tapio from cluster"
    echo "  status      Show Tapio status"
    echo
    echo "Options:"
    echo "  --namespace NAME      Kubernetes namespace (default: tapio-system)"
    echo "  --method METHOD       Installation method: auto, helm, kubectl (default: auto)"
    echo "  --chart-version VER   Helm chart version (default: latest)"
    echo "  --values FILE         Helm values file"
    echo "  --help                Show this help"
    echo
    echo "Environment variables:"
    echo "  TAPIO_NAMESPACE       Kubernetes namespace"
    echo "  INSTALL_METHOD        Installation method"
    echo "  CHART_VERSION         Helm chart version"
    echo "  VALUES_FILE           Helm values file"
    echo
    echo "Examples:"
    echo "  $0 install                                    # Auto-detect and install"
    echo "  $0 install --method helm --namespace tapio    # Install with Helm"
    echo "  $0 status                                     # Check status"
    echo "  $0 uninstall                                  # Remove Tapio"
}

main() {
    local command="install"
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            install|uninstall|status)
                command="$1"
                shift
                ;;
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --method)
                METHOD="$2"
                shift 2
                ;;
            --chart-version)
                CHART_VERSION="$2"
                shift 2
                ;;
            --values)
                VALUES_FILE="$2"
                shift 2
                ;;
            --help)
                show_help
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                ;;
        esac
    done
    
    case $command in
        install)
            detect_installation_method
            check_cluster_access
            check_permissions
            
            if [[ "$METHOD" == "helm" ]]; then
                install_with_helm
            else
                install_with_kubectl
            fi
            
            wait_for_deployment
            show_status
            ;;
        uninstall)
            uninstall
            ;;
        status)
            show_status
            ;;
    esac
}

main "$@"