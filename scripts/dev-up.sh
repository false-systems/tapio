#!/bin/bash
set -e

# Tapio Development Environment Startup Script
# Starts all required services for local development

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Detect OS
OS="unknown"
case "$(uname -s)" in
    Darwin) OS="macos" ;;
    Linux) OS="linux" ;;
esac

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Start Docker/Colima
start_container_runtime() {
    log_info "Starting container runtime..."
    
    if [[ "$OS" == "macos" ]] && command_exists colima; then
        if ! colima status >/dev/null 2>&1; then
            log_info "Starting Colima with VM support for eBPF development..."
            colima start --vm-type=qemu --memory=6 --cpu=4 --disk=50 \
                         --mount-type=9p --kubernetes --network-address
            log_success "Colima started with Kubernetes support"
        else
            log_success "Colima already running"
        fi
    elif command_exists docker; then
        if ! docker ps >/dev/null 2>&1; then
            if [[ "$OS" == "linux" ]]; then
                sudo systemctl start docker
                log_success "Docker started"
            else
                log_warning "Please start Docker Desktop manually"
                return 1
            fi
        else
            log_success "Docker already running"
        fi
    else
        log_error "No container runtime found. Run ./scripts/install.sh first"
        return 1
    fi
}

# Start Minikube
start_minikube() {
    if ! command_exists minikube; then
        log_warning "Minikube not found. Run ./scripts/install.sh to install"
        return 1
    fi
    
    log_info "Starting Minikube..."
    
    # Check if already running
    if minikube status | grep -q "Running" 2>/dev/null; then
        log_success "Minikube already running"
        return 0
    fi
    
    # Start minikube with appropriate driver
    local driver="docker"
    if [[ "$OS" == "macos" ]] && command_exists colima; then
        driver="docker"  # Colima provides Docker
    elif [[ "$OS" == "linux" ]]; then
        driver="docker"
    fi
    
    minikube start --driver="$driver" \
                   --memory=4096 \
                   --cpus=2 \
                   --disk-size=20gb \
                   --kubernetes-version=v1.28.0 \
                   --addons=dashboard,metrics-server,ingress
    
    log_success "Minikube started with addons"
    
    # Configure kubectl context
    kubectl config use-context minikube
    log_info "kubectl context set to minikube"
}

# Setup Kubernetes resources
setup_kubernetes() {
    log_info "Setting up Kubernetes resources for Tapio..."
    
    cd "$PROJECT_ROOT"
    
    # Create namespace
    if ! kubectl get namespace tapio >/dev/null 2>&1; then
        kubectl create namespace tapio
        log_success "Created tapio namespace"
    fi
    
    # Apply RBAC if it exists
    if [ -f "deploy/k8s/rbac.yaml" ]; then
        kubectl apply -f deploy/k8s/rbac.yaml -n tapio
        log_success "Applied RBAC configuration"
    fi
    
    # Create configmaps for collectors
    if [ -f "config/collector.yaml" ]; then
        kubectl create configmap tapio-config \
                --from-file=collector.yaml=config/collector.yaml \
                -n tapio --dry-run=client -o yaml | kubectl apply -f -
        log_success "Applied collector configuration"
    fi
    
    log_success "Kubernetes resources configured"
}

# Build and load Docker images
build_images() {
    log_info "Building Docker images for local development..."
    
    cd "$PROJECT_ROOT"
    
    # Build main collector image
    if [ -f "Dockerfile" ]; then
        docker build -t tapio-collector:dev .
        
        # Load into minikube if available
        if command_exists minikube && minikube status >/dev/null 2>&1; then
            minikube image load tapio-collector:dev
            log_success "Loaded tapio-collector:dev into minikube"
        fi
    fi
    
    # Build development image if available
    if [ -f "Dockerfile.dev" ]; then
        docker build -f Dockerfile.dev -t tapio-dev:latest .
        
        if command_exists minikube && minikube status >/dev/null 2>&1; then
            minikube image load tapio-dev:latest
            log_success "Loaded tapio-dev:latest into minikube"
        fi
    fi
}

# Start monitoring stack (optional)
start_monitoring() {
    log_info "Setting up monitoring stack..."
    
    # Add Prometheus Helm repo
    if command_exists helm; then
        helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
        helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
        helm repo update >/dev/null 2>&1
        
        # Install Prometheus if not exists
        if ! helm list -n monitoring | grep -q prometheus 2>/dev/null; then
            kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
            
            helm install prometheus prometheus-community/kube-prometheus-stack \
                --namespace monitoring \
                --set grafana.adminPassword=admin \
                --set alertmanager.enabled=false \
                --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
                --wait --timeout=300s
            
            log_success "Prometheus and Grafana installed"
            log_info "Grafana: kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80"
            log_info "Prometheus: kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090"
        else
            log_success "Monitoring stack already installed"
        fi
    fi
}

# Show connection info
show_connection_info() {
    log_success "🚀 Development environment is ready!"
    echo
    log_info "Services Status:"
    
    # Docker/Colima status
    if docker ps >/dev/null 2>&1; then
        echo -e "  ✅ Container Runtime: ${GREEN}Running${NC}"
    else
        echo -e "  ❌ Container Runtime: ${RED}Not Running${NC}"
    fi
    
    # Minikube status
    if minikube status >/dev/null 2>&1; then
        echo -e "  ✅ Minikube: ${GREEN}Running${NC}"
        echo -e "     Dashboard: ${BLUE}minikube dashboard${NC}"
    else
        echo -e "  ❌ Minikube: ${RED}Not Running${NC}"
    fi
    
    # Kubernetes context
    if kubectl config current-context >/dev/null 2>&1; then
        CONTEXT=$(kubectl config current-context)
        echo -e "  ✅ kubectl context: ${GREEN}$CONTEXT${NC}"
    fi
    
    echo
    log_info "Quick Start Commands:"
    echo -e "  ${BLUE}# Run Tapio collector locally${NC}"
    echo -e "  go run cmd/tapio-collector/main.go"
    echo
    echo -e "  ${BLUE}# Run Tapio server locally${NC}"
    echo -e "  go run cmd/tapio-server/main.go"
    echo
    echo -e "  ${BLUE}# Deploy to Kubernetes${NC}"
    echo -e "  kubectl apply -f deploy/k8s/"
    echo
    echo -e "  ${BLUE}# Use Skaffold for development${NC}"
    echo -e "  skaffold dev"
    echo
    echo -e "  ${BLUE}# Port forward services${NC}"
    echo -e "  kubectl port-forward -n tapio svc/tapio-server 8080:80"
    echo
    
    if command_exists minikube; then
        MINIKUBE_IP=$(minikube ip 2>/dev/null || echo "localhost")
        log_info "Minikube IP: $MINIKUBE_IP"
        
        # Show NodePort services
        local nodeports
        nodeports=$(kubectl get svc -A --no-headers | grep NodePort | awk '{print $2":"$6}' 2>/dev/null || true)
        if [ -n "$nodeports" ]; then
            log_info "NodePort Services:"
            echo "$nodeports" | while read -r svc; do
                echo "  http://$MINIKUBE_IP:$(echo "$svc" | cut -d':' -f2 | cut -d'/' -f1)"
            done
        fi
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up development environment..."
    
    # Stop services in reverse order
    if command_exists minikube; then
        minikube stop
        log_info "Minikube stopped"
    fi
    
    if [[ "$OS" == "macos" ]] && command_exists colima; then
        colima stop
        log_info "Colima stopped"
    elif [[ "$OS" == "linux" ]] && command_exists docker; then
        sudo systemctl stop docker
        log_info "Docker stopped"
    fi
    
    log_success "Development environment stopped"
}

# Print usage
print_usage() {
    cat << EOF
Tapio Development Environment Startup

Usage: $0 [OPTIONS] [COMMAND]

Commands:
    start           Start all development services (default)
    stop            Stop all development services
    restart         Restart all services
    status          Show status of all services
    logs            Show logs from running services

Options:
    -h, --help      Show this help message
    --no-k8s       Don't start Kubernetes services
    --no-monitoring Don't install monitoring stack
    --build        Force rebuild of Docker images

Examples:
    $0                          # Start everything
    $0 --no-monitoring          # Start without monitoring
    $0 stop                     # Stop all services
    $0 status                   # Check status
EOF
}

# Main execution
main() {
    local command="start"
    local start_k8s=true
    local start_monitoring=true
    local force_build=false
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                print_usage
                exit 0
                ;;
            --no-k8s)
                start_k8s=false
                shift
                ;;
            --no-monitoring)
                start_monitoring=false
                shift
                ;;
            --build)
                force_build=true
                shift
                ;;
            start|stop|restart|status|logs)
                command="$1"
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done
    
    case "$command" in
        start)
            log_info "🚀 Starting Tapio development environment..."
            
            start_container_runtime
            
            if [[ "$start_k8s" == "true" ]]; then
                start_minikube
                setup_kubernetes
                
                if [[ "$start_monitoring" == "true" ]]; then
                    start_monitoring
                fi
            fi
            
            if [[ "$force_build" == "true" ]]; then
                build_images
            fi
            
            show_connection_info
            ;;
        
        stop)
            cleanup
            ;;
            
        restart)
            log_info "Restarting development environment..."
            cleanup
            sleep 2
            "$0" start "${@:2}"
            ;;
            
        status)
            show_connection_info
            ;;
            
        logs)
            if command_exists minikube && minikube status >/dev/null 2>&1; then
                log_info "Recent logs from tapio namespace:"
                kubectl logs -n tapio --tail=50 -l app=tapio-collector 2>/dev/null || log_info "No tapio-collector pods found"
                kubectl logs -n tapio --tail=50 -l app=tapio-server 2>/dev/null || log_info "No tapio-server pods found"
            else
                log_warning "Minikube not running. No logs available."
            fi
            ;;
            
        *)
            log_error "Unknown command: $command"
            print_usage
            exit 1
            ;;
    esac
}

# Handle Ctrl+C
trap 'echo; log_info "Interrupted by user"; exit 130' INT

# Run main function with all arguments
main "$@"