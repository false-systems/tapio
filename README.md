# Tapio - Kubernetes Behavior Learning Through Graph-Based Observability

[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![eBPF](https://img.shields.io/badge/eBPF-enabled-orange.svg)](https://ebpf.io/)
[![Neo4j](https://img.shields.io/badge/Neo4j-graph-blue.svg)](https://neo4j.com/)

Tapio is a month-old observability platform that learns Kubernetes behavior patterns through eBPF-based event collection and graph correlation analysis. It captures low-level system events, enriches them with Kubernetes context, and stores correlations in Neo4j to identify patterns and root causes of issues.

## What Tapio Actually Does

Tapio observes your Kubernetes cluster at the kernel level using eBPF, then correlates events across different layers to understand behavior patterns. Instead of just showing you metrics, it builds a graph of relationships between events to answer "why did this happen?" rather than just "what happened?"

**Current Capabilities:**
- Collects kernel-level events via eBPF (syscalls, network, DNS)
- Monitors Kubernetes components (API server, kubelet, etcd)
- Enriches events with K8s context (pod, container, namespace)
- Publishes to NATS JetStream for async processing
- Converts events to correlation-optimized format (ObservationEvent)
- Batch loads to Neo4j for graph analysis
- Identifies temporal, causal, and dependency patterns

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        eBPF Collectors                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐          │
│  │ Kernel   │ │   DNS    │ │   CNI    │ │ Systemd  │          │
│  │ Events   │ │ Queries  │ │ Network  │ │ Services │          │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘          │
│       │            │            │            │                  │
│       └────────────┴────────────┴────────────┘                 │
│                           │                                     │
│                      RawEvent{}                                 │
└───────────────────────────┬─────────────────────────────────────┘
                            │
┌───────────────────────────┴─────────────────────────────────────┐
│                    Event Pipeline                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              K8s Context Enrichment                      │   │
│  │  (Container ID → Pod → Namespace → Service)             │   │
│  └─────────────────────────┬───────────────────────────────┘   │
│                            │                                     │
│  ┌─────────────────────────┴───────────────────────────────┐   │
│  │            NATS JetStream Publisher                      │   │
│  │  Subjects: observations.kernel, observations.dns, etc    │   │
│  └─────────────────────────┬───────────────────────────────┘   │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                     NATS JetStream
                            │
┌───────────────────────────┴─────────────────────────────────────┐
│                    Observation Loader                            │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │          RawEvent → ObservationEvent Parser              │   │
│  │   Extracts: PID, ContainerID, PodName, Namespace        │   │
│  └─────────────────────────┬───────────────────────────────┘   │
│                            │                                     │
│  ┌─────────────────────────┴───────────────────────────────┐   │
│  │              Batch Processor (1000 events)               │   │
│  └─────────────────────────┬───────────────────────────────┘   │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                      ObservationEvent{}
                            │
┌───────────────────────────┴─────────────────────────────────────┐
│                     Neo4j Graph Store                            │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │   Nodes: Events, Pods, Containers, Services              │   │
│  │   Edges: CAUSED_BY, RELATED_TO, DEPENDS_ON, OWNS        │   │
│  └───────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

## Key Technical Decisions

### ObservationEvent Structure
We recently migrated from a complex nested UnifiedEvent to a simpler, flatter ObservationEvent structure with correlation keys:

```go
type ObservationEvent struct {
    // Core identity
    ID        string
    Timestamp time.Time
    Source    string  // "kernel", "dns", "kubeapi"
    Type      string  // "syscall", "dns_query", "pod_created"
    
    // Correlation keys (pointers for optional fields)
    PID         *int32
    ContainerID *string
    PodName     *string
    Namespace   *string
    ServiceName *string
    
    // Simple string map instead of interface{}
    Data map[string]string
}
```

This design enables efficient correlation queries in Neo4j without complex nested structures.

### Technology Stack
- **eBPF**: Zero-overhead kernel observability (requires Linux 4.14+)
- **NATS JetStream**: Persistent, distributed event streaming
- **Neo4j**: Graph database for correlation storage and queries
- **Go 1.24**: With strict architecture enforcement

### Architecture Rules (Enforced)
```
Level 0: pkg/domain/       # Zero dependencies, pure types
Level 1: pkg/collectors/   # Domain only
Level 2: pkg/intelligence/ # Domain + collectors
Level 3: pkg/integrations/ # Domain + collectors + intelligence  
Level 4: pkg/interfaces/   # All layers (API, CLI)
```

Components can ONLY import from lower levels. This is enforced in CI.

## Getting Started

### Prerequisites
- Linux kernel 4.14+ with eBPF support
- Go 1.24+
- Docker (for Neo4j and NATS)
- clang/llvm-15+ (for eBPF compilation)
- Root access or CAP_BPF capability

### Quick Start

1. **Start Infrastructure**
```bash
# Start NATS JetStream
docker run -d --name nats \
  -p 4222:4222 -p 8222:8222 \
  nats:latest -js

# Start Neo4j
docker run -d --name neo4j \
  -p 7474:7474 -p 7687:7687 \
  -e NEO4J_AUTH=neo4j/password \
  neo4j:latest
```

2. **Build Tapio**
```bash
# Clone repository
git clone https://github.com/yairfalse/tapio
cd tapio

# Generate eBPF programs
make bpf-generate

# Build all components
make build
```

3. **Run Collectors**
```bash
# Start collectors (requires root for eBPF)
sudo ./bin/collectors \
  --nats-url nats://localhost:4222 \
  --enable kernel,dns,kubeapi
```

4. **Run Loader**
```bash
# In another terminal, start the loader
./bin/simple-loader \
  --nats-url nats://localhost:4222 \
  --neo4j-url bolt://localhost:7687 \
  --neo4j-user neo4j \
  --neo4j-password password
```

## Real Use Cases

### 1. Pod Crash Loop Root Cause Analysis
When a pod is crash-looping, Tapio can correlate:
- OOM killer events from kernel
- DNS resolution failures
- File access denials
- Network connection failures
- Container exit codes

Query in Neo4j:
```cypher
MATCH path = (crash:Event {type: 'container_died'})-[:CAUSED_BY*1..5]->(root:Event)
WHERE crash.pod_name = 'problematic-pod'
RETURN path
```

### 2. Service Latency Spike Investigation
Tapio correlates slow requests with:
- CPU throttling events
- Network retransmissions
- DNS lookup delays
- Disk I/O saturation

### 3. Security Incident Timeline
Track lateral movement by correlating:
- Process executions
- Network connections
- File access patterns
- Container escapes

## Development

### Project Structure
```
tapio/
├── cmd/
│   ├── collectors/      # Main collector binary
│   ├── simple-loader/   # NATS→Neo4j loader
│   └── api/            # REST API (future)
├── pkg/
│   ├── domain/         # Core types (ObservationEvent, RawEvent)
│   ├── collectors/     # eBPF and API collectors
│   │   ├── ebpf/      # Kernel, DNS, CNI collectors
│   │   ├── kubeapi/   # K8s API event collector
│   │   └── pipeline/  # Event enrichment & publishing
│   ├── intelligence/   # Correlation engine (in progress)
│   └── integrations/  # Storage and messaging
│       ├── loader/    # Neo4j batch loader
│       └── nats/      # NATS publisher
└── bpf/               # eBPF C programs
```

### Running Tests
```bash
# Unit tests with race detection
make test

# Specific package
go test -race ./pkg/collectors/...

# With coverage
go test -cover ./...
```

### Code Standards
From CLAUDE.md (strictly enforced):
- 80% minimum test coverage
- No TODOs, stubs, or empty functions
- No `interface{}` in public APIs
- Must run `make fmt` before commit
- Architecture violations = CI failure

### Building eBPF Programs
```bash
# Requires clang/llvm
make bpf-generate

# Output in pkg/collectors/ebpf/bpf/
```

## Current Limitations

- **Linux Only**: eBPF requires Linux kernel 4.14+
- **Root Required**: eBPF collectors need CAP_BPF or root
- **Neo4j Dependency**: Graph correlations require Neo4j running
- **Memory Usage**: High-volume clusters may need tuning
- **Learning Phase**: Needs time to build correlation patterns

## Roadmap

### In Progress
- [ ] Correlation engine completion
- [ ] Pattern detection algorithms
- [ ] REST API for queries

### Planned
- [ ] Helm chart for K8s deployment
- [ ] Grafana plugin for visualization
- [ ] ML-based anomaly detection
- [ ] Multi-cluster support
- [ ] Event replay capability
- [ ] Custom correlation rules DSL

### Future
- [ ] Windows container support (non-eBPF)
- [ ] Cloud provider integrations (AWS, GCP, Azure)
- [ ] Service mesh observability (Istio, Linkerd)
- [ ] GitOps integration for correlation rules

## Contributing

We welcome contributions! The architecture is clean and modular:

1. **Add a Collector**: Implement the `Collector` interface in `pkg/collectors/`
2. **Add a Correlator**: Implement correlation logic in `pkg/intelligence/`
3. **Enhance Pipeline**: Add enrichers in `pkg/collectors/pipeline/`

Requirements:
- Follow 5-level architecture (enforced)
- Minimum 80% test coverage
- Run `make fmt` and `make test`
- No stubs or TODOs

## Performance

Current benchmarks on a 4-core machine:
- Event ingestion: ~50,000 events/sec
- NATS publishing: ~30,000 events/sec
- Neo4j batch loading: ~10,000 events/sec
- Memory usage: ~500MB for collectors
- CPU usage: <5% idle, 20-30% under load

## License

MIT License - See [LICENSE](LICENSE) file

## Acknowledgments

Built with:
- [Cilium eBPF](https://github.com/cilium/ebpf) - Go eBPF library
- [NATS](https://nats.io/) - High-performance messaging
- [Neo4j](https://neo4j.com/) - Graph database
- [OpenTelemetry](https://opentelemetry.io/) - Observability framework

## Support

- Issues: [GitHub Issues](https://github.com/yairfalse/tapio/issues)
- Discussions: [GitHub Discussions](https://github.com/yairfalse/tapio/discussions)

---

**Note**: This is a month-old project focused on learning Kubernetes behavior patterns. The architecture is solid, but some components are still being refined. We value clean code and proper design over feature velocity.