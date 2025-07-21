# Tapio Development Scripts

This directory contains automation scripts for setting up and managing the Tapio development environment.

## 📦 Scripts

### `install.sh` - Development Environment Installer
Installs all required tools for Tapio development with cross-platform support (macOS/Linux).

```bash
# Install everything
./scripts/install.sh

# Quick install (no prompts)
./scripts/install.sh --quick

# Skip service startup
./scripts/install.sh --no-services

# Just verify existing installation
./scripts/install.sh --verify-only
```

**Installs:**
- Go (latest)
- Docker
- Colima (macOS) - VM support for eBPF collector
- kubectl, Minikube, Skaffold, Helm
- Development tools (make, jq, curl, buf)

### `dev-up.sh` - Development Environment Manager
Starts and manages all development services.

```bash
# Start all services
./scripts/dev-up.sh

# Start without monitoring
./scripts/dev-up.sh --no-monitoring

# Start without Kubernetes
./scripts/dev-up.sh --no-k8s

# Force rebuild images
./scripts/dev-up.sh --build
```

**Other commands:**
```bash
./scripts/dev-up.sh stop      # Stop all services
./scripts/dev-up.sh restart   # Restart everything  
./scripts/dev-up.sh status    # Show status
./scripts/dev-up.sh logs      # Show recent logs
```

## 🚀 Quick Start Workflow

```bash
# 1. Install everything
./scripts/install.sh

# 2. Start development environment
./scripts/dev-up.sh

# 3. Verify it's working
kubectl get pods -n tapio
docker ps

# 4. Start developing
go run cmd/tapio-collector/main.go
```

## 🔧 What Gets Set Up

### Container Runtime
- **macOS**: Colima with QEMU VM support for eBPF development
- **Linux**: Docker with native kernel access

### Kubernetes
- Minikube cluster with dashboard and metrics
- Tapio namespace and RBAC configuration
- ConfigMaps for collector settings

### Monitoring (Optional)
- Prometheus for metrics collection
- Grafana for visualization (admin/admin)
- ServiceMonitors for Tapio components

### Development Tools
- Protocol buffer compilation (buf)
- Code formatting and linting
- Docker image building and loading

## 🎯 Architecture Support

### Collectors Supported
- **CNI Collector**: Container networking (requires Docker/Colima)
- **eBPF Collector**: Kernel monitoring (requires VM on macOS, native on Linux)
- **K8s Collector**: Kubernetes events (requires Minikube)
- **SystemD Collector**: System logs (Linux native, limited on macOS)

### Development Modes
- **Local Go**: Direct `go run` execution
- **Container**: Docker-based development
- **Kubernetes**: Full K8s deployment with Skaffold
- **Hybrid**: Mix of local and containerized components

## 🐛 Troubleshooting

**Scripts won't run:**
```bash
chmod +x scripts/*.sh
```

**Permission issues:**
```bash
# Linux: Add user to docker group
sudo usermod -aG docker $USER
# Then log out and log back in
```

**Resource issues:**
```bash
# Increase Colima resources
colima stop
colima start --memory=8 --cpu=6

# Increase Minikube resources  
minikube config set memory 4096
minikube config set cpus 4
minikube delete && minikube start
```

**Services won't start:**
```bash
# Check status
./scripts/dev-up.sh status

# Restart everything
./scripts/dev-up.sh restart

# Manual debugging
docker ps
minikube status
kubectl get pods -A
```

For more detailed troubleshooting, see [DEVELOPMENT_SETUP.md](../docs/DEVELOPMENT_SETUP.md).

## 📚 Related Documentation

- [ONBOARDING.md](../ONBOARDING.md) - New contributor guide
- [DEVELOPMENT_SETUP.md](../docs/DEVELOPMENT_SETUP.md) - Detailed setup guide
- [ARCHITECTURE.md](../docs/ARCHITECTURE.md) - System architecture
- [Collector docs](../docs/collectors/) - Individual collector setup