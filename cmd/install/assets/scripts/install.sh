#!/bin/bash
# Tapio installation script

set -euo pipefail

# Configuration
INSTALL_DIR="${INSTALL_DIR:-/opt/tapio}"
CONFIG_DIR="${CONFIG_DIR:-/etc/tapio}"
DATA_DIR="${DATA_DIR:-/var/lib/tapio}"
BINARY_NAME="tapio"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi
}

# Create directories
create_directories() {
    log_info "Creating directories..."
    mkdir -p "$INSTALL_DIR/bin"
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR"
    
    # Set permissions
    chmod 755 "$INSTALL_DIR"
    chmod 755 "$CONFIG_DIR"
    chmod 755 "$DATA_DIR"
}

# Install binary
install_binary() {
    local src_binary="$1"
    local dst_binary="$INSTALL_DIR/bin/$BINARY_NAME"
    
    log_info "Installing binary..."
    cp "$src_binary" "$dst_binary"
    chmod 755 "$dst_binary"
    
    # Create symlink in PATH
    if [[ -d /usr/local/bin ]]; then
        ln -sf "$dst_binary" "/usr/local/bin/$BINARY_NAME"
    fi
}

# Create default configuration
create_default_config() {
    local config_file="$CONFIG_DIR/tapio.yaml"
    
    if [[ ! -f "$config_file" ]]; then
        log_info "Creating default configuration..."
        cat > "$config_file" <<EOF
# Tapio Configuration
version: 1

server:
  address: "0.0.0.0:8080"
  tls:
    enabled: false

collection:
  interval: 60s
  buffer_size: 1000

storage:
  type: "local"
  path: "$DATA_DIR"

logging:
  level: "info"
  format: "json"
EOF
        chmod 644 "$config_file"
    else
        log_warn "Configuration file already exists, skipping..."
    fi
}

# Main installation
main() {
    log_info "Starting Tapio installation..."
    
    check_root
    create_directories
    
    # Binary path should be passed as argument
    if [[ $# -eq 0 ]]; then
        log_error "Binary path not provided"
        exit 1
    fi
    
    install_binary "$1"
    create_default_config
    
    log_info "Installation completed successfully!"
    log_info "Binary installed to: $INSTALL_DIR/bin/$BINARY_NAME"
    log_info "Configuration at: $CONFIG_DIR/tapio.yaml"
    log_info "Data directory: $DATA_DIR"
}

main "$@"