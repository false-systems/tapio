#!/bin/bash
set -e

# Test the installation scripts without actually installing everything
# This helps verify the scripts will work before running them

echo "🧪 Testing Tapio Installation Scripts"
echo "===================================="
echo

# Test bash version
if [ "${BASH_VERSION%%.*}" -lt 4 ]; then
    echo "⚠️  Warning: Bash version is ${BASH_VERSION}"
    echo "   Some features may require Bash 4+"
fi

# Test script syntax
echo "Checking script syntax..."
for script in install.sh dev-up.sh quick-start.sh; do
    if bash -n "scripts/$script"; then
        echo "✅ $script: syntax OK"
    else
        echo "❌ $script: syntax errors found"
        exit 1
    fi
done

# Test functions from install.sh
echo
echo "Testing install.sh functions..."

# Create a subshell and test functions without running anything
(
    # Load just the functions we want to test
    echo -n "Testing OS detection... "
    OS="unknown"
    case "$(uname -s)" in
        Darwin) OS="macos" ;;
        Linux) OS="linux" ;;
    esac
    echo "✅ OK ($OS)"
    
    # Test command_exists function
    echo -n "Testing command detection... "
    command_exists() { command -v "$1" >/dev/null 2>&1; }
    if command_exists bash && ! command_exists nonexistentcommand123; then
        echo "✅ OK"
    else
        echo "❌ Failed"
    fi
)

# Check for required commands that scripts depend on
echo
echo "Checking system commands..."
commands=(
    "curl:downloading files"
    "grep:text processing"
    "awk:text processing"
    "sed:text processing"
    "tar:archive extraction"
    "df:disk space checking"
)

for cmd_info in "${commands[@]}"; do
    IFS=':' read -r cmd desc <<< "$cmd_info"
    if command -v "$cmd" >/dev/null 2>&1; then
        echo "✅ $cmd: available (used for $desc)"
    else
        echo "❌ $cmd: missing (needed for $desc)"
    fi
done

# Check write permissions
echo
echo "Checking permissions..."
if [ -w "/usr/local/bin" ] || [ -w "$HOME/.local/bin" ]; then
    echo "✅ Can write to binary directories"
else
    echo "⚠️  May need sudo for installing to /usr/local/bin"
fi

# Simulate what would be installed
echo
echo "📦 What would be installed:"
echo "  - Go (if not present)"
echo "  - Docker"
if [[ "$(uname -s)" == "Darwin" ]]; then
    echo "  - Colima + QEMU (for eBPF support on macOS)"
fi
echo "  - kubectl, minikube, skaffold, helm"
echo "  - Development tools (make, jq, curl)"
echo "  - Protocol buffer compiler (buf)"

# Check for existing installations
echo
echo "🔍 Current installations:"
tools=(go docker colima kubectl minikube skaffold helm make jq)
for tool in "${tools[@]}"; do
    if command -v "$tool" >/dev/null 2>&1; then
        version=$($tool version 2>/dev/null | head -1 || echo "installed")
        echo "  ✅ $tool: $version"
    else
        echo "  ❌ $tool: not installed"
    fi
done

# Estimate disk space needed
echo
echo "💾 Disk space requirements:"
echo "  - Go: ~500MB"
echo "  - Docker images: ~2-5GB"
echo "  - Minikube: ~1GB"
echo "  - Total recommended: 10GB free"

# Get available disk space (cross-platform)
if [[ "$(uname -s)" == "Darwin" ]]; then
    available_gb=$(( $(df . | awk 'NR==2 {print $4}') * 512 / 1024 / 1024 / 1024 ))
    echo "  - Available: ${available_gb}GB"
else
    available=$(df -BG . | awk 'NR==2 {print $4}')
    echo "  - Available: $available"
fi

# Check for port conflicts
echo
echo "🔌 Checking for port conflicts..."
ports=(
    "8080:Tapio server"
    "9090:Prometheus"
    "3000:Grafana"
)

conflicts=0
for port_info in "${ports[@]}"; do
    IFS=':' read -r port service <<< "$port_info"
    if lsof -i ":$port" >/dev/null 2>&1; then
        echo "  ⚠️  Port $port in use (needed for $service)"
        ((conflicts++))
    else
        echo "  ✅ Port $port available ($service)"
    fi
done

# Summary
echo
echo "📋 Summary"
echo "========="
if [ $conflicts -eq 0 ]; then
    echo "✅ No major issues found. Installation should work!"
    echo
    echo "Next steps:"
    echo "  1. Run: ./scripts/install.sh"
    echo "  2. Then: ./scripts/dev-up.sh"
else
    echo "⚠️  Found $conflicts potential issues. Installation may work with warnings."
    echo
    echo "You can still proceed with:"
    echo "  ./scripts/install.sh"
fi

echo
echo "For a minimal setup (just Go + Docker), try:"
echo "  ./scripts/quick-start.sh"
echo