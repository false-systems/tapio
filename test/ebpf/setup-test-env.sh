#!/bin/bash
set -euo pipefail

echo "🧪 Setting up Tapio eBPF testing environment..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if running on Linux
if [[ "$OSTYPE" != "linux-gnu"* ]]; then
    echo -e "${RED}❌ eBPF testing requires Linux. Current OS: $OSTYPE${NC}"
    exit 1
fi

# Check for required commands
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo -e "${RED}❌ $1 is required but not installed${NC}"
        exit 1
    fi
}

echo -e "${BLUE}🔍 Checking prerequisites...${NC}"
check_command "minikube"
check_command "kubectl"
check_command "docker"

# Check if we can run eBPF programs
echo -e "${BLUE}🔍 Checking eBPF capabilities...${NC}"
if [[ $EUID -ne 0 ]]; then
    echo -e "${YELLOW}⚠️  eBPF requires root privileges. Run with sudo.${NC}"
    echo "Re-running with sudo..."
    exec sudo "$0" "$@"
fi

# Install eBPF dependencies
echo -e "${BLUE}📦 Installing eBPF dependencies...${NC}"
./../../scripts/install-ebpf-deps.sh

# Start Minikube with appropriate settings for eBPF
echo -e "${BLUE}🚀 Starting Minikube with eBPF support...${NC}"
minikube start \
    --driver=docker \
    --cpus=4 \
    --memory=8192 \
    --kubernetes-version=v1.28.0 \
    --extra-config=kubelet.allowed-unsafe-sysctls=net.core.bpf_jit_enable

# Wait for cluster to be ready
echo -e "${BLUE}⏳ Waiting for cluster to be ready...${NC}"
kubectl wait --for=condition=Ready nodes --all --timeout=300s

# Build Tapio with eBPF support
echo -e "${BLUE}🔨 Building Tapio with eBPF support...${NC}"
cd ../../
make build-enhanced-ebpf
cd tests/ebpf/

# Create test namespace
echo -e "${BLUE}🏗️  Creating test namespace...${NC}"
kubectl create namespace tapio-ebpf-tests --dry-run=client -o yaml | kubectl apply -f -

# Deploy test applications
echo -e "${BLUE}🚀 Deploying test applications...${NC}"
kubectl apply -f ./test-apps/

# Wait for test apps to be ready
echo -e "${BLUE}⏳ Waiting for test applications...${NC}"
kubectl wait --for=condition=Ready pods -n tapio-ebpf-tests --all --timeout=120s

echo -e "${GREEN}✅ eBPF testing environment ready!${NC}"
echo ""
echo -e "${YELLOW}🧪 Available test commands:${NC}"
echo "  ./test-memory-leak.sh     - Test memory leak detection"
echo "  ./test-oom-prediction.sh  - Test OOM prediction timing"
echo "  ./test-process-mapping.sh - Test pod-to-process correlation"
echo "  ./run-all-tests.sh       - Run complete test suite"
echo ""
echo -e "${BLUE}📊 Monitor tests with:${NC}"
echo "  kubectl get pods -n tapio-ebpf-tests -w"
echo "  sudo ../../bin/tapio check --namespace tapio-ebpf-tests --verbose"