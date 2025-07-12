# Tapio 🌲

> **Work in Progress** - A simple Kubernetes debugging tool

Tapio is being developed to make Kubernetes debugging more accessible. Currently in early development with basic functionality working.

## What Works Today

### Basic Commands
```bash
# Check the health of your Kubernetes resources
tapio check

# Export metrics for Prometheus
tapio prometheus --port 8080

# Show version information
tapio version
```

### Current Features
- ✅ **Simple Health Checks** - Basic analysis of pods and deployments
- ✅ **Kubernetes Integration** - Works with your existing kubeconfig
- ✅ **Prometheus Metrics** - Exports health metrics for monitoring
- ✅ **Clean CLI** - Human-readable output, no configuration required

## Installation

### Prerequisites
- Kubernetes cluster access (if `kubectl get pods` works, Tapio will work)
- Go 1.21+ (for building from source)

### Build from Source
```bash
git clone https://github.com/your-org/tapio
cd tapio
make build
./bin/tapio check
```

## Example Output

```bash
$ tapio check
HEALTHY: 3 pods running normally
WARNING: 1 pod has resource issues
  - api-service: Memory usage at 85% of limit

READY: 2/3 deployments fully available
```

## Planned Features (Work in Progress)

We're working on adding more advanced capabilities:

- 🚧 **eBPF Integration** - Kernel-level insights for deeper debugging
- 🚧 **Advanced Correlation** - Connect Kubernetes events with system behavior  
- 🚧 **Predictive Analysis** - Early warning for potential issues
- 🚧 **Multi-layer Monitoring** - System, network, and application insights

## Architecture

Tapio is built with a modular architecture:

```
tapio/
├── cmd/tapio/          # CLI entry point
├── pkg/
│   ├── k8s/           # Kubernetes client integration
│   ├── simple/        # Basic health checking
│   ├── metrics/       # Prometheus metrics
│   └── ebpf/          # eBPF framework (in development)
└── deploy/helm/       # Kubernetes deployment
```

## Development Status

### Completed Components
- [x] CLI framework with Cobra
- [x] Kubernetes API integration
- [x] Basic health analysis
- [x] Prometheus metrics export
- [x] Helm deployment charts
- [x] Cross-platform builds

### In Development
- [ ] eBPF-based system monitoring
- [ ] Advanced correlation engine
- [ ] systemd integration
- [ ] Network monitoring
- [ ] Performance optimization

## Contributing

This is an early-stage project and we welcome contributions! Areas where help is especially appreciated:

- Testing on different Kubernetes distributions
- eBPF program development
- Documentation improvements
- Feature suggestions and bug reports

### Development Setup
```bash
# Install development tools
make setup

# Run tests
make test

# Build and test locally
make build
./bin/tapio check
```

## Configuration

Tapio works with zero configuration by default. It uses your existing Kubernetes configuration from:
- `~/.kube/config`
- `KUBECONFIG` environment variable  
- In-cluster service account (when running as a pod)

## Deployment

### Kubernetes Deployment
```bash
# Deploy as a DaemonSet for cluster-wide monitoring
helm install tapio ./deploy/helm/tapio

# Or run locally
./bin/tapio check --all-namespaces
```

## Roadmap

### Short Term (Current Focus)
- Improve health analysis accuracy
- Add more Kubernetes resource types
- Enhanced error messages and debugging

### Medium Term
- eBPF integration for system-level insights
- Advanced correlation between K8s and system events
- Performance monitoring and predictions

### Long Term
- Machine learning for anomaly detection
- Integration with popular monitoring stacks
- Multi-cluster support

## Why Tapio?

Kubernetes debugging often requires deep expertise and multiple tools. Tapio aims to:

- **Simplify** - One command to understand what's wrong
- **Explain** - Clear, human-readable explanations
- **Predict** - Catch issues before they become problems
- **Integrate** - Work with existing tools and workflows

Named after the Finnish forest god, representing the deep roots needed to understand complex systems.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/your-org/tapio/issues)
- **Discussions**: [GitHub Discussions](https://github.com/your-org/tapio/discussions)
- **Documentation**: [Wiki](https://github.com/your-org/tapio/wiki)

---

**Note**: This project is in active development. APIs and commands may change as we iterate toward v1.0. We appreciate your patience and feedback!
