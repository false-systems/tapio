# Tapio V2 Correlation Engine Integration Plan

## Executive Summary

This document outlines the detailed integration plan for introducing the V2 high-performance correlation engine into the existing Tapio architecture while maintaining backward compatibility and enabling gradual migration.

## Current Architecture Analysis

### 1. Data Flow Overview

```
Event Sources → Collectors → EventBridge → Correlation Engine V1 → Results → Actions
     ↓              ↓            ↓                    ↓
   eBPF         SimpleManager  Conversion         Rule Engine
   K8s          Integration    to unified        Event Store
   Journald     Manager        format             Context Builder
```

### 2. Key Integration Points

#### 2.1 Integration Manager (`pkg/events_correlation/integration/manager.go`)
- **Role**: Central orchestrator that combines Tapio collectors with correlation engine
- **Key Methods**:
  - `RegisterCollectors()`: Manages collector registration
  - `processEvents()`: Batches events for correlation processing
  - `processBatch()`: Sends events to correlation engine
  - `Results()`: Provides unified result channel

#### 2.2 Event Bridge (`pkg/events_correlation/bridge/tapio.go`)
- **Role**: Converts between Tapio's `collector.Event` and `events_correlation.Event`
- **Key Functions**:
  - Event type conversion and normalization
  - Entity extraction and metadata mapping
  - Fingerprint generation for deduplication
  - Insight conversion to correlation results

#### 2.3 Correlation Engine V1 (`pkg/events_correlation/engine.go`)
- **Architecture**: 
  - Single-threaded rule execution with goroutine pool
  - In-memory event store with time-based retention
  - Synchronous rule evaluation with timeout protection
  - Result handlers for downstream processing

#### 2.4 Rule System
- **Registration**: Static rule registration through `rules.RegisterAll()`
- **Execution**: Sequential evaluation with cooldown management
- **Categories**: Resource, Performance, Reliability, Network

## V2 Integration Architecture

### 1. Hybrid Router Design

```go
// pkg/events_correlation/router/hybrid_engine.go
type HybridCorrelationEngine struct {
    v1Engine    events_correlation.Engine
    v2Engine    *correlation_v2.HighPerformanceEngine
    router      *TrafficRouter
    metrics     *HybridMetrics
    config      HybridConfig
}

type TrafficRouter struct {
    v2Percentage    atomic.Int32  // 0-100
    ruleRouting     map[string]EngineVersion
    entityRouting   map[string]EngineVersion
    loadBalancer    *LoadBalancer
}

type HybridConfig struct {
    EnableV2        bool
    V2Percentage    int32
    RoutingStrategy RoutingStrategy
    RollbackConfig  RollbackConfig
}
```

### 2. Integration Phases

#### Phase 1: Infrastructure Setup (Week 1-2)
1. **Create Hybrid Engine Wrapper**
   ```go
   // Implement the events_correlation.Engine interface
   func (h *HybridCorrelationEngine) ProcessEvents(ctx context.Context, events []Event) ([]*Result, error) {
       // Route based on configuration
       if h.router.ShouldRouteToV2(events) {
           return h.processWithV2(ctx, events)
       }
       return h.v1Engine.ProcessEvents(ctx, events)
   }
   ```

2. **Modify Integration Manager**
   ```go
   // Update NewIntegratedManager to support hybrid engine
   func NewIntegratedManager(config IntegrationConfig) *IntegratedManager {
       var correlationEngine events_correlation.Engine
       
       if config.EnableHybridEngine {
           correlationEngine = hybrid.NewHybridEngine(
               events_correlation.NewEngine(eventStore, opts...),
               correlation_v2.NewHighPerformanceEngine(v2Config),
               config.HybridConfig,
           )
       } else {
           correlationEngine = events_correlation.NewEngine(eventStore, opts...)
       }
   }
   ```

3. **Event Adapter Layer**
   ```go
   // pkg/events_correlation/adapter/v2_adapter.go
   type V2EventAdapter struct {
       converter *EventConverter
   }
   
   func (a *V2EventAdapter) ConvertToV2(v1Event events_correlation.Event) *correlation_v2.Event {
       // Efficient conversion logic
   }
   ```

#### Phase 2: Rule Migration (Week 3-4)
1. **Dual Rule Registration**
   ```go
   func (h *HybridCorrelationEngine) RegisterRule(rule *Rule) error {
       // Register with V1
       if err := h.v1Engine.RegisterRule(rule); err != nil {
           return err
       }
       
       // Convert and register with V2 if compatible
       if v2Rule := h.convertToV2Rule(rule); v2Rule != nil {
           return h.v2Engine.RegisterRule(v2Rule)
       }
       
       return nil
   }
   ```

2. **Rule Compatibility Layer**
   ```go
   type RuleCompatibilityChecker struct {
       v2Compatible map[string]bool
   }
   
   func (r *RuleCompatibilityChecker) IsV2Compatible(rule *Rule) bool {
       // Check if rule can run on V2 engine
   }
   ```

#### Phase 3: Traffic Routing (Week 5-6)
1. **Progressive Rollout Controller**
   ```go
   type ProgressiveRollout struct {
       stages      []RolloutStage
       metrics     *RolloutMetrics
       rollback    *RollbackController
   }
   
   type RolloutStage struct {
       Percentage  int32
       Duration    time.Duration
       Criteria    SuccessCriteria
   }
   ```

2. **Smart Routing Strategies**
   - **By Rule Type**: Route specific rule categories to V2
   - **By Entity**: Route specific pods/namespaces to V2
   - **By Load**: Route to V2 when load exceeds threshold
   - **By Time**: Route percentage of traffic based on time

#### Phase 4: Monitoring & Rollback (Week 7-8)
1. **Comparison Metrics**
   ```go
   type EngineComparison struct {
       V1Latency       time.Duration
       V2Latency       time.Duration
       V1Throughput    float64
       V2Throughput    float64
       ResultMismatch  int
       V2Errors        int
   }
   ```

2. **Automatic Rollback**
   ```go
   func (h *HybridCorrelationEngine) monitorHealth() {
       if h.metrics.V2ErrorRate() > h.config.RollbackConfig.ErrorThreshold {
           h.router.SetV2Percentage(0)
           h.notifyRollback("High error rate detected")
       }
   }
   ```

### 3. Performance Optimizations

#### 3.1 Zero-Copy Event Routing
```go
// Use unsafe pointers for V1→V2 conversion where possible
type UnsafeEventConverter struct {
    // Avoid allocations during conversion
}
```

#### 3.2 Batch Processing Optimization
```go
func (h *HybridCorrelationEngine) ProcessBatch(events []Event) {
    // Split batch optimally between V1 and V2
    v1Batch, v2Batch := h.router.SplitBatch(events)
    
    // Process in parallel
    var wg sync.WaitGroup
    wg.Add(2)
    
    go func() {
        defer wg.Done()
        h.v1Results = h.v1Engine.ProcessEvents(ctx, v1Batch)
    }()
    
    go func() {
        defer wg.Done()
        h.v2Results = h.processV2Batch(v2Batch)
    }()
    
    wg.Wait()
}
```

#### 3.3 Result Deduplication
```go
type ResultDeduplicator struct {
    seen     map[string]time.Time
    window   time.Duration
}

func (d *ResultDeduplicator) Deduplicate(results []*Result) []*Result {
    // Remove duplicate correlations from V1/V2
}
```

### 4. Configuration Management

#### 4.1 Feature Flags
```yaml
correlation:
  hybrid:
    enabled: true
    v2_percentage: 10
    routing_strategy: "progressive"
    rollback:
      error_threshold: 0.05
      latency_threshold: 100ms
    features:
      v2_memory_rules: true
      v2_cpu_rules: false
      v2_network_rules: false
```

#### 4.2 Dynamic Configuration
```go
type DynamicConfig struct {
    watcher *ConfigWatcher
    updater *ConfigUpdater
}

func (d *DynamicConfig) Watch() {
    // Watch for config changes and update routing
}
```

### 5. Migration Tools

#### 5.1 Rule Migration CLI
```bash
# Analyze rule compatibility
tapio migrate analyze-rules

# Test rule on both engines
tapio migrate test-rule memory-pressure-cascade

# Migrate specific rules
tapio migrate rules --category=resource --to=v2
```

#### 5.2 Performance Comparison Tool
```go
type PerformanceComparator struct {
    v1Engine Engine
    v2Engine Engine
    
    func Compare(events []Event) ComparisonReport {
        // Run same events through both engines
        // Compare latency, throughput, results
    }
}
```

### 6. Safety Mechanisms

#### 6.1 Circuit Breaker
```go
type V2CircuitBreaker struct {
    failureThreshold int
    resetTimeout     time.Duration
    state           CircuitState
}

func (cb *V2CircuitBreaker) Call(fn func() error) error {
    if cb.state == Open {
        return ErrCircuitOpen
    }
    // Execute with circuit breaker protection
}
```

#### 6.2 Shadow Mode
```go
func (h *HybridCorrelationEngine) ProcessInShadowMode(events []Event) {
    // Process in V1 (primary)
    v1Results := h.v1Engine.ProcessEvents(ctx, events)
    
    // Process in V2 (shadow) - async, non-blocking
    go func() {
        v2Results := h.v2Engine.ProcessEvents(ctx, events)
        h.compareResults(v1Results, v2Results)
    }()
    
    // Return only V1 results
    return v1Results
}
```

## Implementation Timeline

### Week 1-2: Foundation
- [ ] Create hybrid engine structure
- [ ] Implement basic routing logic
- [ ] Set up configuration management
- [ ] Create event adapters

### Week 3-4: Integration
- [ ] Modify IntegratedManager
- [ ] Implement rule migration
- [ ] Create monitoring infrastructure
- [ ] Set up metrics collection

### Week 5-6: Testing
- [ ] Unit tests for hybrid engine
- [ ] Integration tests
- [ ] Performance benchmarks
- [ ] Load testing

### Week 7-8: Rollout
- [ ] Deploy to staging
- [ ] Progressive rollout to production
- [ ] Monitor metrics
- [ ] Document learnings

## Success Criteria

1. **Performance**: V2 shows >50% improvement in throughput
2. **Reliability**: Error rate <0.1%
3. **Compatibility**: 100% of existing rules work
4. **Rollback**: Can rollback in <30 seconds

## Risk Mitigation

1. **Data Loss**: Implement result buffering and retry logic
2. **Performance Degradation**: Circuit breakers and automatic rollback
3. **Rule Incompatibility**: Comprehensive testing and gradual migration
4. **Memory Leaks**: Regular profiling and memory limits

## Monitoring Dashboard

```yaml
metrics:
  - engine_selection_count{engine="v1|v2"}
  - processing_latency_ms{engine="v1|v2", percentile="50|90|99"}
  - throughput_events_per_second{engine="v1|v2"}
  - error_rate{engine="v1|v2", type="timeout|panic|invalid"}
  - memory_usage_bytes{engine="v1|v2"}
  - rule_execution_time_ms{engine="v1|v2", rule_id="..."}
  - result_comparison_mismatch_total
  - rollback_triggered_total
```

## Conclusion

This integration plan provides a safe, gradual path to introduce the V2 engine while maintaining system stability. The hybrid approach allows for extensive testing in production with minimal risk and automatic rollback capabilities.