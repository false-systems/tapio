# Orchestrator Consolidation Plan

## Current State Analysis

We have 4 orchestrator implementations that need to be consolidated:

### 1. Collector Orchestrator (`/pkg/integrations/collector/orchestrator.go`)
**Status**: ✅ Already migrated to use UnifiedOrchestrator
- Previously managed collectors and dataflow
- Now uses UnifiedOrchestrator for pipeline integration
- Keeps OTEL integration and collector initialization

### 2. Server Orchestrator (`/pkg/integrations/server/orchestrator.go`)
**Purpose**: Manages gRPC server and HTTP gateway
**Key Features**:
- Dual protocol support (gRPC + HTTP)
- Service registration (TapioService, CollectorService, EventService)
- Health checks and reflection
- CORS configuration
- Graceful shutdown

**Action Required**:
- Keep as-is for now (server management is different concern)
- Consider renaming to `ServerManager` to avoid confusion
- Could integrate with UnifiedOrchestrator for event processing

### 3. Pipeline Orchestrator (`/pkg/intelligence/pipeline/orchestrator.go`)
**Purpose**: High-performance event processing
**Key Features**:
- Worker pool pattern
- Stage-based processing
- Batch processing
- Performance metrics

**Action Required**:
- Deprecate in favor of UnifiedOrchestrator
- Move any unique features to UnifiedOrchestrator
- Update all references

### 4. Collector Manager (`/pkg/integrations/collector-manager/manager.go`)
**Purpose**: Simple collector aggregation
**Status**: ✅ Already integrated
- UnifiedOrchestrator uses this for collector management
- Provides the simple, clean base we built upon

## Consolidation Steps

### Phase 1: Deprecate Pipeline Orchestrator (1-2 days)

1. **Mark as Deprecated**
```go
// Deprecated: Use UnifiedOrchestrator instead
// This will be removed in v3.0.0
type Orchestrator struct {
    // ...
}
```

2. **Update References**
Find and update all imports:
```bash
grep -r "pipeline.Orchestrator" --include="*.go" .
```

3. **Migrate Unique Features**
- The worker pool pattern is already in UnifiedOrchestrator
- Stage-based processing exists via IntelligencePipeline
- Metrics are implemented in UnifiedOrchestrator

### Phase 2: Refactor Server Orchestrator (2-3 days)

1. **Rename to Avoid Confusion**
```go
// ServerManager manages gRPC and HTTP gateway servers
type ServerManager struct {
    // ... existing fields
    eventProcessor *pipeline.UnifiedOrchestrator // Add this
}
```

2. **Integrate with UnifiedOrchestrator**
```go
func (sm *ServerManager) Start(ctx context.Context, orchestrator *pipeline.UnifiedOrchestrator) error {
    sm.eventProcessor = orchestrator
    
    // Use orchestrator's processed events for streaming
    go sm.streamProcessedEvents(orchestrator.ProcessedEvents())
    
    // ... rest of server startup
}
```

3. **Update Service Implementations**
- EventService uses orchestrator.ProcessedEvents()
- CollectorService uses orchestrator.GetHealth()
- TapioService uses orchestrator.GetMetrics()

### Phase 3: Clean Architecture (1 day)

1. **Final Structure**
```
pkg/
├── intelligence/
│   └── pipeline/
│       ├── unified_orchestrator.go    # Core orchestration
│       ├── pipeline.go                # Processing pipeline
│       └── stages.go                  # Processing stages
├── integrations/
│   ├── collector/
│   │   └── manager.go                 # Collector lifecycle
│   └── server/
│       └── manager.go                 # Server lifecycle (renamed)
└── cmd/
    └── tapio/
        └── main.go                    # Wires everything together
```

2. **Clear Responsibilities**
- **UnifiedOrchestrator**: Event routing, processing, monitoring
- **CollectorManager**: Collector lifecycle and aggregation
- **ServerManager**: API server lifecycle and protocol handling
- **IntelligencePipeline**: Event processing logic

### Phase 4: Update Examples and Tests (2 days)

1. **Create Migration Examples**
```go
// Old way
orchestrator := pipeline.NewOrchestrator(config)
dataflow := dataflow.NewTapioDataFlow(dfConfig)

// New way
orchestrator := pipeline.NewUnifiedOrchestrator(config)
// No dataflow needed!
```

2. **Update Integration Tests**
- Test collector → orchestrator → pipeline flow
- Test health aggregation
- Test metrics collection
- Test graceful shutdown

## Implementation Order

1. **Week 1**: 
   - Deprecate pipeline.Orchestrator
   - Update all internal references
   - Create migration guide for users

2. **Week 2**:
   - Refactor server orchestrator to ServerManager
   - Integrate with UnifiedOrchestrator
   - Update service implementations

3. **Week 3**:
   - Update all examples
   - Comprehensive testing
   - Documentation updates

## Benefits After Consolidation

1. **Single Source of Truth**: One orchestrator for all event processing
2. **Reduced Complexity**: From 4 implementations to 1 + specialized managers
3. **Better Performance**: Unified buffer management and worker pools
4. **Easier Maintenance**: ~500 lines vs 1300+ lines
5. **Clear Architecture**: Each component has single responsibility

## Backwards Compatibility

To maintain compatibility during migration:

1. **Type Aliases** (temporary)
```go
// Deprecated: Use UnifiedOrchestrator
type Orchestrator = UnifiedOrchestrator
```

2. **Adapter Functions**
```go
// Deprecated: Use NewUnifiedOrchestrator
func NewOrchestrator(config *OrchestratorConfig) *Orchestrator {
    // Convert old config to new format
    newConfig := convertConfig(config)
    return NewUnifiedOrchestrator(newConfig)
}
```

3. **Migration Warnings**
```go
func init() {
    log.Println("WARNING: pipeline.Orchestrator is deprecated. Please migrate to UnifiedOrchestrator")
}
```

## Verification Checklist

- [ ] All builds pass
- [ ] All tests pass
- [ ] No performance regression
- [ ] Health monitoring works
- [ ] Metrics collection works
- [ ] Graceful shutdown works
- [ ] Examples updated
- [ ] Documentation updated
- [ ] Migration guide published