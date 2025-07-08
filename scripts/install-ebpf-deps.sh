#!/bin/bash
# Tapio eBPF Dependencies Installer

set -e

echo "🔍 Detecting Linux distribution..."

# Detect distro
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    VER=$VERSION_ID
else
    echo "❌ Cannot detect Linux distribution"
    exit 1
fi

# Check kernel version
KERNEL_VERSION=$(uname -r | cut -d. -f1,2)
KERNEL_MAJOR=$(echo $KERNEL_VERSION | cut -d. -f1)
KERNEL_MINOR=$(echo $KERNEL_VERSION | cut -d. -f2)

echo "📊 System Info:"
echo "   OS: $OS $VER"
echo "   Kernel: $(uname -r)"

# Check minimum kernel version (4.14)
if [ "$KERNEL_MAJOR" -lt 4 ] || ([ "$KERNEL_MAJOR" -eq 4 ] && [ "$KERNEL_MINOR" -lt 14 ]); then
    echo "❌ Kernel version too old for eBPF (need 4.14+, have $KERNEL_VERSION)"
    echo "   Consider upgrading your kernel or using Tapio without eBPF features"
    exit 1
fi

# Install dependencies based on distro
case $OS in
    ubuntu|debian)
        echo "📦 Installing Ubuntu/Debian packages..."
        sudo apt-get update
        sudo apt-get install -y \
            clang \
            llvm \
            libbpf-dev \
            linux-headers-$(uname -r) \
            build-essential \
            pkg-config
        
        # Install bpftool - handle Ubuntu 24.04+ differently
        if sudo apt-get install -y linux-tools-$(uname -r) 2>/dev/null; then
            echo "✅ Installed linux-tools for current kernel"
        elif sudo apt-get install -y linux-tools-common linux-tools-generic 2>/dev/null; then
            echo "✅ Installed generic linux-tools"
        else
            echo "⚠️  Could not install bpftool, eBPF will work with limited features"
        fi
        ;;
    
    fedora)
        echo "📦 Installing Fedora packages..."
        sudo dnf install -y \
            clang \
            llvm \
            libbpf-devel \
            kernel-headers-$(uname -r) \
            kernel-devel-$(uname -r) \
            make \
            pkg-config \
            bpftool
        ;;
    
    rhel|centos|rocky|almalinux)
        echo "📦 Installing RHEL-based packages..."
        sudo yum install -y epel-release
        sudo yum install -y \
            clang \
            llvm \
            libbpf-devel \
            kernel-headers-$(uname -r) \
            kernel-devel-$(uname -r) \
            make \
            pkg-config
        # bpftool might not be available, compile from source if needed
        if ! command -v bpftool &> /dev/null; then
            echo "⚠️  bpftool not available in repos, continuing without it"
        fi
        ;;
    
    arch|manjaro)
        echo "📦 Installing Arch packages..."
        sudo pacman -Syu --noconfirm \
            clang \
            llvm \
            libbpf \
            linux-headers \
            base-devel \
            pkg-config \
            bpf
        ;;
    
    opensuse|suse)
        echo "📦 Installing openSUSE packages..."
        sudo zypper install -y \
            clang \
            llvm \
            libbpf-devel \
            kernel-devel \
            make \
            pkg-config
        ;;
    
    *)
        echo "⚠️  Unknown distribution: $OS"
        echo "   Please install manually:"
        echo "   - clang"
        echo "   - llvm" 
        echo "   - libbpf-dev"
        echo "   - kernel headers"
        exit 1
        ;;
esac

# Verify installation
echo ""
echo "🔍 Verifying installation..."

# Check clang
if command -v clang &> /dev/null; then
    echo "✅ clang: $(clang --version | head -n1)"
else
    echo "❌ clang not found"
    exit 1
fi

# Check kernel headers
if [ -d "/lib/modules/$(uname -r)/build" ]; then
    echo "✅ Kernel headers: Found"
else
    echo "❌ Kernel headers not found for $(uname -r)"
    exit 1
fi

# Check BTF support (best eBPF experience)
if [ -f "/sys/kernel/btf/vmlinux" ]; then
    echo "✅ BTF support: Available (excellent!)"
else
    echo "⚠️  BTF support: Not available (eBPF will work but with limitations)"
fi

# Check BPF filesystem
if mount | grep -q bpf; then
    echo "✅ BPF filesystem: Mounted"
else
    echo "⚠️  BPF filesystem not mounted, mounting..."
    sudo mount -t bpf bpf /sys/fs/bpf/ 2>/dev/null || true
fi

echo ""
echo "🎉 eBPF dependencies installed successfully!"
echo ""
echo "📝 Next steps:"
echo "   1. Build Tapio with eBPF: make build"
echo "   2. Run with sudo: sudo ./bin/tapio check"
echo ""

# Kernel feature check
if [ "$KERNEL_MAJOR" -eq 4 ] && [ "$KERNEL_MINOR" -lt 18 ]; then
    echo "⚠️  Note: Kernel $KERNEL_VERSION has limited eBPF features"
    echo "   For best experience, consider upgrading to kernel 5.4+"
elif [ "$KERNEL_MAJOR" -ge 5 ] && [ "$KERNEL_MINOR" -ge 4 ]; then
    echo "🚀 Kernel $KERNEL_VERSION has excellent eBPF support!"
fi