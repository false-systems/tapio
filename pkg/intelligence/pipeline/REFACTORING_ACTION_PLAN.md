# Tapio Refactoring Action Plan

## Immediate Fixes (Blocking Issues)

### 1. Fix Missing DataFlow References

**Problem**: Build failures due to missing `dataflow` package

**Files to fix**:
- `/pkg/integrations/collector/orchestrator.go` (lines 92-93, 138-139, 149, 164-165)
- `/pkg/integrations/server/orchestrator.go` (check for similar references)

**Solution**:
```go
// Replace this:
o.dataFlow = dataflow.NewTapioDataFlow(dataFlowConfig)

// With this:
pipelineConfig := &pipeline.PipelineConfig{
    Mode:               pipeline.PipelineModeHighPerformance,
    EnableCorrelation:  o.config.CorrelationMode == "semantic",
    EnableValidation:   true,
    EnableContext:      true,
    BufferSize:         o.config.BufferSize,
    BatchSize:          1000,
    ProcessingTimeout:  5 * time.Second,
}
o.pipeline = pipeline.NewPipeline(pipelineConfig)
```

### 2. Create Pipeline Adapter for Missing ServerBridge

**Create**: `/pkg/integrations/adapters/pipeline_adapter.go`

```go
package adapters

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)

// PipelineAdapter bridges collectors to intelligence pipeline
type PipelineAdapter struct {
    pipeline     pipeline.IntelligencePipeline
    inputEvents  chan *domain.UnifiedEvent
    outputEvents chan *domain.UnifiedEvent
}

func NewPipelineAdapter(p pipeline.IntelligencePipeline) *PipelineAdapter {
    return &PipelineAdapter{
        pipeline:     p,
        inputEvents:  make(chan *domain.UnifiedEvent, 10000),
        outputEvents: make(chan *domain.UnifiedEvent, 10000),
    }
}

func (pa *PipelineAdapter) Start(ctx context.Context) error {
    return pa.pipeline.Start(ctx)
}

func (pa *PipelineAdapter) ProcessEvents(ctx context.Context) {
    for {
        select {
        case event := <-pa.inputEvents:
            if err := pa.pipeline.ProcessEvent(event); err != nil {
                // Handle error
            }
        case <-ctx.Done():
            return
        }
    }
}
```

## Phase 1: Type Migration (1-2 days)

### Step 1: Update Collector Adapters

**Files to update**:
- `/pkg/integrations/collector-manager/adapters.go`
- `/pkg/integrations/collector/adapters.go`

**Change collector interfaces from**:
```go
Events() <-chan domain.Event
```

**To**:
```go
Events() <-chan domain.UnifiedEvent
```

### Step 2: Update CollectorManager

**File**: `/pkg/integrations/collector-manager/manager.go`

**Change**:
- Line 15: `eventChan chan domain.UnifiedEvent`
- Line 24: `Events() <-chan domain.UnifiedEvent`
- Update all Event references to UnifiedEvent

### Step 3: Remove Event Conversions

**Search and remove all**:
- `ConvertEventToUnified()`
- `ConvertUnifiedToEvent()`
- Event to UnifiedEvent mappings

## Phase 2: Consolidate Orchestrators (2-3 days)

### Step 1: Create Unified Orchestrator

**Create**: `/pkg/intelligence/pipeline/unified_orchestrator.go`

```go
package pipeline

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// UnifiedOrchestrator manages the complete event pipeline
type UnifiedOrchestrator struct {
    collectors map[string]Collector
    pipeline   IntelligencePipeline
    config     *OrchestratorConfig
}

// This consolidates all orchestration logic
```

### Step 2: Deprecate Duplicate Orchestrators

Mark as deprecated:
- `/pkg/integrations/collector/orchestrator.go`
- `/pkg/integrations/server/orchestrator.go`

### Step 3: Update Integration Points

Update all references to use the new unified orchestrator.

## Phase 3: Architecture Compliance (1 week)

### Step 1: Fix Import Violations

**Files with violations**:
- `/pkg/collectors/ebpf/internal/collector.go` - Remove intelligence imports
- `/pkg/integrations/security/*.go` - Remove interface imports

### Step 2: Create Proper Interfaces

**Create**: `/pkg/integrations/core/collector_interface.go`

```go
package core

import (
    "context"
    "github.com/yairfalse/tapio/pkg/domain"
)

// Collector is the unified interface for all collectors
type Collector interface {
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan *domain.UnifiedEvent
    Health() Health
}
```

### Step 3: Enforce Build Rules

**Create**: `Makefile` target

```makefile
check-imports:
    @echo "Checking architectural compliance..."
    @! find pkg/collectors -name "*.go" -exec grep -l "pkg/intelligence\|pkg/integrations\|pkg/interfaces" {} \;
    @! find pkg/intelligence -name "*.go" -exec grep -l "pkg/integrations\|pkg/interfaces" {} \;
    @! find pkg/integrations -name "*.go" -exec grep -l "pkg/interfaces" {} \;
```

## Phase 4: Complete Integration (1 week)

### Step 1: Wire Persistence

**Update**: `/pkg/intelligence/pipeline/integration.go`

Add WAL integration:
```go
walStore, err := wal.NewStore(walConfig)
if err != nil {
    return err
}
pi.correlationStore = walStore
```

### Step 2: Add Health Checks

Create unified health check system across all layers.

### Step 3: Add Metrics Collection

Integrate with OTEL for complete observability.

## Testing Strategy

### Unit Tests
- Test each component in isolation
- Mock interfaces between layers
- Verify type conversions are removed

### Integration Tests
- Test complete flow: Collector → Pipeline → Storage
- Verify no data loss during processing
- Test error scenarios

### Performance Tests
- Benchmark event throughput
- Measure memory usage
- Test under load

## Rollout Plan

1. **Week 1**: Fix blocking issues (DataFlow)
2. **Week 2**: Complete type migration
3. **Week 3**: Consolidate orchestrators
4. **Week 4**: Fix architecture violations
5. **Week 5**: Complete integration and testing

## Success Metrics

- [ ] All builds pass
- [ ] No Event type usage (only UnifiedEvent)
- [ ] Single orchestrator implementation
- [ ] No architecture violations
- [ ] 80%+ test coverage
- [ ] Performance benchmarks pass

## Risk Mitigation

1. **Breaking Changes**: Use feature flags for gradual rollout
2. **Performance Impact**: Benchmark before/after each phase
3. **Data Loss**: Implement comprehensive logging
4. **Rollback Plan**: Tag releases before each phase

## Documentation Updates

1. Update architecture diagrams
2. Update API documentation
3. Create migration guide for downstream users
4. Update collector implementation guide