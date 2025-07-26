# Tapio Architecture: Cross-Layer Observability with Semantic Correlation

## Table of Contents
1. [Vision & Problem Statement](#vision--problem-statement)
2. [Core Innovation: UnifiedEvent](#core-innovation-unifiedevent)
3. [Architecture Overview](#architecture-overview)
4. [Key Components](#key-components)
5. [Data Flow](#data-flow)
6. [Deployment Models](#deployment-models)
7. [Implementation Status](#implementation-status)
8. [Performance & Scale](#performance--scale)

## Vision & Problem Statement

### The Problem
Modern distributed systems generate observability data across multiple layers:
- **Kernel Level**: eBPF events, syscalls, memory operations
- **Network Level**: TCP/UDP packets, HTTP/gRPC requests, latencies
- **Container Level**: Kubernetes events, pod lifecycle, resource usage
- **Application Level**: Logs, errors, business metrics
- **Trace Level**: OpenTelemetry spans, distributed traces

**Current tools treat these as separate domains**, making it nearly impossible to correlate a user-facing error with its root cause in the kernel.

### The Vision
**Tapio provides unified observability that correlates events across ALL layers using OpenTelemetry trace context and semantic understanding.**

Example correlation chain:
```
User sees: "Payment failed" (HTTP 500)
    ↓ (trace_id: abc123)
Database timeout (30s latency)
    ↓ (trace_id: abc123)
Pod OOMKilled event
    ↓ (trace_id: abc123)
Memory allocation failures (eBPF)
    ↓ (trace_id: abc123)
Root cause: Memory leak in payment service
```

## Core Innovation: UnifiedEvent

### The Magic: One Event Format for Everything

```go
type UnifiedEvent struct {
    // Core Identity
    ID        string    // Auto-generated unique ID
    Timestamp time.Time // Event occurrence time
    Type      EventType // System, Network, Process, Memory, etc.
    Source    string    // Which collector generated this
    
    // OTEL Trace Context - THE KEY TO CORRELATION
    TraceContext *TraceContext {
        TraceID      string            // Links all related events
        SpanID       string
        ParentSpanID string
        TraceState   string
        Baggage      map[string]string
        Sampled      bool
    }
    
    // Semantic Understanding - WHAT IT MEANS
    Semantic *SemanticContext {
        Intent     string   // "payment-processing", "cache-miss", "oom-kill"
        Category   string   // "security", "performance", "availability"
        Tags       []string // Additional categorization
        Narrative  string   // Human-readable description
        Confidence float64  // How sure we are about the semantic meaning
    }
    
    // Entity Context - WHAT IT'S ABOUT
    Entity *EntityContext {
        Type       string            // "pod", "service", "node", "container"
        Name       string
        Namespace  string
        UID        string
        Labels     map[string]string
        Attributes map[string]string
    }
    
    // Layer-Specific Data - ONLY WHAT'S RELEVANT
    Kernel      *KernelData      // eBPF: syscalls, PIDs, stack traces
    Network     *NetworkData     // Protocol, IPs, ports, latencies
    Application *ApplicationData // Logs, errors, stack traces
    Kubernetes  *KubernetesData  // Events, pod status, resources
    Metrics     *MetricsData     // Time-series data
    
    // Business Impact - WHY IT MATTERS
    Impact *ImpactContext {
        Severity         string   // critical, high, medium, low
        BusinessImpact   float64  // 0.0-1.0
        AffectedServices []string
        AffectedUsers    int
        SLOImpact        bool
        CustomerFacing   bool
        RevenueImpacting bool
    }
    
    // Correlation Support
    Correlation *CorrelationContext {
        CorrelationID string   // Groups related events
        GroupID       string   // Semantic group
        ParentEventID string   // Causal relationship
        CausalChain   []string // Event IDs in causal order
        RelatedEvents []string
        Pattern       string   // Detected pattern name
        Stage         string   // Which stage in a sequence
    }
    
    // Original raw data for debugging
    RawData []byte
}
```

### UnifiedEvent Builder Pattern

Events are constructed using a fluent builder pattern:

```go
event := domain.NewUnifiedEvent().
    WithSource("ebpf").
    WithType(domain.EventTypeSystem).
    WithTraceContext(traceID, spanID).
    WithSemantic("oom-kill", "availability", "critical").
    WithEntity("pod", "payment-service", "production").
    WithKernelData("oom_kill", pid).
    WithImpact("critical", 0.95).
    Build()
```

## Architecture Overview

### Current Implementation

```
┌─────────────────────────────────────────────────────────────────┐
│                     Active Collectors                             │
├─────────────────────┬────────────────────┬──────────────────────┤
│   eBPF Collector    │  K8s Collector*    │ Systemd Collector*   │
│   (Integrated)      │  (Standalone)      │  (Standalone)        │
└──────────┬──────────┴─────────┬──────────┴───────┬──────────────┘
           │                    │                  │
           │         ┌──────────┴──────────┐       │
           │         │   CNI Collector*    │       │
           │         │   (Standalone)      │       │
           │         └──────────┬──────────┘       │
           │                    │                  │
           └────────────────────┴──────────────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │   UnifiedEvent      │
                    │   Conversion Layer  │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │  TapioDataFlow      │
                    │  (Event Enrichment) │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Analytics Engine   │
                    │  165k events/sec    │
                    └──────────┬──────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│  Semantic       │ │  Real-Time      │ │     Impact      │
│  Correlation    │ │  Analysis       │ │   Assessment    │
└────────┬────────┘ └────────┬────────┘ └────────┬────────┘
         │                    │                    │
         └────────────────────┴────────────────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │   ServerBridge      │
                    │ (gRPC Forwarding)   │
                    └──────────┬──────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │  TapioService       │
                    │  (gRPC Server)      │
                    └──────────┬──────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │   Event Storage     │
                    │   & Query API       │
                    └─────────────────────┘

* = Currently runs as standalone binary with gRPC client
```

## Key Components

### 1. Collectors (`pkg/collectors/`)

#### Active Collectors:

**eBPF Collector** (`pkg/collectors/ebpf/`) - **Dual Layer Architecture**
- **Dual-path processing** preserves both raw kernel data and semantic events
- **Raw Event Path** - Detailed kernel-level data for specialized tools:
  - Process context (PID, TID, UID, GID, Comm)
  - CPU core information and kernel timestamps
  - Network packet details and L7 protocol analysis
  - File operations and memory allocation patterns
  - Raw event storage with configurable retention
- **Semantic Event Path** - UnifiedEvent integration for correlation:
  - Automatic conversion to domain.UnifiedEvent
  - Intelligence pipeline integration
  - OTEL trace context propagation
  - Business impact assessment
- **Production Features**:
  - Rate limiting, circuit breaker, backpressure control
  - Configurable dual-path enablement
  - Memory and CPU monitoring
  - Security event detection
  - External tooling integration via gRPC

**K8s Collector** (`pkg/collectors/k8s/`)
- Kubernetes API events and state changes
- Runs as standalone binary
- Connects to Tapio server via gRPC
- Monitors:
  - Pod lifecycle events
  - Service changes
  - ConfigMap/Secret updates
  - Node status

**Systemd Collector** (`pkg/collectors/systemd/`)
- Journal logs and service events
- Runs as standalone binary
- Connects to Tapio server via gRPC
- Captures:
  - Service start/stop/restart
  - System errors and warnings
  - Boot sequence events

**CNI Collector** (`pkg/collectors/cni/`)
- Container Network Interface events
- Runs as standalone binary
- Connects to Tapio server via gRPC
- Monitors:
  - Network namespace creation/deletion
  - Interface attachment/detachment
  - Network policy changes

### 2. Event Processing Pipeline

#### TapioDataFlow (`pkg/dataflow/`)
- Event enrichment and routing
- OTEL semantic correlation
- Configurable processing modes
- Features:
  - Semantic grouping
  - Trace context propagation
  - Event batching
  - Flow control

#### Analytics Engine (`pkg/intelligence/analytics/engine/`)
- High-throughput event processing (165k+ events/sec)
- Real-time analytics pipeline:
  1. **Validation**: Event integrity checks
  2. **Enrichment**: Context and defaults
  3. **Correlation**: Group related events
  4. **Scoring**: Confidence and impact assessment
- Features:
  - Batch processing
  - Parallel workers
  - Memory pooling
  - Zero-copy optimizations

### 3. Intelligence Layer (`pkg/intelligence/`)

#### Correlation Engine (`pkg/intelligence/correlation/`) - **Modular Architecture**
- **Modular file structure** - Split from 3 massive files (3,855 lines) into 8 organized modules:
  - `semantic_core.go` - Core types and structures (SimpleSemanticGrouper, EventGroup, SemanticPattern)
  - `semantic_analysis.go` - Core analysis methods and temporal pattern analysis
  - `semantic_formatter.go` - Human-readable formatting and insight generation
  - `semantic_tracer.go` - Core OTEL tracer implementation with trace context propagation
  - `semantic_trace_groups.go` - Group management and unified event support
  - `semantic_engine_core.go` - Engine structure and lifecycle management
  - `semantic_engine_analysis.go` - Event processing and insight generation
  - `semantic_engine_converters.go` - Event conversion utilities between formats
  - `resilient_semantic_tracer.go` - Production-grade resilience with recovery strategies

#### Core Features:
- **Semantic event grouping** with multi-dimensional correlation (temporal, causal, spatial)
- **Pattern detection and matching** with confidence scoring
- **OTEL trace-based correlation** with full trace context propagation
- **Production resilience** with circuit breaker, rate limiting, and error recovery
- **Recovery strategies** for timeout, memory pressure, and correlation failures
- **Real-time correlation** with configurable time windows
- **Pattern library** with extensible semantic patterns
- **Finding generation** with business impact assessment

#### Performance Optimizations (`pkg/intelligence/performance/`)
- Ring buffers for high-throughput
- Per-CPU buffers to reduce contention
- Object pooling for memory efficiency
- Lock-free data structures

### 4. gRPC Services (`pkg/interfaces/server/grpc/`)

#### TapioServiceComplete
Main service implementation with:
- Bidirectional event streaming
- Event queries and subscriptions
- Correlation result streaming
- Collector management
- Health monitoring

#### Service Interfaces:
- **EventService**: Event storage and retrieval
- **CorrelationEngine**: Correlation processing
- **CollectorRegistry**: Collector lifecycle management
- **MetricsCollector**: Performance metrics

### 5. Main Binary (`cmd/tapio-collector/`)

#### Components:
- **CollectorManager**: Manages collector lifecycle
- **EBPFCollectorAdapter**: Integrates eBPF collector
- **ServerBridge**: Forwards to gRPC server
- **OTEL Integration**: Distributed tracing

#### Features:
- Cobra CLI framework
- Configurable correlation modes
- Health monitoring
- Graceful shutdown
- Signal handling

## Data Flow

### 1. Event Collection
```
eBPF Program detects OOM Kill
    ↓
eBPF Collector creates UnifiedEvent:
{
    ID: "ebpf_1234567890_1_0",
    Type: "Memory",
    Source: "ebpf",
    TraceContext: { TraceID: "abc123" },
    Semantic: {
        Intent: "oom-kill",
        Category: "availability",
        Tags: ["critical", "memory"]
    },
    Kernel: { 
        Syscall: "oom_kill",
        PID: 4567,
        Comm: "payment-service"
    },
    Impact: {
        Severity: "critical",
        BusinessImpact: 0.95
    }
}
```

### 2. Event Enrichment
```
TapioDataFlow enriches event:
- Adds OTEL context from active spans
- Groups with semantically similar events
- Calculates confidence scores
- Assesses business impact
```

### 3. Correlation Processing
```
Analytics Engine correlates events:
- Groups by TraceID
- Identifies patterns
- Builds causal chains
- Generates findings:

{
    FindingID: "finding-789",
    Pattern: "memory-exhaustion-cascade",
    Events: [/* related events */],
    RootCause: "Memory leak in payment service",
    Impact: {
        BusinessImpact: 0.95,
        AffectedServices: ["payment-api", "checkout"],
        CustomerFacing: true
    },
    Recommendations: [
        "Increase memory limits",
        "Fix memory leak in v1.2.3",
        "Enable memory monitoring"
    ]
}
```

## Deployment Models

### 1. Integrated Mode (Current Default)
```
tapio-collector binary includes:
- eBPF collector (integrated)
- Analytics engine
- Correlation processing
- gRPC server
```

### 2. Distributed Mode
```
Individual collector binaries:
- k8s-collector → Tapio Server
- systemd-collector → Tapio Server  
- cni-collector → Tapio Server

Each connects via gRPC client
```

### 3. Hybrid Mode (Recommended)
```
Main node:
- tapio-collector with eBPF

Other nodes:
- Lightweight collectors
- Connect to main node
```

## Implementation Status

### ✅ Completed
- UnifiedEvent structure and builder
- **Dual layer eBPF collector** with raw event preservation and semantic conversion
- Analytics engine (165k+ events/sec) 
- Semantic correlation engine with modular architecture (8 organized modules)
- Production-grade resilience (circuit breaker, rate limiting, recovery strategies)
- gRPC service implementation with bidirectional streaming
- OTEL trace context propagation
- Event storage interface with intelligent persistence
- Real-time streaming and correlation

### 🚧 In Progress
- K8s collector integration in main binary
- Systemd collector integration in main binary
- CNI collector integration in main binary
- ML-based pattern detection
- Advanced correlation patterns

### 🎉 Recently Completed
- **Intelligence Package Refactoring** - Modularized 3 massive files (3,855 lines) into 8 well-organized modules
- **Recovery Strategies** - Implemented production-grade error recovery for timeout, memory pressure, and correlation failures
- **Semantic Correlation** - Enhanced with OTEL trace context propagation and business impact assessment
- **Resilient Architecture** - Added circuit breaker, rate limiting, and health monitoring to correlation engine

### 📋 Planned
- Service mesh integration
- Cloud provider collectors
- Historical analysis engine
- Cost correlation
- Compliance reporting

## Performance & Scale

### Current Performance
- **Throughput**: 165,000+ events/second
- **Latency**: <10ms event processing
- **Memory**: ~512MB for 100k events/sec
- **CPU**: 4 cores for full throughput

### Optimization Techniques
1. **Zero-copy event passing**
2. **Ring buffers for collection**
3. **Batch processing**
4. **Object pooling**
5. **Lock-free data structures**
6. **Per-CPU buffers**

### Scalability
- Horizontal scaling via multiple collectors
- Partitioned event streams
- Distributed correlation
- Cloud-native deployment

## Success Metrics

1. **Performance**: 165k+ events/second sustained ✅
2. **Correlation Accuracy**: 95%+ correct root cause identification
3. **Time to Root Cause**: <30 seconds from symptom to cause
4. **Memory Efficiency**: <1GB for 100k events/sec ✅
5. **Integration**: All collectors producing UnifiedEvent (in progress)

## Conclusion

Tapio's architecture centers around the UnifiedEvent structure that enables:
1. **Unified correlation** across all observability layers
2. **Semantic understanding** of what events mean
3. **OTEL integration** for distributed tracing
4. **High performance** at 165k+ events/sec
5. **Flexible deployment** models

The system is production-ready for eBPF collection and correlation, with other collectors available as standalone binaries that integrate via gRPC.