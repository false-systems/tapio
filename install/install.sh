#!/bin/bash
# Tapio installer script - Kubernetes intelligence made simple
# Usage: curl -sSL https://tapio.run | sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
TAPIO_VERSION=${TAPIO_VERSION:-latest}
INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}
CONFIG_DIR=${CONFIG_DIR:-$HOME/.tapio}
REPO_URL="https://github.com/falseyair/tapio"
BINARY_NAME="tapio"

# Platform detection
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l) ARCH="arm" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case $OS in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Helper functions
log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

debug() {
    if [[ "${DEBUG:-}" == "1" ]]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if we're running in CI
is_ci() {
    [[ "${CI:-}" == "true" ]] || [[ -n "${GITHUB_ACTIONS:-}" ]] || [[ -n "${GITLAB_CI:-}" ]]
}

# Detect environment
detect_environment() {
    log "Detecting environment..."
    
    # Check for Kubernetes
    if command_exists kubectl; then
        if kubectl cluster-info >/dev/null 2>&1; then
            log "kubectl found and cluster accessible"
            K8S_AVAILABLE=true
        else
            warn "kubectl found but no cluster accessible"
            K8S_AVAILABLE=false
        fi
    else
        warn "kubectl not found"
        K8S_AVAILABLE=false
    fi
    
    # Check for Helm
    if command_exists helm; then
        log "Helm found"
        HELM_AVAILABLE=true
    else
        debug "Helm not found"
        HELM_AVAILABLE=false
    fi
    
    # Check for container runtime
    if command_exists docker; then
        log "Docker found"
        DOCKER_AVAILABLE=true
    elif command_exists podman; then
        log "Podman found"
        CONTAINER_RUNTIME="podman"
    else
        debug "No container runtime found"
        DOCKER_AVAILABLE=false
    fi
    
    # Check kernel version for eBPF
    if [[ "$OS" == "linux" ]]; then
        KERNEL_VERSION=$(uname -r)
        KERNEL_MAJOR=$(echo "$KERNEL_VERSION" | cut -d. -f1)
        KERNEL_MINOR=$(echo "$KERNEL_VERSION" | cut -d. -f2)
        
        if (( KERNEL_MAJOR > 4 || (KERNEL_MAJOR == 4 && KERNEL_MINOR >= 18) )); then
            log "Kernel $KERNEL_VERSION supports eBPF"
            EBPF_SUPPORTED=true
        else
            warn "Kernel $KERNEL_VERSION may not fully support eBPF (4.18+ recommended)"
            EBPF_SUPPORTED=false
        fi
    else
        debug "eBPF not available on $OS"
        EBPF_SUPPORTED=false
    fi
}

# Get latest version from GitHub
get_latest_version() {
    if [[ "$TAPIO_VERSION" == "latest" ]]; then
        debug "Fetching latest version from GitHub..."
        if command_exists curl; then
            TAPIO_VERSION=$(curl -sSL "https://api.github.com/repos/falseyair/tapio/releases/latest" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
        elif command_exists wget; then
            TAPIO_VERSION=$(wget -qO- "https://api.github.com/repos/falseyair/tapio/releases/latest" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
        else
            warn "Neither curl nor wget found, using development version"
            TAPIO_VERSION="main"
        fi
    fi
    
    debug "Using version: $TAPIO_VERSION"
}

# Download and install binary
install_binary() {
    log "Installing Tapio binary..."
    
    if [[ "$TAPIO_VERSION" == "main" ]]; then
        # Development installation - build from source
        install_from_source
    else
        # Release installation
        install_from_release
    fi
}

install_from_release() {
    local download_url="https://github.com/falseyair/tapio/releases/download/${TAPIO_VERSION}/tapio_${TAPIO_VERSION#v}_${OS}_${ARCH}.tar.gz"
    local temp_dir=$(mktemp -d)
    
    debug "Download URL: $download_url"
    debug "Temp directory: $temp_dir"
    
    if command_exists curl; then
        curl -sSL "$download_url" | tar -xz -C "$temp_dir"
    elif command_exists wget; then
        wget -qO- "$download_url" | tar -xz -C "$temp_dir"
    else
        error "Neither curl nor wget found"
    fi
    
    # Install binary
    if [[ -w "$INSTALL_DIR" ]]; then
        mv "$temp_dir/$BINARY_NAME" "$INSTALL_DIR/"
    else
        log "Installing to $INSTALL_DIR (requires sudo)"
        sudo mv "$temp_dir/$BINARY_NAME" "$INSTALL_DIR/"
    fi
    
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    rm -rf "$temp_dir"
    
    log "Tapio installed to $INSTALL_DIR/$BINARY_NAME"
}

install_from_source() {
    if ! command_exists go; then
        error "Go not found. Please install Go 1.21+ or use a release version"
    fi
    
    local temp_dir=$(mktemp -d)
    cd "$temp_dir"
    
    log "Cloning repository..."
    git clone "$REPO_URL" .
    
    log "Building from source..."
    go build -ldflags="-s -w" -o "$BINARY_NAME" ./cmd/tapio
    
    # Install binary
    if [[ -w "$INSTALL_DIR" ]]; then
        mv "$BINARY_NAME" "$INSTALL_DIR/"
    else
        log "Installing to $INSTALL_DIR (requires sudo)"
        sudo mv "$BINARY_NAME" "$INSTALL_DIR/"
    fi
    
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    cd - >/dev/null
    rm -rf "$temp_dir"
    
    log "Tapio built and installed to $INSTALL_DIR/$BINARY_NAME"
}

# Setup configuration
setup_config() {
    log "Setting up configuration..."
    
    mkdir -p "$CONFIG_DIR"
    
    # Create basic config if it doesn't exist
    local config_file="$CONFIG_DIR/config.yaml"
    if [[ ! -f "$config_file" ]]; then
        cat > "$config_file" << EOF
# Tapio configuration
update_interval: 30s
memory_thresholds:
  warning: 80
  critical: 90
prediction:
  enabled: true
  min_confidence: 0.8
logging:
  level: info
  format: text
EOF
        log "Created config at $config_file"
    fi
}

# Verify installation
verify_installation() {
    log "Verifying installation..."
    
    if ! command_exists "$BINARY_NAME"; then
        error "Installation failed: $BINARY_NAME not found in PATH"
    fi
    
    # Test basic functionality
    if "$BINARY_NAME" --version >/dev/null 2>&1; then
        local version=$("$BINARY_NAME" --version 2>/dev/null | head -1)
        log "Installation successful: $version"
    else
        error "Installation failed: $BINARY_NAME not working properly"
    fi
}

# Show next steps
show_next_steps() {
    echo
    log "Tapio is ready! Here's what you can do:"
    echo
    
    if [[ "$K8S_AVAILABLE" == "true" ]]; then
        echo "  Try Tapio with your cluster:"
        echo "    tapio check                    # Check current namespace"
        echo "    tapio check --all              # Check all namespaces"
        echo "    tapio why <pod-name>           # Understand a specific pod"
        echo
        
        if [[ "$HELM_AVAILABLE" == "true" ]]; then
            echo "  Install Tapio in-cluster with Helm:"
            echo "    helm install tapio oci://ghcr.io/falseyair/tapio/chart"
            echo
        fi
        
        if [[ "$EBPF_SUPPORTED" == "true" ]]; then
            echo "  Enable advanced monitoring:"
            echo "    tapio install --ebpf           # Deploy with eBPF monitoring"
            echo
        fi
    else
        echo "  Set up Kubernetes access:"
        echo "    1. Install kubectl: https://kubernetes.io/docs/tasks/tools/"
        echo "    2. Configure cluster access"
        echo "    3. Run: tapio check"
        echo
    fi
    
    echo "  Get help:"
    echo "    tapio help                     # Show all commands"
    echo "    tapio docs                     # Open documentation"
    echo
    echo "  Questions? Visit: https://github.com/falseyair/tapio"
}

# Main installation flow
main() {
    echo "Welcome to Tapio - Kubernetes intelligence for humans"
    echo
    
    # Environment detection
    detect_environment
    
    # Get version
    get_latest_version
    
    # Install
    install_binary
    
    # Setup
    setup_config
    
    # Verify
    verify_installation
    
    # Next steps
    show_next_steps
}

# Handle script arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            TAPIO_VERSION="$2"
            shift 2
            ;;
        --install-dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --debug)
            DEBUG=1
            shift
            ;;
        --help)
            echo "Tapio installer"
            echo
            echo "Usage: $0 [options]"
            echo
            echo "Options:"
            echo "  --version VERSION     Install specific version (default: latest)"
            echo "  --install-dir DIR     Install directory (default: /usr/local/bin)"
            echo "  --debug               Enable debug output"
            echo "  --help                Show this help"
            echo
            echo "Environment variables:"
            echo "  TAPIO_VERSION         Version to install"
            echo "  INSTALL_DIR           Installation directory"
            echo "  CONFIG_DIR            Configuration directory"
            echo
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

# Run main installation
main