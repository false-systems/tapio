# Tapio 🌲
**Making Kubernetes debugging actually make sense**

Named after Tapio, the Finnish forest god, because debugging Kubernetes clusters shouldn't require divine intervention.

---

## Why This Exists

We got tired of running `kubectl describe pod` and staring at 50 lines of YAML, wondering "What does this actually mean?"

Tapio bridges that gap—turning Kubernetes complexity into something humans can actually understand and act on.

## What It Does (Right Now)

**Simple premise**: Run one command, get clear answers about your cluster health.

```bash
tapio check
```

**What you get:**
- Plain English explanations of pod problems
- Actionable fix suggestions with exact commands
- Pattern recognition for common issues (restart loops, resource pressure)
- Zero configuration—works with your existing kubectl setup

**Example:**
```bash
$ tapio check my-app

ANALYSIS: my-app has issues that need attention

pod/my-app-7d4b9c8f: High restart count
  Container 'api' restarted 8 times in last hour
  Pattern: Consistent OOMKilled events
  
  Likely cause: Memory limit (256Mi) too low for workload
  
  Next steps:
  [1] kubectl logs my-app-7d4b9c8f --previous  # Check what caused the crash
  [2] kubectl top pod my-app-7d4b9c8f          # See current memory usage
  
  Suggested fix:
  kubectl patch deployment my-app -p '{"spec":{"template":{"spec":{"containers":[{"name":"api","resources":{"limits":{"memory":"512Mi"}}}]}}}}'
```

## Current Capabilities

| What it does | How it helps |
|-------------|-------------|
| **Health Analysis** | Quickly spot problematic pods across your cluster |
| **Pattern Recognition** | Identify restart loops, resource pressure, stuck pods |
| **Plain English Explanations** | "Your pod is OOMKilling" instead of "Exit code 137" |
| **Actionable Commands** | Exact kubectl commands to investigate and fix issues |
| **Multiple Scopes** | Check single pods, apps, namespaces, or entire clusters |
| **OpenTelemetry Integration** | Enhanced distributed tracing with correlation analysis |

## Installation & Usage

```bash
# Install
go install github.com/yairfalse/tapio/cmd/tapio@latest

# Use
tapio check                    # Current namespace
tapio check my-app             # Specific deployment  
tapio check pod/my-pod-xyz     # Specific pod
tapio check --all              # Entire cluster
tapio check --output json      # Machine-readable output

# OpenTelemetry Exporter (NEW!)
tapio opentelemetry            # Start OTEL exporter with correlation tracing
tapio opentelemetry --enable-timeline --correlation-window 1h  # Enhanced timeline visualization
```

## Design Philosophy

**1. Clear Goals**
We're building the debugging tool we wish we had during those 3 AM incidents.

**2. Human First**
If you need a manual to understand the output, we failed. Clear beats clever.

**3. Action Oriented**
Don't just tell someone their pod is broken—tell them exactly how to fix it.

**4. Real Solutions First**
We focus on building something genuinely useful today while working toward bigger goals.

## How It Works

**Current Architecture (v0.x):**
```
kubectl/K8s API → Tapio Analysis → Human-Readable Output
```

1. **Data Collection**: Uses your existing kubeconfig to fetch pod, deployment, and event data
2. **Pattern Analysis**: Applies intelligent heuristics to identify common failure patterns
3. **Human Translation**: Converts technical states into clear explanations
4. **Action Generation**: Suggests specific debugging and fix commands

**What makes it effective**: Context-aware analysis that understands the difference between temporary startup issues and real problems.

## OpenTelemetry Integration (NEW!)

Tapio now includes comprehensive OpenTelemetry support for distributed tracing and observability:

**Enhanced Correlation Analysis**
- Multi-layer system analysis across eBPF, Kubernetes, systemd, and network layers
- Automatic correlation of events across different data sources
- Confidence scoring for identified patterns

**Timeline Visualization**
- Visual representation of event causation over time
- Heatmap analysis for identifying hotspots
- Event flow tracking (sequential, parallel, branching)

**Root Cause Analysis**
- Automated root cause determination with confidence scoring
- Impact chain visualization showing propagation
- Actionable recommendations for resolution

See [Enhanced OTEL Tracing Documentation](docs/otel-enhanced-tracing.md) for detailed information.

## Roadmap (Realistic Timeline)

**Near Term (Next 3-6 months):**
- [ ] Enhanced pattern recognition for complex scenarios
- [ ] Improved resource usage analysis and trending
- [ ] Advanced troubleshooting workflows
- [ ] Performance optimizations for large clusters

**Medium Term (6-12 months):**
- [ ] eBPF integration for kernel-level insights
- [ ] Predictive analysis based on resource trends
- [ ] Network connectivity debugging
- [ ] Integration with monitoring ecosystems

**Long Term (Vision):**
- [ ] Intelligent failure prediction with confidence scoring
- [ ] Automated remediation suggestions
- [ ] Multi-cluster support and federation
- [ ] Advanced correlation analysis across systems

## Development Status

**Production Ready:**
- Core Kubernetes analysis engine
- CLI interface and output formatting  
- Pattern recognition for common issues
- Zero-config setup and operation

**Actively Developing:**
- Sophistication of analysis algorithms
- Breadth of covered failure scenarios
- Integration capabilities
- Performance optimization
- Enhanced OpenTelemetry tracing with correlation analysis

**Future Innovation:**
- eBPF kernel monitoring
- Machine learning predictions
- Auto-healing capabilities

## Contributing

We welcome contributions that help achieve our mission:

- **Enhanced Analysis**: Help us identify and handle more failure patterns
- **Better Communication**: Improve how we translate technical states to human language  
- **Robustness**: Make the tool work reliably across different cluster configurations
- **Documentation**: Help others understand and effectively use the tool

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Positioning

**Is this production-ready?** For Kubernetes health analysis and debugging, absolutely. For advanced features like eBPF monitoring, we're building toward that.

**Does this replace monitoring tools?** No. This complements your monitoring stack by focusing on debugging workflows and incident response.

**Why another Kubernetes tool?** Because `kubectl describe` output is still cryptic to most people, and we believe debugging should be accessible to everyone.

**What's the Finnish forest connection?** Kubernetes clusters are complex ecosystems that need understanding and protection. Plus, memorable names help projects succeed (see: Kubernetes, Grafana, Apache Kafka).

## Similar Tools & Ecosystem

We build on the shoulders of giants and respect excellent work in this space:
- [kubectl describe](https://kubernetes.io/docs/reference/kubectl/kubectl-commands/#describe) - The foundation we're making more accessible
- [kubectx/kubens](https://github.com/ahmetb/kubectx) - Essential cluster navigation
- [k9s](https://github.com/derailed/k9s) - Powerful terminal UI for cluster management
- [kubectl-debug](https://github.com/aylei/kubectl-debug) - Advanced debugging capabilities

Tapio occupies the space between "basic kubectl commands" and "comprehensive monitoring platforms"—making debugging accessible without complex infrastructure.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

**Current Reality**: Genuinely useful for everyday Kubernetes debugging and incident response.

**Development Philosophy**: Ship useful software today while building toward transformative capabilities.

**Project Approach**: Focused team solving real problems with clear technical vision and practical execution.

Give it a try. If it saves you time during your next debugging session, we've delivered value.

[⭐ Star if useful](https://github.com/yairfalse/tapio) | [🐛 Report issues](https://github.com/yairfalse/tapio/issues) | [💬 Discuss ideas](https://github.com/yairfalse/tapio/discussions)
