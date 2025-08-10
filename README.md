# Tapio - Observability Correlation Platform

Tapio is a correlation engine for observability data. It collects system events from multiple sources, identifies relationships between them, and stores correlations in Neo4j for graph-based analysis.

## What It Actually Does

- Collects kernel events via eBPF (process, network, file operations)
- Monitors systemd services and journals
- Tracks DNS queries and responses
- Correlates events to find patterns and dependencies
- Stores correlation results in Neo4j graph database
- Streams events through NATS

## Architecture Flow

```mermaid
graph TD
    subgraph "Level 0: Domain"
        D[domain.UnifiedEvent]
    end
    
    subgraph "Level 1: Collectors"
        K[Kernel/eBPF Collector]
        S[Systemd Collector]
        DNS[DNS Collector]
        CNI[CNI Collector]
        CRI[CRI Collector]

        K --> D
        S --> D
        DNS --> D
        CNI --> D
    end
    
    subgraph "Level 2: Intelligence"
        CE[Correlation Engine]
        TC[Temporal Correlator]
        SC[Sequence Correlator]
        DC[Dependency Correlator]
        D --> CE
        CE --> TC
        CE --> SC
        CE --> DC
    end
    
    subgraph "Level 3: Integrations"
        NATS[NATS Publisher]
        NEO[Neo4j Storage]
        CE --> NATS
        CE --> NEO
    end
    
    subgraph "Data Flow"
        K -.->|syscalls| eBPF
        S -.->|journal| systemd
        DNS -.->|packets| network
        NEO -.->|queries| Graph
    end
```

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

### Collectors (What's Actually Working)
- **kernel**: eBPF programs for syscall monitoring (process exec, network, file ops)
- **systemd**: Journal reader for service events
- **dns**: eBPF-based DNS query/response capture

### Correlation Engine
Processes events and finds relationships:
- **Temporal**: Events occurring in time patterns
- **Sequence**: Event chains (A→B→C patterns)
- **Dependency**: Service/pod dependencies (requires Neo4j)

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

- Kubernetes collectors (kubeapi, kubelet, cri) are defined but not fully implemented
- Graph correlations require Neo4j to be running
- eBPF collectors require root/CAP_BPF privileges
- Only works on Linux (eBPF dependency)

## Project Status

This is an active correlation engine with working eBPF collectors and basic correlation capabilities. The architecture is solid and enforced. More collectors and correlators can be added following the established patterns.

## License

Apache 2.0
