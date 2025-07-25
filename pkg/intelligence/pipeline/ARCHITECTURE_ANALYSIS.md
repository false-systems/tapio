# Tapio Architecture Analysis

## Executive Summary

After analyzing the Tapio codebase, I've identified several architectural issues:

1. **Type Inconsistency**: Mixed usage of `domain.Event` (132 occurrences) and `domain.UnifiedEvent` (669 occurrences)
2. **Missing Components**: References to non-existent `dataflow` package
3. **Architectural Violations**: Some imports cross hierarchy boundaries
4. **Duplicate Functionality**: Multiple pipeline/orchestration implementations
5. **Integration Gaps**: Incomplete connections between layers

## Data Flow Path (Current State)

```
1. EVENT GENERATION
   └─> Collectors (L1: pkg/collectors/)
       ├─> eBPF Collector     → domain.UnifiedEvent
       ├─> K8s Collector      → domain.UnifiedEvent
       ├─> SystemD Collector  → domain.UnifiedEvent
       └─> CNI Collector      → domain.UnifiedEvent

2. EVENT AGGREGATION
   └─> CollectorManager (L3: pkg/integrations/collector-manager/)
       └─> Aggregates events from all collectors
       └─> Output: chan domain.UnifiedEvent

3. INTELLIGENCE PROCESSING
   └─> IntelligencePipeline (L2: pkg/intelligence/pipeline/)
       ├─> Validation Stage
       ├─> Context Building Stage
       ├─> Correlation Stage
       └─> Output: Enriched UnifiedEvents with correlations

4. INTEGRATION LAYER
   └─> Missing DataFlow Component (Referenced but not implemented)
       └─> Should bridge CollectorManager → IntelligencePipeline

5. API/SERVER LAYER
   └─> gRPC Server (L4: pkg/interfaces/server/grpc/)
       ├─> EventService
       ├─> CollectorService
       └─> TapioService
```

## Architectural Issues

### 1. Type System Confusion

**Problem**: Two event types coexist:
- `domain.Event` (deprecated, 132 uses)
- `domain.UnifiedEvent` (modern, 669 uses)

**Impact**: 
- Type conversions needed throughout the system
- Inconsistent data models
- Potential data loss during conversion

**Recommendation**: Complete migration to UnifiedEvent

### 2. Missing DataFlow Component

**Problem**: Multiple references to `dataflow.TapioDataFlow` but no implementation exists

**Files affected**:
- `/pkg/integrations/collector/orchestrator.go`
- `/pkg/integrations/server/orchestrator.go`
- Documentation references

**Impact**: 
- Build failures in integration layer
- Cannot bridge collectors to intelligence pipeline
- Missing semantic correlation capability

**Recommendation**: Either:
- Remove references and use IntelligencePipeline directly
- Implement the missing DataFlow component

### 3. Duplicate Orchestration

**Found duplicates**:
1. `/pkg/integrations/collector/orchestrator.go`
2. `/pkg/integrations/server/orchestrator.go`
3. `/pkg/intelligence/pipeline/orchestrator.go`
4. `/pkg/integrations/collector-manager/`

**Impact**:
- Confusion about which to use
- Duplicate maintenance burden
- Inconsistent behavior

**Recommendation**: Consolidate into single orchestration pattern

### 4. Architectural Violations

**Found violations**:
- Some L3 components importing from L4
- Circular dependency risks between intelligence and integrations

**Recommendation**: Strict enforcement of hierarchy rules

### 5. Integration Gaps

**Missing connections**:
- CollectorManager → IntelligencePipeline integration incomplete
- No clear event routing from collectors to processing
- Persistence layer (pkg/persistence/wal) not integrated

**Recommendation**: Create clear integration points

## Refactoring Recommendations

### Phase 1: Type System Cleanup
1. Complete migration from Event to UnifiedEvent
2. Update all collector adapters to emit UnifiedEvent
3. Remove Event type and conversion code

### Phase 2: Component Consolidation
1. Remove duplicate orchestrator implementations
2. Create single pipeline orchestration in pkg/intelligence/pipeline
3. Move collector management into pipeline orchestrator

### Phase 3: Fix Missing Components
1. Either implement DataFlow or remove references
2. If removing, update integration layer to use IntelligencePipeline directly
3. Update documentation to reflect actual architecture

### Phase 4: Integration Completion
1. Create clear integration between CollectorManager and IntelligencePipeline
2. Implement persistence integration for correlation results
3. Add WAL support for reliability

### Phase 5: Architecture Enforcement
1. Add build-time checks for hierarchy violations
2. Create clear interface boundaries between layers
3. Document integration patterns

## Simplified Architecture Proposal

```
┌─────────────────────────────────────────────────────────┐
│                    L4: API Layer                        │
│  gRPC/REST Server → Serves processed data              │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────┴────────────────────────────────┐
│                 L3: Integration Layer                    │
│  PipelineOrchestrator → Manages entire flow            │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────┴────────────────────────────────┐
│                L2: Intelligence Layer                    │
│  IntelligencePipeline → Processing & Correlation       │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────┴────────────────────────────────┐
│                  L1: Collector Layer                     │
│  Unified Collector Interface → All emit UnifiedEvent    │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────┴────────────────────────────────┐
│                   L0: Domain Layer                       │
│  UnifiedEvent → Single event type for entire system     │
└─────────────────────────────────────────────────────────┘
```

## Implementation Priority

1. **Critical**: Fix missing DataFlow references (blocking builds)
2. **High**: Complete Event → UnifiedEvent migration
3. **Medium**: Consolidate duplicate components
4. **Low**: Add persistence integration

## Metrics

- **Total Go files**: ~200
- **Event type usage**: 132 (old) vs 669 (new)
- **Duplicate components**: 4 orchestrators
- **Missing components**: 1 (DataFlow)
- **Architecture violations**: ~10 files

## Conclusion

The Tapio architecture has solid foundations but needs consolidation and cleanup. The main issues are:
1. Incomplete type migration
2. Missing components referenced in code
3. Duplicate implementations
4. Integration gaps

Following the recommended refactoring phases will result in a cleaner, more maintainable architecture that fully adheres to the 5-level hierarchy.