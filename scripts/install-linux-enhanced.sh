#!/bin/bash
set -euo pipefail

# Enhanced Linux installation script with performance and eBPF tools
# This adds Linux-specific optimizations beyond the basic install.sh

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() { echo -e "${BLUE}ℹ ${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warning() { echo -e "${YELLOW}⚠${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

# Check if running on Linux
if [[ "$(uname -s)" != "Linux" ]]; then
    log_error "This script is for Linux systems only!"
    exit 1
fi

# Check if running as root when needed
check_sudo() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script requires sudo privileges"
        exit 1
    fi
}

# Detect Linux distribution
detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        DISTRO=$ID
        VERSION=$VERSION_ID
    elif [ -f /etc/debian_version ]; then
        DISTRO="debian"
        VERSION=$(cat /etc/debian_version)
    elif [ -f /etc/redhat-release ]; then
        DISTRO="rhel"
        VERSION=$(rpm -q --queryformat '%{VERSION}' centos-release)
    else
        DISTRO="unknown"
        VERSION="unknown"
    fi
    
    log_info "Detected: $DISTRO $VERSION"
}

# Install kernel headers for eBPF
install_kernel_headers() {
    log_info "Installing kernel headers for eBPF development..."
    
    case "$DISTRO" in
        ubuntu|debian)
            apt-get update
            apt-get install -y linux-headers-$(uname -r) build-essential
            ;;
        fedora|rhel|centos)
            dnf install -y kernel-devel-$(uname -r) kernel-headers-$(uname -r)
            dnf group install -y "Development Tools"
            ;;
        arch|manjaro)
            pacman -S --noconfirm linux-headers base-devel
            ;;
        *)
            log_warning "Unknown distro, skipping kernel headers"
            ;;
    esac
}

# Install BPF/BCC tools
install_bpf_tools() {
    log_info "Installing BPF/BCC tools..."
    
    case "$DISTRO" in
        ubuntu|debian)
            apt-get install -y bpfcc-tools linux-tools-common linux-tools-generic \
                               linux-tools-$(uname -r) libbpf-dev
            ;;
        fedora)
            dnf install -y bcc-tools kernel-tools libbpf-devel
            ;;
        arch|manjaro)
            pacman -S --noconfirm bcc bcc-tools libbpf
            ;;
        *)
            log_warning "BPF tools installation not automated for $DISTRO"
            ;;
    esac
    
    # Create symlinks for BCC tools
    if [ -d /usr/share/bcc/tools ]; then
        log_info "Creating BCC tool symlinks..."
        for tool in /usr/share/bcc/tools/*; do
            if [ -f "$tool" ] && [ ! -L "/usr/local/bin/$(basename $tool)" ]; then
                ln -s "$tool" "/usr/local/bin/$(basename $tool)" 2>/dev/null || true
            fi
        done
    fi
}

# Install performance analysis tools
install_perf_tools() {
    log_info "Installing performance analysis tools..."
    
    case "$DISTRO" in
        ubuntu|debian)
            apt-get install -y linux-tools-$(uname -r) perf-tools-unstable \
                               sysstat htop iotop iftop nethogs \
                               trace-cmd kernelshark stress-ng
            ;;
        fedora|rhel|centos)
            dnf install -y perf sysstat htop iotop iftop nethogs \
                          trace-cmd kernelshark stress-ng
            ;;
        arch|manjaro)
            pacman -S --noconfirm perf sysstat htop iotop iftop nethogs \
                                  trace-cmd stress
            ;;
    esac
}

# Install container tools
install_container_tools() {
    log_info "Installing advanced container tools..."
    
    case "$DISTRO" in
        ubuntu|debian)
            # Podman for rootless containers
            apt-get install -y podman buildah skopeo
            
            # Container debugging tools
            apt-get install -y dive docker-slim
            ;;
        fedora|rhel|centos)
            dnf install -y podman buildah skopeo
            ;;
        arch|manjaro)
            pacman -S --noconfirm podman buildah skopeo
            ;;
    esac
    
    # Install crictl for CRI debugging
    CRICTL_VERSION="v1.28.0"
    curl -L "https://github.com/kubernetes-sigs/cri-tools/releases/download/${CRICTL_VERSION}/crictl-${CRICTL_VERSION}-linux-amd64.tar.gz" | \
        tar -C /usr/local/bin -xz
}

# Install network analysis tools
install_network_tools() {
    log_info "Installing network analysis tools..."
    
    case "$DISTRO" in
        ubuntu|debian)
            apt-get install -y tcpdump tshark ngrep tcpflow \
                               iptraf-ng nmap netcat-openbsd \
                               iproute2 iputils-ping traceroute
            ;;
        fedora|rhel|centos)
            dnf install -y tcpdump wireshark-cli ngrep tcpflow \
                          iptraf-ng nmap nmap-ncat \
                          iproute iputils traceroute
            ;;
        arch|manjaro)
            pacman -S --noconfirm tcpdump wireshark-cli ngrep \
                                  iptraf-ng nmap gnu-netcat \
                                  iproute2 iputils traceroute
            ;;
    esac
}

# Setup eBPF development environment
setup_ebpf_dev() {
    log_info "Setting up eBPF development environment..."
    
    # Install libbpf and development files
    case "$DISTRO" in
        ubuntu|debian)
            apt-get install -y libbpf-dev clang llvm libelf-dev
            ;;
        fedora|rhel|centos)
            dnf install -y libbpf-devel clang llvm elfutils-libelf-devel
            ;;
        arch|manjaro)
            pacman -S --noconfirm libbpf clang llvm elfutils
            ;;
    esac
    
    # Install bpftool
    if ! command -v bpftool &> /dev/null; then
        log_info "Building bpftool from source..."
        git clone --depth 1 https://github.com/libbpf/bpftool.git /tmp/bpftool
        cd /tmp/bpftool/src
        make && make install
        cd - && rm -rf /tmp/bpftool
    fi
    
    # Enable BPF filesystem
    if ! mount | grep -q bpf; then
        log_info "Mounting BPF filesystem..."
        mount -t bpf bpf /sys/fs/bpf/
        echo "bpf /sys/fs/bpf bpf defaults 0 0" >> /etc/fstab
    fi
}

# Configure kernel for optimal eBPF/observability
configure_kernel() {
    log_info "Configuring kernel parameters for observability..."
    
    cat > /etc/sysctl.d/99-tapio.conf << EOF
# Tapio observability optimizations

# Enable BPF JIT compiler
net.core.bpf_jit_enable = 1

# Increase BPF program size limits
kernel.unprivileged_bpf_disabled = 0
kernel.bpf_stats_enabled = 1

# Increase kernel ring buffer
kernel.perf_event_paranoid = -1
kernel.perf_event_mlock_kb = 516

# Network performance
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.ipv4.tcp_rmem = 4096 87380 134217728
net.ipv4.tcp_wmem = 4096 65536 134217728

# Enable kernel tracing
kernel.ftrace_enabled = 1
EOF
    
    sysctl -p /etc/sysctl.d/99-tapio.conf
}

# Setup cgroup v2 for better container monitoring
setup_cgroup_v2() {
    log_info "Setting up cgroup v2..."
    
    # Check if cgroup v2 is already enabled
    if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
        log_success "cgroup v2 already enabled"
    else
        log_info "Enabling cgroup v2 (requires reboot)..."
        
        # Update GRUB to enable cgroup v2
        if [ -f /etc/default/grub ]; then
            sed -i 's/GRUB_CMDLINE_LINUX="/GRUB_CMDLINE_LINUX="systemd.unified_cgroup_hierarchy=1 /' /etc/default/grub
            update-grub || grub2-mkconfig -o /boot/grub2/grub.cfg
            log_warning "Reboot required to enable cgroup v2"
        fi
    fi
}

# Install monitoring stack
install_monitoring() {
    log_info "Installing Prometheus node exporter..."
    
    NODE_EXPORTER_VERSION="1.7.0"
    wget -q "https://github.com/prometheus/node_exporter/releases/download/v${NODE_EXPORTER_VERSION}/node_exporter-${NODE_EXPORTER_VERSION}.linux-amd64.tar.gz"
    tar xzf "node_exporter-${NODE_EXPORTER_VERSION}.linux-amd64.tar.gz"
    cp "node_exporter-${NODE_EXPORTER_VERSION}.linux-amd64/node_exporter" /usr/local/bin/
    rm -rf "node_exporter-${NODE_EXPORTER_VERSION}.linux-amd64"*
    
    # Create systemd service
    cat > /etc/systemd/system/node_exporter.service << EOF
[Unit]
Description=Prometheus Node Exporter
After=network.target

[Service]
Type=simple
User=nobody
Group=nogroup
ExecStart=/usr/local/bin/node_exporter \
    --collector.systemd \
    --collector.processes \
    --collector.tcpstat \
    --collector.mountstats \
    --collector.qdisc
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    systemctl enable node_exporter
}

# Create helper scripts
create_helper_scripts() {
    log_info "Creating helper scripts..."
    
    # eBPF capability setter
    cat > /usr/local/bin/tapio-setcap << 'EOF'
#!/bin/bash
# Grant capabilities to Tapio binaries for unprivileged eBPF
for binary in /opt/tapio/bin/*-collector; do
    if [ -f "$binary" ]; then
        setcap cap_sys_resource,cap_sys_admin,cap_net_admin,cap_net_raw+eip "$binary"
        echo "Capabilities set for $(basename $binary)"
    fi
done
EOF
    chmod +x /usr/local/bin/tapio-setcap
    
    # Performance profiling script
    cat > /usr/local/bin/tapio-profile << 'EOF'
#!/bin/bash
# Profile Tapio performance
DURATION=${1:-30}
OUTPUT_DIR=${2:-/tmp/tapio-profile-$(date +%Y%m%d-%H%M%S)}

mkdir -p "$OUTPUT_DIR"
echo "Profiling for $DURATION seconds to $OUTPUT_DIR..."

# CPU profile
perf record -F 99 -a -g -o "$OUTPUT_DIR/perf.data" -- sleep $DURATION &

# System metrics
sar -A 1 $DURATION > "$OUTPUT_DIR/sar.txt" &

# eBPF stats
while [ $DURATION -gt 0 ]; do
    bpftool prog list > "$OUTPUT_DIR/bpf-progs-$DURATION.txt"
    bpftool map list > "$OUTPUT_DIR/bpf-maps-$DURATION.txt"
    sleep 5
    DURATION=$((DURATION - 5))
done

wait
echo "Profile complete. Results in $OUTPUT_DIR"
EOF
    chmod +x /usr/local/bin/tapio-profile
}

# Main installation flow
main() {
    echo "🚀 Tapio Enhanced Linux Installation"
    echo "===================================="
    echo
    
    # Check for root
    if [[ $EUID -ne 0 ]]; then
        log_error "Please run with sudo: sudo $0"
        exit 1
    fi
    
    # Detect distribution
    detect_distro
    
    # Install components
    install_kernel_headers
    install_bpf_tools
    install_perf_tools
    install_container_tools
    install_network_tools
    setup_ebpf_dev
    configure_kernel
    setup_cgroup_v2
    install_monitoring
    create_helper_scripts
    
    log_success "Enhanced Linux installation complete!"
    echo
    echo "🎯 Next steps:"
    echo "1. Run basic installation: ./scripts/install.sh"
    echo "2. Start development environment: ./scripts/dev-up.sh"
    echo "3. Set capabilities: sudo tapio-setcap"
    echo "4. Profile performance: sudo tapio-profile 60"
    echo
    echo "📚 New tools available:"
    echo "- BCC tools in /usr/local/bin/"
    echo "- bpftool for eBPF inspection"
    echo "- perf for CPU profiling"
    echo "- tcpdump/tshark for network analysis"
    echo "- podman for rootless containers"
    echo
    
    if grep -q "systemd.unified_cgroup_hierarchy=1" /proc/cmdline; then
        log_success "cgroup v2 is active"
    else
        log_warning "Reboot required to activate cgroup v2"
    fi
}

# Run main
main "$@"