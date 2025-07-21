#!/bin/bash
set -e

# Tapio Development Environment Installer
# Supports macOS and Linux with intelligent tool detection and installation

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Minimum requirements
MIN_MEMORY_GB=8
MIN_DISK_GB=20

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Detect OS and architecture
detect_os() {
    case "$(uname -s)" in
        Darwin)
            OS="macos"
            ARCH=$(uname -m)
            ;;
        Linux)
            OS="linux"
            ARCH=$(uname -m)
            ;;
        *)
            log_error "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac
    
    log_info "Detected OS: $OS ($ARCH)"
}

# Pre-flight checks
preflight_check() {
    log_info "Running pre-flight checks..."
    
    # Check available memory
    if [[ "$OS" == "macos" ]]; then
        TOTAL_MEM_GB=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
    else
        TOTAL_MEM_GB=$(( $(grep MemTotal /proc/meminfo | awk '{print $2}') / 1024 / 1024 ))
    fi
    
    if [ "$TOTAL_MEM_GB" -lt "$MIN_MEMORY_GB" ]; then
        log_warning "System has ${TOTAL_MEM_GB}GB RAM, recommended minimum is ${MIN_MEMORY_GB}GB"
        log_warning "You may experience performance issues with all services running"
    else
        log_success "Memory check passed: ${TOTAL_MEM_GB}GB available"
    fi
    
    # Check disk space
    if [[ "$OS" == "macos" ]]; then
        # macOS: df shows 512-byte blocks, convert to GB
        AVAILABLE_DISK_GB=$(( $(df . | awk 'NR==2 {print $4}') * 512 / 1024 / 1024 / 1024 ))
    else
        # Linux: use -BG flag
        AVAILABLE_DISK_GB=$(df -BG . | awk 'NR==2 {print $4}' | sed 's/G//')
    fi
    
    if [ "$AVAILABLE_DISK_GB" -lt "$MIN_DISK_GB" ]; then
        log_warning "Only ${AVAILABLE_DISK_GB}GB disk space available, recommended minimum is ${MIN_DISK_GB}GB"
    else
        log_success "Disk space check passed: ${AVAILABLE_DISK_GB}GB available"
    fi
    
    # Check for conflicting services
    if lsof -i :8080 >/dev/null 2>&1; then
        log_warning "Port 8080 is already in use. This may conflict with Tapio server."
    fi
    
    if lsof -i :9090 >/dev/null 2>&1; then
        log_warning "Port 9090 is already in use. This may conflict with Prometheus."
    fi
    
    # Check if running in container
    if [ -f /.dockerenv ]; then
        log_error "This script should not be run inside a container"
        exit 1
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Install package manager (if needed)
install_package_manager() {
    if [[ "$OS" == "macos" ]]; then
        if ! command_exists brew; then
            log_info "Installing Homebrew..."
            /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
        else
            log_success "Homebrew already installed"
        fi
    elif [[ "$OS" == "linux" ]]; then
        # Detect Linux distribution
        if command_exists apt-get; then
            PACKAGE_MANAGER="apt"
            log_info "Using apt package manager"
        elif command_exists yum; then
            PACKAGE_MANAGER="yum"
            log_info "Using yum package manager"
        elif command_exists pacman; then
            PACKAGE_MANAGER="pacman"
            log_info "Using pacman package manager"
        else
            log_error "No supported package manager found (apt, yum, pacman)"
            exit 1
        fi
    fi
}

# Install Go
install_go() {
    if command_exists go; then
        GO_VERSION=$(go version | grep -o 'go[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1)
        log_success "Go already installed: $GO_VERSION"
        return
    fi

    log_info "Installing Go..."
    if [[ "$OS" == "macos" ]]; then
        brew install go
    elif [[ "$OS" == "linux" ]]; then
        # Install latest Go from official source
        GO_VERSION="1.21.5"
        wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
        sudo rm -rf /usr/local/go
        sudo tar -C /usr/local -xzf /tmp/go.tar.gz
        rm /tmp/go.tar.gz
        
        # Add to PATH if not already there
        if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
            echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
            echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc
        fi
        
        export PATH=$PATH:/usr/local/go/bin
    fi
    
    log_success "Go installed successfully"
}

# Install Docker
install_docker() {
    if command_exists docker; then
        log_success "Docker already installed"
        return
    fi

    log_info "Installing Docker..."
    if [[ "$OS" == "macos" ]]; then
        brew install docker
        log_warning "On macOS, you'll need Docker Desktop or Colima to run containers"
    elif [[ "$OS" == "linux" ]]; then
        if [[ "$PACKAGE_MANAGER" == "apt" ]]; then
            sudo apt-get update
            sudo apt-get install -y apt-transport-https ca-certificates curl gnupg lsb-release
            curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
            echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
            sudo apt-get update
            sudo apt-get install -y docker-ce docker-ce-cli containerd.io
        elif [[ "$PACKAGE_MANAGER" == "yum" ]]; then
            sudo yum install -y yum-utils
            sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
            sudo yum install -y docker-ce docker-ce-cli containerd.io
        fi
        
        # Add user to docker group
        sudo usermod -aG docker $USER
        sudo systemctl enable docker
        sudo systemctl start docker
        
        log_warning "Please log out and log back in for Docker group permissions to take effect"
    fi
    
    log_success "Docker installed successfully"
}

# Install Colima (for eBPF and VM support)
install_colima() {
    if command_exists colima; then
        log_success "Colima already installed"
        return
    fi

    if [[ "$OS" != "macos" ]]; then
        log_info "Colima is primarily for macOS. Using Docker directly on Linux."
        return
    fi

    log_info "Installing Colima (Docker runtime for macOS with VM support)..."
    if ! brew install colima; then
        log_error "Failed to install Colima"
        log_info "Try: brew update && brew install colima"
        return 1
    fi
    
    # Install QEMU for VM support (needed for eBPF)
    log_info "Installing QEMU for VM support..."
    if ! brew install qemu; then
        log_warning "QEMU installation failed. eBPF collector may not work properly."
        log_info "Try: brew install qemu"
    fi
    
    log_success "Colima installed successfully"
    log_info "Note: Colima requires macOS 11+ and Apple Silicon or Intel with virtualization"
}

# Install kubectl
install_kubectl() {
    if command_exists kubectl; then
        log_success "kubectl already installed"
        return
    fi

    log_info "Installing kubectl..."
    if [[ "$OS" == "macos" ]]; then
        brew install kubectl
    elif [[ "$OS" == "linux" ]]; then
        curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
        chmod +x kubectl
        sudo mv kubectl /usr/local/bin/
    fi
    
    log_success "kubectl installed successfully"
}

# Install Minikube
install_minikube() {
    if command_exists minikube; then
        log_success "Minikube already installed"
        return
    fi

    log_info "Installing Minikube..."
    if [[ "$OS" == "macos" ]]; then
        brew install minikube
    elif [[ "$OS" == "linux" ]]; then
        curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
        chmod +x minikube-linux-amd64
        sudo mv minikube-linux-amd64 /usr/local/bin/minikube
    fi
    
    log_success "Minikube installed successfully"
}

# Install Skaffold
install_skaffold() {
    if command_exists skaffold; then
        log_success "Skaffold already installed"
        return
    fi

    log_info "Installing Skaffold..."
    if [[ "$OS" == "macos" ]]; then
        brew install skaffold
    elif [[ "$OS" == "linux" ]]; then
        curl -Lo skaffold https://storage.googleapis.com/skaffold/releases/latest/skaffold-linux-amd64
        chmod +x skaffold
        sudo mv skaffold /usr/local/bin
    fi
    
    log_success "Skaffold installed successfully"
}

# Install Helm
install_helm() {
    if command_exists helm; then
        log_success "Helm already installed"
        return
    fi

    log_info "Installing Helm..."
    if [[ "$OS" == "macos" ]]; then
        brew install helm
    elif [[ "$OS" == "linux" ]]; then
        curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
    fi
    
    log_success "Helm installed successfully"
}

# Install additional development tools
install_dev_tools() {
    log_info "Installing additional development tools..."
    
    # Install Make (if not present)
    if ! command_exists make; then
        if [[ "$OS" == "macos" ]]; then
            xcode-select --install 2>/dev/null || true
        elif [[ "$OS" == "linux" ]]; then
            if [[ "$PACKAGE_MANAGER" == "apt" ]]; then
                sudo apt-get install -y build-essential
            elif [[ "$PACKAGE_MANAGER" == "yum" ]]; then
                sudo yum groupinstall -y "Development Tools"
            fi
        fi
        log_success "Make installed"
    else
        log_success "Make already installed"
    fi

    # Install jq (for JSON processing)
    if ! command_exists jq; then
        if [[ "$OS" == "macos" ]]; then
            brew install jq
        elif [[ "$OS" == "linux" ]]; then
            if [[ "$PACKAGE_MANAGER" == "apt" ]]; then
                sudo apt-get install -y jq
            elif [[ "$PACKAGE_MANAGER" == "yum" ]]; then
                sudo yum install -y jq
            fi
        fi
        log_success "jq installed"
    else
        log_success "jq already installed"
    fi

    # Install curl (usually present, but ensure it's there)
    if ! command_exists curl; then
        if [[ "$OS" == "macos" ]]; then
            brew install curl
        elif [[ "$OS" == "linux" ]]; then
            if [[ "$PACKAGE_MANAGER" == "apt" ]]; then
                sudo apt-get install -y curl
            elif [[ "$PACKAGE_MANAGER" == "yum" ]]; then
                sudo yum install -y curl
            fi
        fi
        log_success "curl installed"
    else
        log_success "curl already installed"
    fi
}

# Setup development environment
setup_dev_environment() {
    log_info "Setting up Tapio development environment..."
    
    cd "$PROJECT_ROOT"
    
    # Download Go dependencies
    log_info "Downloading Go dependencies..."
    go mod download
    
    # Build protocol buffers
    if [ -d "proto" ]; then
        log_info "Setting up protocol buffer generation..."
        if ! command_exists buf; then
            if [[ "$OS" == "macos" ]]; then
                brew install bufbuild/buf/buf
            elif [[ "$OS" == "linux" ]]; then
                curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-Linux-x86_64" -o buf
                chmod +x buf
                sudo mv buf /usr/local/bin/
            fi
        fi
        
        # Generate proto files
        buf generate
        log_success "Protocol buffers generated"
    fi
    
    # Format code
    log_info "Formatting code..."
    if command_exists make; then
        make fmt || gofmt -w .
    else
        gofmt -w .
    fi
    
    # Build project
    log_info "Building project..."
    go build ./... || log_warning "Build failed - this might be expected if some collectors require specific setup"
    
    log_success "Development environment setup complete"
}

# Start services (optional)
start_services() {
    log_info "Would you like to start the development environment? (y/N)"
    read -r response
    
    if [[ "$response" =~ ^[Yy]$ ]]; then
        log_info "Starting development services..."
        
        # Start Colima (macOS) or Docker (Linux)
        if [[ "$OS" == "macos" ]] && command_exists colima; then
            if ! colima status >/dev/null 2>&1; then
                log_info "Starting Colima with VM support for eBPF..."
                colima start --vm-type=qemu --memory=4 --cpu=2
            fi
        elif [[ "$OS" == "linux" ]]; then
            sudo systemctl start docker
        fi
        
        # Start Minikube
        if command_exists minikube; then
            if minikube status | grep -q "Stopped" 2>/dev/null; then
                log_info "Starting Minikube..."
                if [[ "$OS" == "macos" ]]; then
                    minikube start --driver=docker
                else
                    minikube start
                fi
            fi
        fi
        
        log_success "Services started. Use 'docker ps' and 'minikube status' to verify"
    fi
}

# Verification
verify_installation() {
    log_info "Verifying installation..."
    
    local errors=0
    
    # Check required tools
    tools=(
        "go:Go language"
        "docker:Docker"
        "kubectl:Kubernetes CLI"
        "minikube:Minikube"
        "skaffold:Skaffold"
        "helm:Helm"
        "make:Make"
        "jq:jq JSON processor"
    )
    
    for tool_info in "${tools[@]}"; do
        IFS=':' read -r tool desc <<< "$tool_info"
        if command_exists "$tool"; then
            log_success "$desc ✓"
        else
            log_error "$desc ✗"
            ((errors++))
        fi
    done
    
    # Check OS-specific tools
    if [[ "$OS" == "macos" ]]; then
        if command_exists colima; then
            log_success "Colima ✓"
        else
            log_error "Colima ✗"
            ((errors++))
        fi
    fi
    
    # Check Docker/Colima status
    if docker ps >/dev/null 2>&1; then
        log_success "Docker daemon running ✓"
    else
        log_warning "Docker daemon not running (start with: docker/colima start)"
    fi
    
    # Check Minikube status
    if minikube status >/dev/null 2>&1; then
        log_success "Minikube running ✓"
    else
        log_warning "Minikube not running (start with: minikube start)"
    fi
    
    if [ $errors -eq 0 ]; then
        log_success "All tools installed successfully! 🚀"
        log_info "Next steps:"
        log_info "  1. cd $(basename "$PROJECT_ROOT")"
        log_info "  2. Start services: ./scripts/dev-up.sh"
        log_info "  3. Run collectors: go run cmd/tapio-collector/main.go"
        log_info "  4. Run server: go run cmd/tapio-server/main.go"
    else
        log_error "$errors tools failed to install"
        return 1
    fi
}

# Print usage
print_usage() {
    cat << EOF
Tapio Development Environment Installer

Usage: $0 [OPTIONS]

Options:
    -h, --help          Show this help message
    -q, --quick         Skip interactive prompts
    --no-services       Don't start services automatically
    --verify-only       Only verify existing installation

This script installs all required tools for Tapio development:
- Go (latest)
- Docker
- Colima (macOS) - for eBPF collector VM support
- kubectl, Minikube, Skaffold, Helm - for Kubernetes development
- Development tools (make, jq, curl)

Supported platforms: macOS, Linux (Ubuntu, CentOS, Arch)
EOF
}

# Main execution
main() {
    local quick_mode=false
    local start_services_mode=true
    local verify_only=false
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                print_usage
                exit 0
                ;;
            -q|--quick)
                quick_mode=true
                shift
                ;;
            --no-services)
                start_services_mode=false
                shift
                ;;
            --verify-only)
                verify_only=true
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done
    
    log_info "🚀 Tapio Development Environment Installer"
    log_info "========================================="
    
    detect_os
    preflight_check
    
    if [[ "$verify_only" == "true" ]]; then
        verify_installation
        exit $?
    fi
    
    # Install everything
    install_package_manager
    install_go
    install_docker
    install_colima
    install_kubectl
    install_minikube
    install_skaffold
    install_helm
    install_dev_tools
    setup_dev_environment
    
    if [[ "$start_services_mode" == "true" && "$quick_mode" == "false" ]]; then
        start_services
    fi
    
    verify_installation
    
    log_success "🎉 Tapio development environment is ready!"
}

# Run main function with all arguments
main "$@"