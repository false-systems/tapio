# TAPIO-AHTI Data Flow Architecture

> **SUPERSEDED**: NATS references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS for all event transport. See `pkg/intelligence/polku.go` and `pkg/publisher/polku.go`.

## Overview

This document defines the data flow architecture between TAPIO (Edge Intelligence) and AHTI (Central Intelligence).

**Key Principle:** TAPIO is the **only watcher**. AHTI **only learns**.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        TAPIO (Edge - Per Node)                           │
│                                                                          │
│  ┌──────────────────────────┐    ┌──────────────────────────┐           │
│  │     eBPF Observers       │    │     K8s Observers        │           │
│  │     (kernel level)       │    │     (API level)          │           │
│  │                          │    │                          │           │
│  │  • network (TCP/UDP/DNS) │    │  • deployments           │           │
│  │  • container (OOM/exit)  │    │  • configmaps/secrets    │           │
│  │  • node (PMC/cgroup)     │    │  • scaling events        │           │
│  │                          │    │  • pod lifecycle         │           │
│  └────────────┬─────────────┘    └────────────┬─────────────┘           │
│               │                               │                          │
│               ▼                               ▼                          │
│        ┌──────────────┐                ┌──────────────┐                 │
│        │   FILTER     │                │   SEND ALL   │                 │
│        │   (~1%)      │                │   (rare)     │                 │
│        │              │                │              │                 │
│        │  Anomalies   │                │  Causal      │                 │
│        │  only        │                │  events      │                 │
│        └──────┬───────┘                └──────┬───────┘                 │
│               │                               │                          │
│               └───────────────┬───────────────┘                          │
│                               │                                          │
│                               ▼                                          │
│                     ┌──────────────────┐                                │
│                     │   NATS Publish   │                                │
│                     │                  │                                │
│                     │ tapio.events.*   │                                │
│                     └────────┬─────────┘                                │
└──────────────────────────────┼──────────────────────────────────────────┘
                               │
                         NATS Cluster
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                        AHTI (Central - Single)                           │
│                                                                          │
│                    ┌──────────────────────────┐                         │
│                    │     NATS Subscribe       │                         │
│                    │     tapio.events.*       │                         │
│                    └────────────┬─────────────┘                         │
│                                 │                                        │
│                                 ▼                                        │
│                    ┌──────────────────────────┐                         │
│                    │    Event Ingestion       │                         │
│                    │                          │                         │
│                    │  • Parse events          │                         │
│                    │  • Validate schema       │                         │
│                    │  • Store in graph        │                         │
│                    └────────────┬─────────────┘                         │
│                                 │                                        │
│                                 ▼                                        │
│                    ┌──────────────────────────┐                         │
│                    │    Causality Graph       │                         │
│                    │                          │                         │
│                    │  deployment ──┐          │                         │
│                    │               ▼          │                         │
│                    │             pod ──┐      │                         │
│                    │                   ▼      │                         │
│                    │              container   │                         │
│                    │                   │      │                         │
│                    │                   ▼      │                         │
│                    │                 OOM      │                         │
│                    └────────────┬─────────────┘                         │
│                                 │                                        │
│                                 ▼                                        │
│                    ┌──────────────────────────┐                         │
│                    │    Pattern Learning      │                         │
│                    │                          │                         │
│                    │  • Temporal correlation  │                         │
│                    │  • Anomaly patterns      │                         │
│                    │  • Root cause detection  │                         │
│                    └────────────┬─────────────┘                         │
│                                 │                                        │
│                                 ▼                                        │
│                    ┌──────────────────────────┐                         │
│                    │    Insights / Alerts     │                         │
│                    │                          │                         │
│                    │  "Deployment X caused    │                         │
│                    │   OOM in pod Y due to    │                         │
│                    │   memory limit decrease" │                         │
│                    └──────────────────────────┘                         │
│                                                                          │
│                      AHTI NEVER WATCHES ANYTHING                         │
│                      AHTI ONLY RECEIVES AND LEARNS                       │
└──────────────────────────────────────────────────────────────────────────┘
```

## What TAPIO Sends

### From eBPF Observers (~1% of kernel events)

Only anomalies are sent. Normal events are filtered at the edge.

| Observer | Sent Events | Filtered Out |
|----------|-------------|--------------|
| Network | Connection refused, RTT spikes >2x baseline, retransmit rate >5%, DNS failures | Every TCP SYN/ACK/FIN, normal connections |
| Container | OOM kills, error exits (code ≠ 0), crash signals | Normal exits (code 0), SIGTERM shutdowns |
| Node | Memory pressure >80%, CPU throttling, PMC anomalies | Normal resource usage within limits |

**Why filter at edge?**
- Reduces NATS traffic by 99%
- AHTI doesn't need noise to learn patterns
- Context is embedded in the event (baseline, trend, evidence)

### From K8s Observers (100% - but they're rare)

All K8s lifecycle events are sent because they're causal anchors.

| Observer | Sent Events | Why 100%? |
|----------|-------------|-----------|
| Deployments | All creates, updates, deletes | Causal: "deploy → OOM" |
| ConfigMaps | All changes | Causal: "config change → crash" |
| Scaling | HPA/VPA events, replica changes | Causal: "scale down → pressure" |
| Pod lifecycle | Creates, scheduled, running, terminated | Causal: correlate with eBPF events |

**Why send all?**
- K8s events are infrequent (not 99% of traffic)
- They're causal anchors for root cause analysis
- Without them, AHTI can't answer "what changed before the failure?"

## Event Format

All events follow the `domain.ObserverEvent` schema:

```go
type ObserverEvent struct {
    ID        string    `json:"id"`
    Type      string    `json:"type"`      // "container", "network", "deployment"
    Subtype   string    `json:"subtype"`   // "oom_kill", "connection_refused"
    Timestamp time.Time `json:"timestamp"`

    // Classification
    Severity  Severity  `json:"severity"`  // critical, error, warning, info
    Outcome   Outcome   `json:"outcome"`   // success, failure

    // Context (pre-enriched by TAPIO)
    ContainerData *ContainerEventData `json:"container_data,omitempty"`
    NetworkData   *NetworkEventData   `json:"network_data,omitempty"`
    K8sData       *K8sEventData       `json:"k8s_data,omitempty"`
}
```

## NATS Topics

```
tapio.events.container.oom_kill     # OOM kills
tapio.events.container.exit_error   # Error exits
tapio.events.network.connection     # Connection failures
tapio.events.network.rtt_spike      # Latency degradation
tapio.events.k8s.deployment         # All deployment changes
tapio.events.k8s.configmap          # All config changes
tapio.events.k8s.scaling            # All scaling events
```

## Why This Architecture

### 1. Clear Separation of Concerns

| Component | Role | Watches? | Learns? |
|-----------|------|----------|---------|
| TAPIO | Edge Intelligence | ✅ Yes | ❌ No |
| AHTI | Central Intelligence | ❌ No | ✅ Yes |

### 2. Efficient Data Flow

- **eBPF events**: High volume (millions/sec) → Filter to 1% at edge
- **K8s events**: Low volume (hundreds/day) → Send 100%
- **NATS bandwidth**: Minimal (only what matters)

### 3. Causality Preserved

AHTI can build causal chains:
```
T=0:00  deployment/nginx updated (image: v1.2 → v1.3)
T=0:05  pod/nginx-abc created
T=0:10  container/nginx started
T=0:15  container/nginx OOM killed (memory: 512Mi limit, 510Mi used)
        └── Root cause: v1.3 has memory leak, limit too low
```

### 4. AHTI Stays Simple

AHTI's job is pattern recognition, not data collection:
- Receives pre-filtered, pre-enriched events
- Builds temporal causality graph
- Learns failure patterns
- Generates insights

## Implementation Notes

### TAPIO Side

```go
// eBPF observer - filtered
func (c *ContainerObserver) processEvent(ctx context.Context, evt ContainerEventBPF) {
    // Only send anomalies
    if evt.Type == EventTypeOOMKill || classification.Category == ExitCategoryError {
        c.nats.Publish("tapio.events.container."+subtype, domainEvent)
    }
    // Normal exits are NOT sent
}

// K8s observer - send all
func (d *DeploymentObserver) OnUpdate(old, new *appsv1.Deployment) {
    // ALL deployment changes are causal
    d.nats.Publish("tapio.events.k8s.deployment", domainEvent)
}
```

### AHTI Side

```go
// AHTI only subscribes, never watches
func (a *AHTI) Start() {
    a.nats.Subscribe("tapio.events.>", func(msg *nats.Msg) {
        event := parseEvent(msg.Data)
        a.graph.Ingest(event)       // Build causality graph
        a.patterns.Learn(event)      // Learn patterns
        a.insights.Evaluate(event)   // Generate insights
    })
}
```

## Summary

| Question | Answer |
|----------|--------|
| Who watches? | TAPIO only |
| Who learns? | AHTI only |
| What % of eBPF events sent? | ~1% (anomalies) |
| What % of K8s events sent? | 100% (causal anchors) |
| How do they communicate? | NATS |
| Why filter at edge? | Reduce noise, preserve bandwidth |
| Why send all K8s events? | They're rare and causally important |
