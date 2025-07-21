#!/bin/bash
set -euo pipefail

# Install systemd services for Tapio

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICES=(
    "tapio-collector.service"
    "tapio-intelligence.service"
)

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   log_error "This script must be run as root"
   exit 1
fi

# Create tapio user and group
if ! id -u tapio &>/dev/null; then
    echo "Creating tapio user..."
    useradd -r -s /bin/false -d /var/lib/tapio -m tapio
    log_success "Created tapio user"
fi

# Create directories
echo "Creating directories..."
mkdir -p /opt/tapio/bin
mkdir -p /etc/tapio
mkdir -p /var/lib/tapio
mkdir -p /var/log/tapio
chown -R tapio:tapio /var/lib/tapio /var/log/tapio
chmod 755 /opt/tapio/bin
log_success "Directories created"

# Create default configs
if [ ! -f /etc/tapio/collector.yaml ]; then
    cat > /etc/tapio/collector.yaml << 'EOF'
# Tapio Collector Configuration
collectors:
  ebpf:
    enabled: true
    programs:
      - type: syscall
        enabled: true
      - type: network
        enabled: true
      - type: memory
        enabled: true
  
  kubernetes:
    enabled: true
    kubeconfig: /etc/tapio/kubeconfig
    
  systemd:
    enabled: true
    journal_path: /var/log/journal
    
  cni:
    enabled: true
    socket_path: /var/run/tapio/cni.sock

output:
  type: grpc
  endpoint: localhost:8080
  batch_size: 1000
  flush_interval: 1s

logging:
  level: info
  output: /var/log/tapio/collector.log
EOF
    chown tapio:tapio /etc/tapio/collector.yaml
    chmod 644 /etc/tapio/collector.yaml
    log_success "Created collector config"
fi

if [ ! -f /etc/tapio/intelligence.yaml ]; then
    cat > /etc/tapio/intelligence.yaml << 'EOF'
# Tapio Intelligence Configuration
pipeline:
  mode: high-performance
  max_concurrency: 32
  batch_size: 1000
  buffer_size: 50000

correlation:
  enabled: true
  time_window: 5m
  min_score: 0.7
  patterns:
    - name: cascade_failure
      window: 2m
      min_events: 3
    - name: memory_leak
      window: 10m
      threshold: 0.8

ml:
  enabled: true
  model_path: /var/lib/tapio/models
  update_interval: 1h

storage:
  type: embedded
  path: /var/lib/tapio/data
  retention: 7d

metrics:
  enabled: true
  listen: :9091
  path: /metrics
EOF
    chown tapio:tapio /etc/tapio/intelligence.yaml
    chmod 644 /etc/tapio/intelligence.yaml
    log_success "Created intelligence config"
fi

# Install systemd services
echo "Installing systemd services..."
for service in "${SERVICES[@]}"; do
    cp "$SCRIPT_DIR/$service" /etc/systemd/system/
    chmod 644 "/etc/systemd/system/$service"
    log_success "Installed $service"
done

# Create systemd drop-in directory for overrides
mkdir -p /etc/systemd/system/tapio-collector.service.d
cat > /etc/systemd/system/tapio-collector.service.d/override.conf << 'EOF'
# Override file for local customizations
# Add your overrides here
# Example:
# [Service]
# Environment="TAPIO_DEBUG=true"
EOF

# Reload systemd
systemctl daemon-reload
log_success "Systemd configuration reloaded"

# Create log rotation
cat > /etc/logrotate.d/tapio << 'EOF'
/var/log/tapio/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0644 tapio tapio
    sharedscripts
    postrotate
        systemctl reload tapio-collector tapio-intelligence >/dev/null 2>&1 || true
    endscript
}
EOF
log_success "Log rotation configured"

echo
echo "✅ Tapio systemd services installed!"
echo
echo "To start services:"
echo "  systemctl start tapio-collector"
echo "  systemctl start tapio-intelligence"
echo
echo "To enable at boot:"
echo "  systemctl enable tapio-collector"
echo "  systemctl enable tapio-intelligence"
echo
echo "To check status:"
echo "  systemctl status tapio-collector"
echo "  systemctl status tapio-intelligence"
echo
echo "Logs available at:"
echo "  journalctl -u tapio-collector -f"
echo "  journalctl -u tapio-intelligence -f"