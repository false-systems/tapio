# NATS-Based Correlation Subscriber

## Overview

This is a correlation subscriber component that uses NATS as a message bus for event distribution. It's part of Tapio's broader goal to reduce alert fatigue through intelligent, contextual correlation of Kubernetes events.

## Philosophy: Less is More

Traditional monitoring systems overwhelm operators with hundreds of alerts. Tapio takes a different approach:

- **Collect everything**: Comprehensive K8s telemetry from kernel to application layer
- **Correlate intelligently**: Find the real relationships between events
- **Report contextually**: One meaningful story instead of 50 alerts

## Architecture

```
Collectors (K8s-focused)          Message Bus            Correlation
━━━━━━━━━━━━━━━━━━━━━           ━━━━━━━━━━━           ━━━━━━━━━━━━

┌─────────────────┐                                    
│   eBPF          │─────┐        ┌─────────────┐      ┌──────────────┐
│   (kernel)      │     │        │             │      │              │
└─────────────────┘     │        │    NATS     │      │ Correlation  │
                        ├───────▶│             │─────▶│   Engine     │
┌─────────────────┐     │        │ (temporary) │      │              │
│   K8s API       │─────┤        │             │      └──────────────┘
│   (events)      │     │        └─────────────┘              │
└─────────────────┘     │                                     ▼
                        │                              Single Context
┌─────────────────┐     │                              "Pod X failed
│   Network       │─────┘                               because..."
│   (services)    │
└─────────────────┘
```

## Core Concept: Trace-Based Correlation

Events sharing the same trace ID represent a single operational context:

```
TraceID: abc123
├── kernel: Memory allocation failed
├── k8s: Pod OOMKilled
├── k8s: Deployment rollback triggered
└── network: Service endpoint removed

Result: "Service disruption due to memory pressure"
(Not 4 separate alerts)
```

## Implementation Status

**Current State**: Work in Progress

- Basic NATS subscriber implementation
- Trace-based event grouping
- Integration with correlation engine (pending refactor)

**Not Implemented**:
- Production deployment
- Performance optimizations
- Advanced correlation patterns

## Configuration

```go
type NATSSubscriberConfig struct {
    URL               string
    StreamName        string
    TraceSubjects     []string        // Trace-based routing
    CorrelationWindow time.Duration   // Event collection window
    MinEventsForCorr  int            // Threshold for correlation
}
```

## Design Decisions

### Why NATS?
- Simple message bus for prototyping
- May be replaced with more K8s-native solutions
- Not critical to core correlation logic

### Why Trace-Based Grouping?
- Natural boundary for related events
- Enables distributed correlation
- Maintains context across components

## K8s-Centric Correlation

All correlation focuses on Kubernetes contexts:

1. **Pod Lifecycle**: Creation, scheduling, termination
2. **Service Dependencies**: Network calls, endpoint changes
3. **Resource Pressure**: CPU, memory, storage
4. **Configuration Changes**: ConfigMaps, Secrets, CRDs
5. **Security Events**: RBAC, admission webhooks, policy violations

## Future Direction

The specific implementation details (NATS, message formats, etc.) are less important than the core concept:

**Transform noise into narrative.**

Whether using NATS, Kafka, or direct integration, the goal remains:
- Collect comprehensive K8s telemetry
- Correlate events intelligently
- Deliver contextual insights

## Usage Example

```go
// Initialize
subscriber := NewNATSSubscriber(config, correlationEngine)
subscriber.Start(ctx)

// Process correlation results
for results := range subscriber.Results() {
    // One result instead of many alerts
    fmt.Printf("Root cause: %s\n", result.Summary)
}
```

## Technical Details

- Groups events by trace ID
- Configurable time windows
- Parallel processing support
- Backpressure handling

---

This component is part of Tapio's mission to make Kubernetes operations more intelligent and less noisy.