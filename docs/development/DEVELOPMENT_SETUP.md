# Tapio Development Setup Guide

This guide helps you set up a complete local development environment for Tapio that can run all collectors on the same laptop.

## 🎯 Overview

The development environment includes:
- **Colima** (macOS) or **Docker** (Linux) - Container runtime with VM support for eBPF
- **Minikube** - Local Kubernetes cluster for K8s collector
- **Skaffold** - Kubernetes development workflow
- **Monitoring Stack** - Prometheus + Grafana for observability

## 🚀 Quick Start

### 1. One-Command Install

```bash
# Install everything automatically
./scripts/install.sh

# Or with options
./scripts/install.sh --quick --no-services
```

### 2. Start Development Environment

```bash
# Start all services
./scripts/dev-up.sh

# Or with specific options
./scripts/dev-up.sh --no-monitoring
```

### 3. Verify Installation

```bash
# Check everything is working
./scripts/install.sh --verify-only
```

## 📋 Manual Installation (if needed)

### Prerequisites

**macOS:**
- Homebrew
- Xcode Command Line Tools

**Linux:**
- Package manager (apt/yum/pacman)
- sudo access

### Core Tools

```bash
# Go (latest)
# macOS
brew install go

# Linux
wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz -O /tmp/go.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

# Docker
# macOS
brew install docker

# Linux (Ubuntu)
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
sudo usermod -aG docker $USER

# Colima (macOS only - for VM support needed by eBPF)
brew install colima qemu

# Kubernetes Tools
brew install kubectl minikube skaffold helm  # macOS
# Linux: see install script for detailed instructions
```

## 🏗️ Architecture-Specific Setup

### eBPF Collector (Requires VM/Linux)

The eBPF collector needs kernel access, so:

**On macOS:**
```bash
# Colima with QEMU for VM support
colima start --vm-type=qemu --memory=6 --cpu=4 --kubernetes
```

**On Linux:**
```bash
# Native Docker (has kernel access)
sudo systemctl start docker
```

### CNI Collector (Container Networks)

Monitors container network interfaces:

```bash
# Needs Docker/Colima running
docker network create tapio-test
```

### K8s Collector (Kubernetes Events)

Monitors Kubernetes API:

```bash
# Start Minikube
minikube start --driver=docker --memory=4096

# Enable addons
minikube addons enable dashboard metrics-server
```

### SystemD Collector (System Logs)

Monitors system logs:

```bash
# Linux: Works natively with journald
systemctl status

# macOS: Limited functionality (no systemd)
# Use CNI/eBPF collectors instead
```

## 🔧 Development Workflows

### 1. Local Development (Go)

```bash
# Run collector locally
go run cmd/tapio-collector/main.go --collectors=cni,k8s

# Run server locally  
go run cmd/tapio-server/main.go

# Run specific collector
cd pkg/collectors/cni
go test ./...
```

### 2. Container Development

```bash
# Build development image
docker build -f Dockerfile.dev -t tapio-dev .

# Run in container
docker run -it --privileged --pid=host tapio-dev

# For eBPF development (needs privileged mode)
docker run -it --privileged --pid=host -v /sys/fs/bpf:/sys/fs/bpf tapio-dev
```

### 3. Kubernetes Development

```bash
# Deploy to Minikube
kubectl apply -f deploy/k8s/

# Use Skaffold for hot reload
skaffold dev

# Port forward for testing
kubectl port-forward svc/tapio-server 8080:80
```

### 4. Full Stack Development

```bash
# Start everything
./scripts/dev-up.sh

# Check status
kubectl get pods -n tapio
minikube dashboard

# View logs
kubectl logs -n tapio -l app=tapio-collector -f
```

## 🎛️ Service Management

### Container Runtime

```bash
# macOS (Colima)
colima start --vm-type=qemu --kubernetes    # Start with K8s
colima status                               # Check status
colima stop                                 # Stop

# Linux (Docker)
sudo systemctl start docker
sudo systemctl status docker
sudo systemctl stop docker
```

### Kubernetes

```bash
# Minikube
minikube start                    # Start cluster
minikube status                   # Check status
minikube dashboard                # Open dashboard
minikube stop                     # Stop cluster

# kubectl
kubectl config current-context   # Current context
kubectl get nodes                 # Check cluster
kubectl get pods -A              # All pods
```

### Monitoring

```bash
# Access Grafana (password: admin)
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80

# Access Prometheus
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090

# Check metrics
curl localhost:9090/api/v1/query?query=up
```

## 🐛 Troubleshooting

### Common Issues

**"Docker daemon not running"**
```bash
# macOS
colima start

# Linux
sudo systemctl start docker
```

**"eBPF programs won't load"**
```bash
# Ensure VM support (macOS)
colima start --vm-type=qemu

# Ensure privileged mode (containers)
docker run --privileged ...
```

**"Minikube won't start"**
```bash
# Check driver
minikube config view
minikube delete && minikube start --driver=docker

# Check resources
minikube start --memory=4096 --cpus=2
```

**"Build failures"**
```bash
# Format code first
make fmt

# Check dependencies
go mod tidy
go mod download

# Build incrementally
go build ./pkg/domain
go build ./pkg/collectors/cni
```

### Service Status Commands

```bash
# Quick health check
./scripts/dev-up.sh status

# Detailed diagnostics
docker ps                         # Containers
minikube status                   # Kubernetes
kubectl get pods -A              # All pods
helm list -A                     # Helm releases

# Logs
docker logs <container>          # Container logs
kubectl logs -n tapio <pod>      # Pod logs
minikube logs                    # Minikube logs
```

### Resource Issues

```bash
# Check resource usage
docker stats
kubectl top nodes
kubectl top pods -A

# Free up space
docker system prune -a           # Docker cleanup
minikube delete && minikube start # Fresh cluster

# Colima resource adjustment
colima stop
colima start --memory=8 --cpu=4 --disk=60
```

## 🔬 Testing Different Scenarios

### 1. Multi-Collector Testing

```bash
# Test all collectors together
go run cmd/tapio-collector/main.go --collectors=cni,k8s,systemd

# Test specific combinations
go run cmd/tapio-collector/main.go --collectors=cni,k8s
```

### 2. Event Generation

```bash
# Generate CNI events
docker run --rm nginx
docker network create test-net

# Generate K8s events  
kubectl create deployment test --image=nginx
kubectl scale deployment test --replicas=3
kubectl delete deployment test

# Generate system events (Linux)
logger "Test message for Tapio"
```

### 3. Load Testing

```bash
# High event volume
for i in {1..100}; do
  kubectl create job test-$i --image=busybox -- sleep 1
done

# Monitor performance
kubectl top pods -n tapio
```

## 📊 Performance Optimization

### Resource Allocation

```bash
# Colima (macOS)
colima start --memory=8 --cpu=6 --disk=80

# Minikube  
minikube config set memory 4096
minikube config set cpus 4
```

### Build Optimization

```bash
# Parallel builds
export GOMAXPROCS=$(nproc)

# Module cache
export GOPROXY=https://proxy.golang.org,direct
export GOSUMDB=sum.golang.org

# Docker build cache
export DOCKER_BUILDKIT=1
```

## 🎯 Next Steps

1. **Run the installer**: `./scripts/install.sh`
2. **Start services**: `./scripts/dev-up.sh`  
3. **Test locally**: `go run cmd/tapio-collector/main.go`
4. **Deploy to K8s**: `kubectl apply -f deploy/k8s/`
5. **Use Skaffold**: `skaffold dev`

For more details, see:
- [ONBOARDING.md](../ONBOARDING.md) - Project overview
- [Architecture docs](../docs/ARCHITECTURE.md) - System design
- [Collector docs](../docs/collectors/) - Individual collector setup