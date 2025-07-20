# Tapio Architecture: Cross-Layer Observability with Semantic Correlation

## Table of Contents
1. [Vision & Problem Statement](#vision--problem-statement)
2. [Core Innovation: UnifiedEvent](#core-innovation-unifiedevent)
3. [Architecture Overview](#architecture-overview)
4. [Key Components](#key-components)
5. [Data Flow](#data-flow)
6. [Why This Approach](#why-this-approach)
7. [Implementation Roadmap](#implementation-roadmap)

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
    ID        string
    Timestamp time.Time
    Type      EventType
    Source    string  // Which collector generated this
    
    // OTEL Trace Context - THE KEY TO CORRELATION
    TraceContext *TraceContext {
        TraceID      string  // Links all related events
        SpanID       string
        ParentSpanID string
    }
    
    // Semantic Understanding - WHAT IT MEANS
    Semantic *SemanticContext {
        Intent   string  // "payment-processing", "cache-miss", "oom-kill"
        Category string  // "business-critical", "performance", "reliability"
        Tags     []string
    }
    
    // Layer-Specific Data - ONLY WHAT'S RELEVANT
    Kernel      *KernelData      // eBPF: syscalls, PIDs, stack traces
    Network     *NetworkData     // HTTP status, latency, IPs
    Application *ApplicationData // Logs, errors, stack traces
    Kubernetes  *KubernetesData  // Events, pod status, resources
    
    // Business Impact - WHY IT MATTERS
    Impact *ImpactContext {
        BusinessImpact   float64  // 0.0-1.0
        CustomerFacing   bool
        RevenueImpacting bool
        SLOImpact        bool
    }
}
```

### Why UnifiedEvent Works

1. **Flexible**: Only populate relevant fields (eBPF events don't need HTTP headers)
2. **Traceable**: OTEL context links events across all layers
3. **Semantic**: We understand what events mean, not just raw data
4. **Actionable**: Business impact drives prioritization

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Event Sources                              │
├───────────┬────────────┬─────────────┬────────────┬─────────────┤
│   eBPF    │    OTEL    │ Kubernetes  │  Network   │ Application │
│ Collector │ Collector  │ Collector   │ Collector  │ Collector   │
└─────┬─────┴──────┬─────┴──────┬──────┴─────┬──────┴──────┬──────┘
      │            │            │            │             │
      └────────────┴────────────┴────────────┴─────────────┘
                              │
                    ┌─────────┴──────────┐
                    │  UnifiedEvent      │
                    │  Conversion Layer  │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │  Analytics Engine  │
                    │  165k events/sec   │
                    └─────────┬──────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
┌───────┴────────┐ ┌──────────┴──────────┐ ┌───────┴────────┐
│  Correlation   │ │ Real-Time Analysis  │ │Impact Assessment│
│    Engine      │ │    & Scoring        │ │  & Prediction   │
└───────┬────────┘ └──────────┬──────────┘ └───────┬────────┘
        │                     │                     │
        └─────────────────────┼─────────────────────┘
                              │
                    ┌─────────┴──────────┐
                    │   gRPC/REST API    │
                    │  Streaming Results │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │   Tapio Clients    │
                    │  (GUI, CLI, SDK)   │
                    └────────────────────┘
```

## Key Components

### 1. Collectors (pkg/collectors/)
- **eBPF Collector**: Kernel-level events using eBPF programs
- **OTEL Collector**: OpenTelemetry traces, metrics, logs
- **K8s Collector**: Kubernetes API events and state changes
- **Network Collector**: Packet capture and protocol analysis
- **Prometheus Collector**: Metrics scraping (being replaced)

Each collector converts its native format to UnifiedEvent.

### 2. Analytics Engine (pkg/analytics/)
- **Throughput**: 165,000+ events/second
- **Pipeline Stages**:
  1. Validation: Ensure event integrity
  2. Enrichment: Add context and defaults
  3. Correlation: Group related events
  4. Analytics: Score and assess impact

### 3. Correlation Engine (pkg/intelligence/correlation/)
- **Semantic Grouping**: Groups events by meaning, not just time
- **Pattern Detection**: Identifies known failure patterns
- **Causal Analysis**: Builds causal chains of events
- **OTEL Integration**: Uses trace context for correlation

### 4. gRPC/REST API (pkg/interfaces/server/grpc/)
- **Bidirectional Streaming**: Real-time event flow
- **Five Core Services**:
  - TapioService: Main event streaming
  - CollectorService: Collector management
  - ObservabilityService: OTEL signals
  - EventService: Event queries and subscriptions
  - CorrelationService: Correlation results

### 5. Intelligence Layer (pkg/intelligence/)
- **Semantic Understanding**: What events mean
- **Impact Assessment**: Business impact scoring
- **Predictive Analytics**: Predict cascading failures
- **Root Cause Analysis**: Identify actual causes

## Data Flow

### 1. Event Collection
```
eBPF Program captures open() syscall failure
    ↓
eBPF Collector converts to UnifiedEvent:
{
    ID: "evt-123",
    Type: "kernel.syscall",
    TraceContext: { TraceID: "abc123" },
    Kernel: { 
        Syscall: "open",
        ReturnCode: -2,  // ENOENT
        PID: 4567
    },
    Semantic: {
        Intent: "file-access-failed",
        Category: "system"
    }
}
```

### 2. Correlation
```
Analytics Engine receives events with same TraceID:
- Kernel: open() failure
- Application: "Config file not found" error
- K8s: Container restart
- Network: Health check failures

Correlation Engine groups them:
{
    CorrelationID: "corr-789",
    Pattern: "missing-config-cascade",
    RootCause: "Config file missing",
    Impact: {
        BusinessImpact: 0.8,
        AffectedServices: ["payment-api", "checkout"],
        CustomerFacing: true
    }
}
```

### 3. Real-Time Analysis
```
Analytics Results Stream:
{
    EventID: "evt-123",
    CorrelationID: "corr-789",
    SemanticGroupID: "missing-config-pattern",
    ConfidenceScore: 0.95,
    ImpactAssessment: {
        BusinessImpact: 0.8,
        CascadeRisk: 0.7,
        RecommendedActions: [
            "Check ConfigMap mounting",
            "Verify file permissions",
            "Review recent deployments"
        ]
    }
}
```

## Why This Approach

### 1. Unified Correlation
- **Traditional**: Separate tools for metrics, logs, traces, eBPF
- **Tapio**: One correlation engine understanding all signals

### 2. Semantic Understanding
- **Traditional**: Alert on "CPU > 80%"
- **Tapio**: Understand "payment processing degraded due to resource exhaustion"

### 3. Root Cause, Not Symptoms
- **Traditional**: See symptoms across multiple dashboards
- **Tapio**: Trace from user impact to kernel-level root cause

### 4. Business Context
- **Traditional**: Technical metrics only
- **Tapio**: Business impact scoring and SLO awareness

### 5. Predictive Capabilities
- **Traditional**: React after failures
- **Tapio**: Predict cascading failures before they happen

## Implementation Roadmap

### Phase 1: Foundation ✅
- [x] UnifiedEvent structure
- [x] Analytics engine (165k events/sec)
- [x] gRPC/REST APIs
- [x] Basic collectors

### Phase 2: Intelligence (Current)
- [ ] Update correlation engine for UnifiedEvent
- [ ] Semantic pattern library
- [ ] ML-based anomaly detection
- [ ] Predictive analytics

### Phase 3: Advanced Collectors
- [ ] CNI plugin for network correlation
- [ ] Advanced eBPF programs
- [ ] Service mesh integration
- [ ] Cloud provider integrations

### Phase 4: Enterprise Features
- [ ] Multi-cluster support
- [ ] Historical analysis
- [ ] Compliance reporting
- [ ] Cost correlation

## Success Metrics

1. **Performance**: 165k+ events/second sustained
2. **Correlation Accuracy**: 95%+ correct root cause identification
3. **Time to Root Cause**: <30 seconds from symptom to cause
4. **Business Impact**: Reduce MTTR by 80%
5. **Developer Experience**: Single pane of glass for all observability

## Conclusion

Tapio revolutionizes observability by:
1. **Unifying all signals** into one correlated stream
2. **Understanding semantics** not just metrics
3. **Connecting user impact** to root causes
4. **Enabling proactive** response to issues

The UnifiedEvent structure and semantic correlation engine are the foundation that makes this possible.