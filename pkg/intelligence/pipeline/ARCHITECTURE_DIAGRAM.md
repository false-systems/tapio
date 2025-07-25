# Tapio Architecture Visual Diagram

## Current Architecture (AS-IS)

```mermaid
graph TB
    subgraph "L0: Domain Layer"
        Event[domain.Event - DEPRECATED]
        UnifiedEvent[domain.UnifiedEvent]
        Interfaces[Core Interfaces]
    end

    subgraph "L1: Collectors Layer"
        EBPF[eBPF Collector]
        K8S[K8s Collector]
        SYSTEMD[SystemD Collector]
        CNI[CNI Collector]
    end

    subgraph "L2: Intelligence Layer"
        Pipeline[IntelligencePipeline]
        Correlation[Correlation Engine]
        Context[Context Builder]
        Analytics[Analytics Engine]
    end

    subgraph "L3: Integration Layer"
        CollectorMgr[CollectorManager]
        DataFlow[DataFlow - MISSING!]
        Orchestrator1[collector/Orchestrator]
        Orchestrator2[server/Orchestrator]
        OTEL[OTEL Integration]
    end

    subgraph "L4: Interface Layer"
        GRPCServer[gRPC Server]
        EventService[EventService]
        CollectorService[CollectorService]
        TapioService[TapioService]
    end

    %% Collector outputs
    EBPF -->|UnifiedEvent| CollectorMgr
    K8S -->|UnifiedEvent| CollectorMgr
    SYSTEMD -->|UnifiedEvent| CollectorMgr
    CNI -->|UnifiedEvent| CollectorMgr

    %% Integration flow (broken)
    CollectorMgr -.->|Should connect to| DataFlow
    DataFlow -.->|Missing link| Pipeline
    
    %% Current workarounds
    CollectorMgr -->|Direct| Pipeline
    Orchestrator1 -.->|References missing| DataFlow
    Orchestrator2 -.->|Also broken| DataFlow

    %% Intelligence processing
    Pipeline --> Correlation
    Pipeline --> Context
    Pipeline --> Analytics

    %% Server layer
    Pipeline --> GRPCServer
    GRPCServer --> EventService
    GRPCServer --> CollectorService
    GRPCServer --> TapioService

    %% Type confusion
    Event -.->|132 uses| EBPF
    UnifiedEvent -->|669 uses| EBPF

    classDef deprecated fill:#ff6666
    classDef missing fill:#ffcc66
    classDef working fill:#66ff66
    
    class Event deprecated
    class DataFlow missing
    class UnifiedEvent,Pipeline,CollectorMgr working
```

## Proposed Architecture (TO-BE)

```mermaid
graph TB
    subgraph "L0: Domain Layer"
        UnifiedEvent[domain.UnifiedEvent]
        CoreInterfaces[Core Interfaces]
        Types[Shared Types]
    end

    subgraph "L1: Collectors Layer"
        subgraph "Unified Collector Interface"
            EBPF[eBPF Collector]
            K8S[K8s Collector]
            SYSTEMD[SystemD Collector]
            CNI[CNI Collector]
        end
    end

    subgraph "L2: Intelligence Layer"
        subgraph "Processing Pipeline"
            Validation[Validation Stage]
            ContextBuild[Context Builder]
            Correlation[Correlation Engine]
            Analytics[Analytics Engine]
        end
        Pipeline[IntelligencePipeline]
    end

    subgraph "L3: Integration Layer"
        PipelineOrch[Pipeline Orchestrator]
        OTEL[OTEL Integration]
        Persistence[Persistence/WAL]
        Resilience[Resilience Layer]
    end

    subgraph "L4: Interface Layer"
        subgraph "API Gateway"
            GRPCServer[gRPC Server]
            REST[REST Gateway]
        end
        subgraph "Services"
            EventAPI[Event API]
            CorrelationAPI[Correlation API]
            MetricsAPI[Metrics API]
        end
    end

    %% Clean data flow
    EBPF -->|UnifiedEvent| PipelineOrch
    K8S -->|UnifiedEvent| PipelineOrch
    SYSTEMD -->|UnifiedEvent| PipelineOrch
    CNI -->|UnifiedEvent| PipelineOrch

    %% Orchestration
    PipelineOrch --> Pipeline
    PipelineOrch --> OTEL
    PipelineOrch --> Persistence

    %% Pipeline stages
    Pipeline --> Validation
    Validation --> ContextBuild
    ContextBuild --> Correlation
    Correlation --> Analytics

    %% Results flow
    Pipeline --> GRPCServer
    GRPCServer --> EventAPI
    GRPCServer --> CorrelationAPI
    GRPCServer --> MetricsAPI

    %% Persistence
    Analytics --> Persistence
    Persistence --> PipelineOrch

    classDef primary fill:#66ff66
    classDef secondary fill:#66ccff
    
    class UnifiedEvent,Pipeline,PipelineOrch primary
    class OTEL,Persistence,GRPCServer secondary
```

## Key Improvements

### 1. Single Event Type
- Remove `domain.Event` completely
- All components use `domain.UnifiedEvent`
- No type conversions needed

### 2. Single Orchestration Point
- One `PipelineOrchestrator` in L3
- Manages collector lifecycle
- Handles pipeline integration
- Controls persistence

### 3. Clear Data Flow
- Collectors → Orchestrator → Pipeline → API
- No missing components
- No circular dependencies

### 4. Proper Layering
- L0: Pure domain types
- L1: Data collection
- L2: Intelligence/processing
- L3: Integration/orchestration
- L4: External interfaces

### 5. Added Components
- Persistence integration
- Resilience patterns
- REST gateway
- Proper metrics API

## Migration Path

```mermaid
graph LR
    A[Current State] --> B[Fix DataFlow]
    B --> C[Migrate to UnifiedEvent]
    C --> D[Consolidate Orchestrators]
    D --> E[Add Persistence]
    E --> F[Complete Integration]
    F --> G[Target State]

    style A fill:#ff9999
    style G fill:#99ff99
```

## Component Relationships

```mermaid
graph TD
    subgraph "Clear Ownership"
        A[CollectorManager owns Collectors]
        B[Pipeline owns Processing Stages]
        C[Orchestrator owns Integration]
        D[Server owns APIs]
    end

    A --> C
    B --> C
    C --> D

    style A fill:#ffcc99
    style B fill:#99ccff
    style C fill:#cc99ff
    style D fill:#99ffcc
```