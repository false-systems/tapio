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

### Current Architecture
Tapio is built with a modular, extensible foundation:

```
tapio/
├── cmd/tapio/          # CLI entry point
├── pkg/
│   ├── k8s/           # Kubernetes client integration
│   ├── simple/        # Basic health checking
│   ├── metrics/       # Prometheus metrics
│   └── ebpf/          # eBPF framework (foundation ready)
└── deploy/helm/       # Kubernetes deployment
```

### Future Vision: Complete System Intelligence

We're designing Tapio to eventually provide unprecedented visibility into Kubernetes systems. Here's the architecture we're working toward:

```
┌─ User Experience ────────────────────────────────────────┐
│ tapio check → "Pod will OOM in 7 minutes"               │
│ tapio why   → "Memory leak in /api/users endpoint"       │
│ tapio fix   → "Applied memory limit increase"           │
└──────────────────────────────────────────────────────────┘
                             │
┌─ Correlation Engine ─────────────────────────────────────┐
│ • Timeline analysis across all data sources             │
│ • Pattern recognition for known failure modes           │
│ • Confidence scoring for predictions                    │
│ • Human-readable root cause explanations                │
└──────────────────────────────────────────────────────────┘
                             │
┌─ Multi-Layer Data Collection ───────────────────────────┐
│                                                          │
│ eBPF Layer (In Development)                             │
│ ├─ Memory: allocation tracking, leak detection          │
│ ├─ Network: packet analysis, connection mapping         │
│ ├─ Process: syscall patterns, resource usage            │
│ └─ Performance: CPU scheduling, I/O bottlenecks         │
│                                                          │
│ System Layer (Planned)                                  │
│ ├─ systemd: service health, restart patterns           │
│ ├─ journald: log analysis, error correlation            │
│ └─ container runtime: lifecycle events                  │
│                                                          │
│ Kubernetes Layer (Working Today) ✅                     │
│ ├─ API resources: pods, deployments, services           │
│ ├─ Events: scheduling, failures, scaling                │
│ └─ Metrics: resource usage, health status               │
└──────────────────────────────────────────────────────────┘
```

#### The Big Idea: Predictive Kubernetes Debugging

Instead of reactive debugging ("why did it crash?"), we want to enable predictive insights:

**Today's Debugging:**
```bash
# Something breaks first, then you investigate
kubectl get pods  → CrashLoopBackOff
kubectl logs pod  → "OutOfMemoryError"
kubectl describe  → "Container was OOMKilled"
```

**Tapio's Vision:**
```bash
# Catch problems before they happen
tapio check → "WARNING: api-service will OOM in 7m23s"
tapio why   → "Memory leak: 18MB/min growth in user session cache"
tapio fix   → "Recommendation: Increase memory limit to 512Mi"
```

#### Technical Approach: Correlation Across Layers

The key insight is that Kubernetes problems usually have signatures across multiple system layers:

```
Problem: Memory Leak → OOM → Pod Restart
├─ eBPF sees: Growing heap allocations, no corresponding frees
├─ systemd sees: containerd memory pressure warnings  
├─ journald sees: "Memory cgroup out of memory" messages
└─ Kubernetes sees: OOMKilled event, pod restart

Tapio correlates these signals to predict the OOM before it happens
```

#### Why This Approach Could Work

1. **Layer Correlation**: Most K8s issues leave traces across multiple system layers
2. **Early Signals**: Kernel/system events often precede K8s-visible failures  
3. **Pattern Recognition**: Similar failure modes create recognizable signatures
4. **Explainable AI**: Rule-based correlation provides clear explanations

#### Development Philosophy

We're building this incrementally:
- ✅ **Foundation First**: Solid CLI, K8s integration, metrics (working today)
- 🚧 **Add Layers Gradually**: eBPF, then systemd, then advanced correlation
- 🔮 **Keep It Simple**: Complex backend, simple frontend ("just run `tapio check`")
- 📚 **Learn and Iterate**: Real-world testing drives feature priorities

The goal isn't to replace existing tools, but to provide the "first command" you run when something seems wrong - the one that gives you the clearest picture of what's actually happening in your cluster.

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
