# Tapio - Observability Correlation Platform

Tapio is a correlation engine for observability data. It collects system events from multiple sources, identifies relationships between them, and stores correlations in Neo4j for graph-based analysis.

## What It Actually Does

**Core Mission: Kubernetes Observability Intelligence**
- Provides complete visibility into Kubernetes cluster behavior and dependencies
- Correlates events across all layers: kernel, container runtime, kubelet, API server
- Identifies cascading failures and resource exhaustion patterns
- Maps service dependencies and performance bottlenecks
- Delivers actionable insights for K8s troubleshooting and optimization

**Key Capabilities:**
- Collects kernel events via eBPF (process, network, file operations)
- Monitors systemd services and journals
- Tracks DNS queries and responses
- Observes Kubernetes API server events and etcd operations
- Monitors kubelet, container runtime (CRI), and networking (CNI)
- Correlates events to find patterns and dependencies
- Stores correlation results in Neo4j graph database
- Streams events through NATS for real-time processing

## Architecture Flow

```mermaid
flowchart TD
    subgraph Cluster["Kubernetes Cluster"]
        subgraph Level0["Level 0: Domain"]
            D[domain.UnifiedEvent]
        end
        
        subgraph Level1["Level 1: Collectors"]
            K[Kernel eBPF Collector]
            S[Systemd Collector] 
            DNS[DNS Collector]
            CNI[CNI Collector]
            CRI[CRI Collector]
            Kubelet[Kubelet Collector]
            Kubeapi[Kubeapi Collector]
            Etcd[Etcd Collector]
        end
        
        NATS[NATS Streaming]
        
        K -->|raw events| NATS
        S -->|raw events| NATS
        DNS -->|raw events| NATS
        CNI -->|raw events| NATS
        CRI -->|raw events| NATS
        Kubelet -->|raw events| NATS
        Kubeapi -->|raw events| NATS
        Etcd -->|raw events| NATS
        
        NATS -->|transform| D
        
        subgraph Level2["Level 2: Intelligence"]
            CE[Correlation Engine]
            TC[Temporal Correlator]
            SC[Sequence Correlator] 
            DC[Dependency Correlator]
            OC[Ownership Correlator]
            PC[Performance Correlator]
        end
        
        D --> CE
        CE --> TC
        CE --> SC
        CE --> DC
        CE --> OC
        CE --> PC
        
        subgraph Level3["Level 3: Integrations"]
            NEO[Neo4j Storage]
        end
        
        CE --> NEO
    end

## Architecture Rules

**5-Level Hierarchy (STRICTLY ENFORCED):**
```
Level 0: pkg/domain/          # Zero dependencies
Level 1: pkg/collectors/      # Domain only  
Level 2: pkg/intelligence/    # Domain + L1
Level 3: pkg/integrations/    # Domain + L1 + L2
Level 4: pkg/interfaces/      # All above
```

Components can ONLY import from lower levels. No exceptions.

## Implemented Components

### Collectors (All Available)
- **kernel**: eBPF programs for syscall monitoring (process exec, network, file ops)
- **systemd**: Journal reader for service events  
- **dns**: eBPF-based DNS query/response capture
- **kubeapi**: Kubernetes API server event monitoring
- **kubelet**: Node-level container lifecycle monitoring
- **cri**: Container runtime interface monitoring
- **cni**: Container network interface plugin tracking
- **etcd**: Kubernetes datastore operation monitoring

### Correlation Engine
Processes events and finds relationships:
- **Temporal**: Events occurring in time patterns and recurring behaviors
- **Sequence**: Event chains (A→B→C patterns) and causal relationships
- **Dependency**: Service/pod dependencies and infrastructure correlations (requires Neo4j)
- **Ownership**: Kubernetes ownership chain analysis (Deployment→ReplicaSet→Pod)
- **Performance**: Resource exhaustion cascades and bottleneck detection

### Storage
- **Neo4j**: Stores correlations as graph relationships
- **Memory**: In-memory correlation cache

## Building

```bash
# Prerequisites
# - Go 1.21+
# - Linux kernel 4.14+ (for eBPF)
# - clang/llvm (for eBPF compilation)

# Build everything
make build

# Format code (MANDATORY before commit)
make fmt

# Run tests
make test

# Generate eBPF programs
make bpf-generate
```

## Configuration

```yaml
# config/tapio.yaml
collectors:
  kernel:
    enabled: true
    buffer_size: 8192
  systemd:
    enabled: true
    unit_filter: ["*.service"]
  dns:
    enabled: true

correlation:
  engine:
    worker_count: 4
    event_buffer_size: 1000
  
integrations:
  neo4j:
    uri: "bolt://localhost:7687"
    username: "neo4j"
    password: "password"
  nats:
    url: "nats://localhost:4222"
    stream: "events"
```

## Running

```bash
# Start with default config
./bin/tapio

# With custom config
./bin/tapio -config config/tapio.yaml

# Collectors only mode
./bin/tapio -mode collectors

# Correlation only mode  
./bin/tapio -mode correlation
```

## Example Correlations It Can Find

### 1. Service Restart Cascade
When systemd restarts a service, the correlation engine can identify:
- Related pod terminations
- Dependent service impacts
- Configuration changes that triggered it

### 2. Memory Pressure Events
Kernel OOM killer events are correlated with:
- Process memory allocations
- Container memory limits
- Service degradation

### 3. DNS Resolution Failures
DNS failures are correlated with:
- Service connection errors
- Pod networking issues
- Network policy changes

## Development Standards

From `CLAUDE.md` - these are enforced:
- **80% test coverage minimum**
- **No stubs, no TODOs** - only working code
- **Must compile**: `go build ./...` must pass
- **Must format**: `make fmt` before any commit
- **No `map[string]interface{}`** in public APIs
- **Follow 5-level architecture** - no exceptions

## Current Limitations

- Graph correlations require Neo4j to be running
- eBPF collectors require root/CAP_BPF privileges  
- Only works on Linux (eBPF dependency)
- Some collectors may need additional configuration for specific environments

## Project Status

This is an active correlation engine with working eBPF collectors and basic correlation capabilities. The architecture is solid and enforced. More collectors and correlators can be added following the established patterns.

## License

Apache 2.0
