# Tapio Correlation Engine - Design Document

## Overview

The Tapio Correlation Engine is a modular, extensible system that transforms raw Kubernetes events into actionable intelligence through a funnel architecture.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          TAPIO CORRELATION ENGINE                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Events (1000s)          Correlations (100s)         Answer (1)            │
│      ┃                         ┃                         ┃                 │
│      ▼                         ▼                         ▼                 │
│  ╔═══════════╗           ╔═══════════╗           ╔═══════════╗           │
│  ║           ║           ║           ║           ║           ║           │
│  ║  EVENTS   ║ ========> ║CORRELATORS║ ========> ║AGGREGATOR ║           │
│  ║           ║           ║           ║           ║           ║           │
│  ╚═══════════╝           ╚═══════════╝           ╚═══════════╝           │
│       │                        │                        │                 │
│       │                   ┌────┴────┐                   │                 │
│       │               ┌───┴───┐ ┌───┴───┐               │                 │
│       │               │Perf   │ │Service│               │                 │
│       │               │Corr   │ │Map    │               │                 │
│       │               └───────┘ └───────┘               │                 │
│       │               ┌───────┐ ┌───────┐               │                 │
│       │               │K8s    │ │Work   │               │                 │
│       │               │Corr   │ │load   │               │                 │
│       │               └───────┘ └───────┘               │                 │
│       │                                                 │                 │
│       └─────────────────────────────────────────────────┘                 │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## The Funnel Concept

```
Wide ══════════════════════════════════════════════════> Narrow

Stage 1: Raw Events (Thousands)
════════════════════════════════════════════════════════════════
CPU:85% | Memory:2GB | Pod:Crash | Error:502 | Latency:500ms
Network:Timeout | Disk:Full | Service:Down | Config:Changed

                    ║
                    ▼

Stage 2: Correlations (Hundreds)  
════════════════════════════════════════════════════════
[Memory Leak Detected] [Service Cascade] [Config Drift]
[CPU Throttling Pattern] [Network Congestion]

                    ║
                    ▼

Stage 3: Root Cause (One)
══════════════════════
"Frontend down due to backend memory leak 
caused by ConfigMap change at 14:32"
```

## Component Flow Diagram

```
┌─────────────┐     ┌─────────────────┐     ┌──────────────┐
│             │     │                 │     │              │
│   Event     │────>│ Correlation     │────>│  Aggregator  │
│             │     │ Engine          │     │              │
└─────────────┘     └─────────────────┘     └──────────────┘
                            │                        │
                            ▼                        ▼
                    ┌───────────────┐       ┌──────────────┐
                    │               │       │              │
                    │  Correlators  │       │Final Result  │
                    │               │       │              │
                    └───────────────┘       └──────────────┘
```

## Detailed Component Design

### 1. Standard Correlator Interface

```
┌─────────────────────────────────────────┐
│            CORRELATOR                   │
├─────────────────────────────────────────┤
│ + Name(): string                        │
│ + Version(): string                     │
│ + RequiredEventTypes(): []string        │
│ + RequiredContext(): []string           │
│ + Process(input): Output                │
│ + IsHealthy(): bool                     │
└─────────────────────────────────────────┘
                    ▲
                    │ implements
    ┌───────────────┼───────────────────┐
    │               │                   │
┌───────────┐ ┌───────────┐ ┌───────────┐
│Performance│ │ServiceMap │ │Kubernetes │
│Correlator │ │Correlator │ │Correlator │
└───────────┘ └───────────┘ └───────────┘
```

### 2. Data Flow Through Correlators

```
┌──────────────┐
│ Event Input  │
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────────────┐
│          CorrelatorInput                     │
├──────────────────────────────────────────────┤
│ - Event: UnifiedEvent                        │
│ - Context: map[string]interface{}            │
│ - RecentEvents: []UnifiedEvent               │
│ - GraphQuerier: Neo4j Interface              │
└──────────────┬───────────────────────────────┘
               │
               ▼
        ┌──────────────┐
        │  Correlator  │
        │   Process    │
        └──────┬───────┘
               │
               ▼
┌──────────────────────────────────────────────┐
│          CorrelatorOutput                    │
├──────────────────────────────────────────────┤
│ - CorrelatorName: string                     │
│ - Findings: []Finding                        │
│ - Context: map[string]interface{}            │
│ - Confidence: float64                        │
└──────────────────────────────────────────────┘
```

### 3. Aggregator Design

```
Multiple Inputs                    Single Output
─────────────────                 ──────────────

┌─────────────┐
│Performance  │────┐
│Output       │    │
└─────────────┘    │    ┌─────────────┐     ┌──────────────┐
                   ├───>│             │     │              │
┌─────────────┐    │    │ AGGREGATOR  │────>│Final Result  │
│ServiceMap   │────┤    │             │     │              │
│Output       │    │    └─────────────┘     └──────────────┘
└─────────────┘    │            │
                   │            ▼
┌─────────────┐    │    ┌─────────────┐
│Kubernetes   │────┘    │Aggregation  │
│Output       │         │Rules Engine │
└─────────────┘         └─────────────┘
```

### 4. Context Sharing via Neo4j

```
┌────────────────────────────────────────────────────┐
│                    Neo4j Graph                     │
├────────────────────────────────────────────────────┤
│                                                    │
│  ┌────────┐     FOUND_BY      ┌────────┐         │
│  │ Event  │─────────────────>  │Finding │         │
│  └────────┘                    └────────┘         │
│       │                             │              │
│       │ CAUSED                      │ INDICATES    │
│       ▼                             ▼              │
│  ┌────────┐                    ┌────────┐         │
│  │ Event  │                    │Pattern │         │
│  └────────┘                    └────────┘         │
│                                                    │
│  Correlators Query & Store Findings Here          │
└────────────────────────────────────────────────────┘
```

## Processing Pipeline

### Phase 1: Event Enrichment
```
Raw Event ──> Add Context ──> Add Recent Events ──> Ready for Processing
              (from Neo4j)     (last 5 min)
```

### Phase 2: Parallel Correlation
```
                 ┌─> Performance Correlator ─┐
                 │                           │
Enriched Event ──┼─> ServiceMap Correlator  ├──> Findings[]
                 │                           │
                 └─> Kubernetes Correlator  ─┘
```

### Phase 3: Aggregation
```
Findings[] ──> Apply Rules ──> Resolve Conflicts ──> Build Story ──> Final Result
```

### Phase 4: Storage & Learning
```
Final Result ──> Store in Neo4j ──> Update Patterns ──> Improve Future Correlations
```

## Example: Complete Flow

```
1. EVENT ARRIVES
   ┌─────────────────┐
   │ Pod OOMKilled   │
   │ namespace: prod │
   │ pod: backend-1  │
   └────────┬────────┘
            │
2. CORRELATORS ANALYZE
            ▼
   ┌─────────────────────────────────────┐
   │ Performance: "Memory leak detected"  │
   │ ServiceMap: "Frontend depends on it" │
   │ Kubernetes: "Part of backend deploy" │
   └────────┬────────────────────────────┘
            │
3. AGGREGATOR COMBINES
            ▼
   ┌─────────────────────────────────────┐
   │ Root Cause: Memory leak in backend   │
   │ Impact: Frontend returning 502s      │
   │ Fix: Increase memory limit or fix    │
   │      the leak in function processReq │
   └─────────────────────────────────────┘
```

## Extensibility

### Adding New Correlator

```
1. Implement Interface
   ┌─────────────────┐
   │ WorkloadCorr    │
   │ implements      │
   │ Correlator      │
   └─────────────────┘

2. Register with Engine
   engine.RegisterCorrelator(workloadCorr)

3. Automatic Integration
   - Receives same inputs
   - Outputs standard format
   - Aggregator handles it
```

## Benefits of This Design

1. **Modular**: Add/remove correlators without changing core
2. **Scalable**: Run correlators in parallel
3. **Extensible**: Easy to add new correlators
4. **Testable**: Each component has clear interface
5. **Maintainable**: Clear separation of concerns

## Next Steps

1. Implement base interfaces
2. Create first 3 correlators (Performance, ServiceMap, K8s)
3. Build aggregator with basic rules
4. Test with real events
5. Add more correlators based on needs