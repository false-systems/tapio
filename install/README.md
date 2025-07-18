# Tapio Installation

Multiple ways to install Tapio - choose what works best for you.

## Quick Install (Recommended)

```bash
curl -sSL https://tapio.run | sh
```

This script will:
- Detect your environment automatically
- Install the appropriate version for your OS/arch
- Set up basic configuration
- Show you next steps

## Manual Installation

### Download Binary

1. Go to [Releases](https://github.com/yairfalse/tapio/releases)
2. Download the binary for your platform
3. Extract and move to your PATH:

```bash
tar -xzf tapio_*.tar.gz
sudo mv tapio /usr/local/bin/
```

### Build from Source

```bash
git clone https://github.com/yairfalse/tapio.git
cd tapio
go build -o tapio ./cmd/tapio
sudo mv tapio /usr/local/bin/
```

## Kubernetes Installation

### Option 1: Helm (Recommended)

```bash
# Add repository
helm repo add tapio https://charts.tapio.io
helm repo update

# Install
helm install tapio tapio/tapio --namespace tapio-system --create-namespace
```

### Option 2: Auto-installer

```bash
curl -sSL https://install.tapio.io/k8s | sh
```

### Option 3: kubectl

```bash
kubectl apply -f https://raw.githubusercontent.com/falseyair/tapio/main/deploy/manifests/install.yaml
```

## Configuration

### Environment Detection

Tapio automatically detects:
- Operating system and architecture
- Kubernetes cluster access
- eBPF support (Linux kernel 4.18+)
- Available tools (helm, docker, etc.)

### Configuration File

Default location: `~/.tapio/config.yaml`

```yaml
# Update interval for health checks
update_interval: 30s

# Memory usage thresholds
memory_thresholds:
  warning: 80   # Percentage
  critical: 90  # Percentage

# OOM prediction settings
prediction:
  enabled: true
  min_confidence: 0.8
  time_window: 10m

# Logging
logging:
  level: info
  format: text

# Namespace filtering
namespaces:
  exclude:
    - kube-system
    - kube-public
```

## Verification

After installation, verify Tapio is working:

```bash
# Check version
tapio --version

# Test cluster access
tapio check

# Get help
tapio help
```

## Architecture Options

### CLI Only (Default)
- Lightweight
- Run on your local machine
- Connect to any Kubernetes cluster

### In-Cluster Deployment
- DaemonSet for eBPF collection on each node
- Deployment for centralized API and metrics
- Service for internal communication

### Hybrid
- CLI for interactive use
- In-cluster for monitoring and alerting

## Requirements

### Minimum
- Kubernetes cluster access (any version)
- kubectl configured

### Recommended
- Linux with kernel 4.18+ (for eBPF features)
- Helm 3.0+ (for easy installation)
- Prometheus (for metrics collection)

### Optional
- Docker/Podman (for building from source)
- Go 1.21+ (for development)

## Troubleshooting

### Permission Issues
```bash
# Install to user directory instead
curl -sSL https://tapio.run | INSTALL_DIR=$HOME/.local/bin sh
```

### Cluster Access
```bash
# Check kubectl access
kubectl cluster-info

# Check current context
kubectl config current-context

# Switch context if needed
kubectl config use-context <context-name>
```

### eBPF Issues
```bash
# Check kernel version
uname -r

# Check eBPF support
# Kernel 4.18+ required for full features
# Tapio will fall back to Kubernetes API metrics if eBPF unavailable
```

## Uninstall

### CLI Binary
```bash
sudo rm /usr/local/bin/tapio
rm -rf ~/.tapio
```

### Kubernetes (Helm)
```bash
helm uninstall tapio -n tapio-system
```

### Kubernetes (kubectl)
```bash
kubectl delete namespace tapio-system
```

## Next Steps

After installation:

1. **Try basic commands:**
   ```bash
   tapio check          # Health check current namespace
   tapio check --all    # Check all namespaces
   tapio why <pod>      # Understand a problematic pod
   ```

2. **Set up monitoring:**
   ```bash
   tapio prometheus     # Start Prometheus metrics server
   ```

3. **Deploy in cluster:**
   ```bash
   tapio install        # Deploy Tapio as a service
   ```

4. **Explore advanced features:**
   ```bash
   tapio predict        # Get OOM predictions
   tapio explain        # Detailed explanations
   ```

## Support

- Documentation: https://docs.tapio.io
- Issues: https://github.com/yairfalse/tapio/issues
- Discussions: https://github.com/yairfalse/tapio/discussions