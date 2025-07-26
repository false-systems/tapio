# Migration Guide: Analytics Engine to Intelligence Pipeline

This guide provides step-by-step instructions for migrating from the legacy Analytics Engine to the new high-performance Intelligence Pipeline.

## Table of Contents

1. [Overview](#overview)
2. [Breaking Changes](#breaking-changes)
3. [Migration Steps](#migration-steps)
4. [Code Examples](#code-examples)
5. [Testing Your Migration](#testing-your-migration)
6. [Rollback Plan](#rollback-plan)
7. [FAQ](#faq)

## Overview

The Intelligence Pipeline is a complete rewrite of the Analytics Engine, designed to handle 165,000+ events per second with improved reliability and lower latency. The migration involves updating your code to use the new pipeline interfaces and configuration patterns.

### Key Improvements

| Feature | Analytics Engine | Intelligence Pipeline | Improvement |
|---------|------------------|----------------------|-------------|
| Throughput | 10k events/sec | 165k+ events/sec | 16.5x |
| Latency P99 | 100ms | < 10ms | 10x |
| Memory Usage | 500MB base | 100MB base | 5x |
| CPU Efficiency | Single-threaded | Multi-core | N-core scaling |
| Error Handling | Basic | Circuit breakers | Automatic recovery |

## Breaking Changes

### 1. Package Structure

**Old:**
```go
import (
    "github.com/yairfalse/tapio/pkg/intelligence/analytics/engine"
    "github.com/yairfalse/tapio/pkg/intelligence/performance"
)
```

**New:**
```go
import (
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
    "github.com/yairfalse/tapio/pkg/intelligence/context"
    "github.com/yairfalse/tapio/pkg/intelligence/correlation"
)
```

### 2. Interface Changes

**Old Analytics Engine:**
```go
type AnalyticsEngine interface {
    ProcessData(data []byte) (*AnalyticsResult, error)
    GetInsights() ([]*Insight, error)
    Configure(config map[string]interface{}) error
}
```

**New Intelligence Pipeline:**
```go
type IntelligencePipeline interface {
    ProcessEvent(event *domain.UnifiedEvent) error
    ProcessBatch(events []*domain.UnifiedEvent) error
    Start(ctx context.Context) error
    Stop() error
    GetMetrics() PipelineMetrics
}
```

### 3. Configuration

**Old:**
```go
engine := analytics.NewEngine()
engine.Configure(map[string]interface{}{
    "workers": 4,
    "buffer": 1000,
})
```

**New:**
```go
pipeline, err := pipeline.NewPipelineBuilder().
    WithMode(pipeline.PipelineModeHighPerformance).
    WithMaxConcurrency(4).
    WithBufferSize(1000).
    Build()
```

### 4. Event Processing

**Old:**
```go
result, err := engine.ProcessData(jsonBytes)
if err != nil {
    return err
}
```

**New:**
```go
event := &domain.UnifiedEvent{
    ID:        "evt-123",
    Type:      domain.EventTypeSystem,
    Timestamp: time.Now(),
    Source:    "collector",
    // ... structured data
}
err := pipeline.ProcessEvent(event)
```

## Migration Steps

### Step 1: Update Dependencies

```bash
# Remove old packages
go mod edit -dropreplace github.com/yairfalse/tapio/pkg/intelligence/analytics
go mod edit -dropreplace github.com/yairfalse/tapio/pkg/intelligence/performance

# Update to latest
go get -u github.com/yairfalse/tapio/pkg/intelligence@latest
go mod tidy
```

### Step 2: Update Imports

Replace all imports in your codebase:

```bash
# Find all files using old imports
grep -r "intelligence/analytics" --include="*.go" .
grep -r "intelligence/performance" --include="*.go" .

# Update imports (example for sed on macOS/Linux)
find . -name "*.go" -exec sed -i '' \
  -e 's|intelligence/analytics/engine|intelligence/pipeline|g' \
  -e 's|intelligence/performance|intelligence/pipeline|g' {} +
```

### Step 3: Initialize Pipeline

Replace engine initialization:

```go
// Old code to remove
func initAnalytics() (*analytics.Engine, error) {
    engine := analytics.NewEngine()
    engine.Configure(map[string]interface{}{
        "workers": runtime.NumCPU(),
        "buffer": 10000,
    })
    return engine, nil
}

// New code
func initPipeline(ctx context.Context) (pipeline.IntelligencePipeline, error) {
    p, err := pipeline.NewPipelineBuilder().
        WithMode(pipeline.PipelineModeHighPerformance).
        WithMaxConcurrency(runtime.NumCPU()).
        WithBufferSize(10000).
        EnableCorrelation(true).
        EnableCircuitBreaker(true).
        Build()
    
    if err != nil {
        return nil, fmt.Errorf("failed to build pipeline: %w", err)
    }
    
    if err := p.Start(ctx); err != nil {
        return nil, fmt.Errorf("failed to start pipeline: %w", err)
    }
    
    return p, nil
}
```

### Step 4: Update Event Processing

Convert your data processing logic:

```go
// Old processing
func processData(engine *analytics.Engine, data []byte) error {
    result, err := engine.ProcessData(data)
    if err != nil {
        log.Printf("Processing failed: %v", err)
        return err
    }
    
    insights := result.GetInsights()
    for _, insight := range insights {
        handleInsight(insight)
    }
    return nil
}

// New processing
func processEvent(pipeline pipeline.IntelligencePipeline, data []byte) error {
    // Parse data into UnifiedEvent
    var event domain.UnifiedEvent
    if err := json.Unmarshal(data, &event); err != nil {
        return fmt.Errorf("invalid event data: %w", err)
    }
    
    // Ensure required fields
    if event.ID == "" {
        event.ID = domain.GenerateEventID()
    }
    if event.Timestamp.IsZero() {
        event.Timestamp = time.Now()
    }
    
    // Process through pipeline
    if err := pipeline.ProcessEvent(&event); err != nil {
        return fmt.Errorf("pipeline processing failed: %w", err)
    }
    
    return nil
}
```

### Step 5: Update Metrics Collection

Replace analytics metrics with pipeline metrics:

```go
// Old metrics
func collectMetrics(engine *analytics.Engine) {
    stats := engine.GetStats()
    prometheus.Set("events_processed", stats.EventsProcessed)
    prometheus.Set("errors", stats.Errors)
}

// New metrics
func collectMetrics(pipeline pipeline.IntelligencePipeline) {
    metrics := pipeline.GetMetrics()
    
    // Rich metrics available
    prometheus.Set("events_processed", metrics.EventsProcessed)
    prometheus.Set("events_failed", metrics.EventsFailed)
    prometheus.Set("throughput", metrics.ThroughputPerSecond)
    prometheus.Set("latency_p99", metrics.P99Latency.Seconds())
    prometheus.Set("error_rate", metrics.ErrorRate)
    
    // Circuit breaker status
    if metrics.CircuitBreakerState == "open" {
        prometheus.Set("circuit_breaker_open", 1)
    } else {
        prometheus.Set("circuit_breaker_open", 0)
    }
}
```

### Step 6: Update Shutdown Logic

Ensure graceful shutdown:

```go
// Old shutdown
func shutdown(engine *analytics.Engine) {
    engine.Stop()
}

// New shutdown
func shutdown(pipeline pipeline.IntelligencePipeline) error {
    // Graceful shutdown with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    done := make(chan error, 1)
    go func() {
        done <- pipeline.Shutdown()
    }()
    
    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return fmt.Errorf("shutdown timeout exceeded")
    }
}
```

## Code Examples

### Example 1: Basic Migration

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
    
    "github.com/yairfalse/tapio/pkg/intelligence/pipeline"
    "github.com/yairfalse/tapio/pkg/domain"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Create pipeline
    p, err := pipeline.NewHighPerformancePipeline()
    if err != nil {
        log.Fatal(err)
    }
    
    // Start pipeline
    if err := p.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Handle shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    // Process events
    go func() {
        for {
            event := &domain.UnifiedEvent{
                ID:        domain.GenerateEventID(),
                Type:      domain.EventTypeSystem,
                Timestamp: time.Now(),
                Source:    "migration-example",
            }
            
            if err := p.ProcessEvent(event); err != nil {
                log.Printf("Processing error: %v", err)
            }
            
            time.Sleep(10 * time.Millisecond)
        }
    }()
    
    // Wait for shutdown signal
    <-sigChan
    log.Println("Shutting down...")
    
    if err := p.Shutdown(); err != nil {
        log.Printf("Shutdown error: %v", err)
    }
}
```

### Example 2: Custom Configuration

```go
func createCustomPipeline() (pipeline.IntelligencePipeline, error) {
    return pipeline.NewPipelineBuilder().
        // Performance settings
        WithMode(pipeline.PipelineModeStandard).
        WithBatchSize(500).
        WithBufferSize(5000).
        WithMaxConcurrency(8).
        
        // Timeouts
        WithProcessingTimeout(10 * time.Second).
        WithMetricsInterval(5 * time.Second).
        
        // Features
        EnableValidation(true).
        EnableContext(true).
        EnableCorrelation(true).
        EnableCircuitBreaker(true).
        
        // Error handling
        WithErrorThreshold(0.05).      // 5% error rate
        WithCircuitBreakerThreshold(0.5). // 50% triggers circuit breaker
        
        Build()
}
```

### Example 3: Batch Processing

```go
func migrateBatchProcessing(p pipeline.IntelligencePipeline) error {
    // Prepare batch
    events := make([]*domain.UnifiedEvent, 1000)
    for i := range events {
        events[i] = &domain.UnifiedEvent{
            ID:        fmt.Sprintf("batch-%d", i),
            Type:      domain.EventTypeSystem,
            Timestamp: time.Now(),
            Source:    "batch-processor",
            Entity: &domain.EntityContext{
                Type: "service",
                Name: "api-gateway",
            },
        }
    }
    
    // Process batch
    if err := p.ProcessBatch(events); err != nil {
        return fmt.Errorf("batch processing failed: %w", err)
    }
    
    return nil
}
```

## Testing Your Migration

### 1. Unit Tests

Update your tests to use the new interfaces:

```go
func TestEventProcessing(t *testing.T) {
    // Create test pipeline
    p, err := pipeline.NewPipelineBuilder().
        WithMode(pipeline.PipelineModeDebug).
        EnableCorrelation(false). // Disable for unit tests
        Build()
    require.NoError(t, err)
    
    ctx := context.Background()
    err = p.Start(ctx)
    require.NoError(t, err)
    defer p.Shutdown()
    
    // Test event processing
    event := &domain.UnifiedEvent{
        ID:        "test-123",
        Type:      domain.EventTypeSystem,
        Timestamp: time.Now(),
        Source:    "test",
    }
    
    err = p.ProcessEvent(event)
    assert.NoError(t, err)
    
    // Verify metrics
    metrics := p.GetMetrics()
    assert.Equal(t, int64(1), metrics.EventsReceived)
}
```

### 2. Integration Tests

Test the full pipeline with your infrastructure:

```go
func TestPipelineIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    // Setup
    ctx := context.Background()
    p, err := pipeline.NewHighPerformancePipeline()
    require.NoError(t, err)
    
    err = p.Start(ctx)
    require.NoError(t, err)
    defer p.Shutdown()
    
    // Simulate real workload
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                event := generateTestEvent(id, j)
                err := p.ProcessEvent(event)
                assert.NoError(t, err)
            }
        }(i)
    }
    
    wg.Wait()
    
    // Verify results
    metrics := p.GetMetrics()
    assert.Equal(t, int64(10000), metrics.EventsReceived)
    assert.Less(t, metrics.ErrorRate, 0.01) // Less than 1% errors
}
```

### 3. Performance Tests

Verify performance improvements:

```go
func BenchmarkPipelineVsAnalytics(b *testing.B) {
    // Benchmark new pipeline
    b.Run("Pipeline", func(b *testing.B) {
        p, _ := pipeline.NewHighPerformancePipeline()
        ctx := context.Background()
        p.Start(ctx)
        defer p.Shutdown()
        
        event := generateBenchmarkEvent()
        b.ResetTimer()
        
        for i := 0; i < b.N; i++ {
            p.ProcessEvent(event)
        }
    })
    
    // Results should show significant improvement
    // Example output:
    // BenchmarkPipelineVsAnalytics/Pipeline-8  1000000  150 ns/op
}
```

## Rollback Plan

If you need to rollback to the Analytics Engine:

### 1. Feature Flag Approach

```go
var useNewPipeline = os.Getenv("USE_NEW_PIPELINE") == "true"

func processEvents(data []byte) error {
    if useNewPipeline {
        return processWithPipeline(data)
    }
    return processWithAnalytics(data)
}
```

### 2. Gradual Migration

```go
// Route percentage of traffic to new pipeline
func shouldUseNewPipeline() bool {
    return rand.Float64() < 0.1 // Start with 10%
}
```

### 3. Quick Rollback

```bash
# Revert to previous version
git checkout tags/v1.0.0-analytics
go build ./...

# Or use previous Docker image
docker run -d company/tapio:analytics-latest
```

## FAQ

### Q: Can I run both systems in parallel?

Yes, you can run both during migration:

```go
func dualProcessing(data []byte) error {
    // Process with both systems
    go processWithAnalytics(data)  // Async, non-blocking
    return processWithPipeline(data) // Primary path
}
```

### Q: How do I migrate custom analytics plugins?

Convert plugins to pipeline stages:

```go
// Old plugin
type AnalyticsPlugin interface {
    Process(data []byte) error
}

// New stage
type ProcessingStage interface {
    Name() string
    Process(ctx context.Context, event *domain.UnifiedEvent) error
}

// Migration wrapper
type PluginAdapter struct {
    plugin AnalyticsPlugin
}

func (p *PluginAdapter) Process(ctx context.Context, event *domain.UnifiedEvent) error {
    data, _ := json.Marshal(event)
    return p.plugin.Process(data)
}
```

### Q: What about historical data?

The pipeline works with new event format, but you can convert:

```go
func migrateHistoricalData(oldData []OldFormat) error {
    events := make([]*domain.UnifiedEvent, len(oldData))
    
    for i, old := range oldData {
        events[i] = convertToUnifiedEvent(old)
    }
    
    return pipeline.ProcessBatch(events)
}
```

### Q: Performance not meeting expectations?

Check these settings:

```go
// Ensure high-performance mode
pipeline.NewPipelineBuilder().
    WithMode(pipeline.PipelineModeHighPerformance).
    WithMaxConcurrency(runtime.NumCPU() * 2). // Oversubscribe
    WithBatchSize(5000). // Larger batches
    WithBufferSize(50000). // Larger buffer
    Build()
```

## Support

For migration support:
- GitHub Issues: https://github.com/yairfalse/tapio/issues
- Documentation: https://docs.tapio.io/migration
- Troubleshooting Guide: [troubleshooting-pipeline.md](troubleshooting-pipeline.md)
- Community Slack: #tapio-migration

## Next Steps

After successful migration:
1. Monitor metrics for performance validation
2. Remove analytics engine code
3. Update documentation
4. Train team on new pipeline features
5. Explore advanced features (custom stages, correlation patterns)