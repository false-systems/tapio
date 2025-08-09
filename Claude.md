# TAPIO DEVELOPMENT STANDARDS - PRODUCTION GRADE ONLY

## 🎯 MISSION
Build enterprise-grade observability platform with zero tolerance for incomplete code. Every line must be production-ready, tested, and performant.

## ⚠️ CRITICAL DEVELOPMENT WORKFLOW

### REFACTOR MODE (TEMPORARY - FOR MEGA REFACTOR ONLY)
During major refactoring, use incremental validation for faster iteration:

```bash
# ENABLE REFACTOR MODE (95% faster validation, incremental CI)
make refactor-mode

# FAST VALIDATION DURING REFACTOR
make refactor-quick-check    # Check only changed files (5-30 sec)
make refactor-validate       # Complete but fast validation (1-3 min)
make show-changed-packages  # See what packages changed

# RETURN TO PRODUCTION MODE AFTER REFACTOR
make production-mode         # Re-enable full validation
```

**Refactor Mode Benefits:**
- Pre-commit: 5-30 sec vs 5-15 min (95% faster)
- CI Pipeline: 2-5 min vs 15-30 min (85% faster)
- Only checks changed packages
- Essential safety maintained

**What STAYS ENFORCED in Refactor Mode:**
- ❌ NO TODOs, FIXMEs, XXX, HACK comments (ZERO TOLERANCE)
- ✅ Code formatting and imports
- ✅ Build verification
- ✅ Architecture hierarchy compliance
- ✅ Race condition detection

**What's Temporarily Relaxed:**
- Coverage requirement (warns instead of fails)
- Full codebase scanning (incremental only)
- Complete test suite (changed packages only)

### SMALL ITERATIONS WITH CONTINUOUS TESTING
**MANDATORY**: Test after EVERY change, not after accumulating changes.

```bash
# WRONG - Writing 500 lines then testing
Write collector.go (500 lines) → Test → 20 failures → Debug nightmare

# RIGHT - Incremental development
Write function signature → Build → Pass
Add validation → Test → Pass  
Add core logic (10 lines) → Test → Pass
Add error handling → Test → Pass
Add cleanup → Test → Pass
```

### TEST-DRIVEN DEVELOPMENT CYCLE
1. Write test first
2. Run test - see it fail
3. Write minimal code to pass
4. Refactor with confidence
5. Repeat for next feature

**NO CODE WITHOUT TESTS - PERIOD**

## 🚨 ZERO TOLERANCE POLICY

### INSTANT REJECTION CRITERIA
Code will be REJECTED if it contains:
- `TODO`, `FIXME`, `XXX`, `HACK` comments
- `panic()` calls (except in `init()` for critical failures)
- `fmt.Print*` for debugging (use structured logging)
- `log.Fatal()` or `os.Exit()` (graceful shutdown only)
- Empty function bodies or `return nil` placeholders
- `interface{}` in public APIs
- `map[string]interface{}` anywhere except JSON unmarshaling
- Ignored errors (`_ = someFunc()`)
- Magic numbers without constants
- Functions > 50 lines (refactor required)
- Test coverage < 80%
- Memory leaks or unsafe operations without validation

## 🏗️ ARCHITECTURE RULES (IMMUTABLE)

### 5-LEVEL DEPENDENCY HIERARCHY
```
Level 0: pkg/domain/       # ZERO dependencies
Level 1: pkg/collectors/   # Domain ONLY
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

**VIOLATION = IMMEDIATE TASK REASSIGNMENT**

### Import Rules
```go
// GOOD - Lower level import
package intelligence
import "github.com/yairfalse/tapio/pkg/domain"

// BAD - Higher level import  
package domain
import "github.com/yairfalse/tapio/pkg/collectors" // REJECTED
```

## 💀 GO CODE STANDARDS

### Type Safety Requirements
```go
// BAD - Never use interface{} in public APIs
func Process(data interface{}) error  // REJECTED

// GOOD - Use concrete types or generics
func Process[T EventData](data T) error  // ACCEPTED

// BAD - Map with interface values
type Config map[string]interface{}  // REJECTED

// GOOD - Structured configuration
type Config struct {
    Timeout   time.Duration `json:"timeout"`
    BatchSize int          `json:"batch_size"`
}
```

### Error Handling Pattern
```go
// BAD - Ignored errors
_ = collector.Start()  // REJECTED

// BAD - Generic errors
return fmt.Errorf("failed")  // REJECTED

// GOOD - Contextual errors with wrapping
if err := collector.Start(ctx); err != nil {
    return fmt.Errorf("failed to start collector %s: %w", name, err)
}
```

### Resource Management
```go
// BAD - No cleanup
func Process() error {
    conn := getConnection()
    return doWork(conn)  // LEAKED CONNECTION
}

// GOOD - Proper cleanup with defer
func Process() error {
    conn, err := getConnection()
    if err != nil {
        return fmt.Errorf("failed to get connection: %w", err)
    }
    defer conn.Close()
    
    return doWork(conn)
}
```

### Concurrency Patterns
```go
// BAD - Goroutine leak
func Start() {
    go worker()  // No way to stop
}

// GOOD - Managed goroutines
func Start(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                process()
            }
        }
    }()
}
```

## 🔥 C/eBPF CODE STANDARDS

### Memory Safety Requirements
```c
// BAD - Unchecked array access
char comm[16];
strcpy(comm, task->comm);  // BUFFER OVERFLOW RISK

// GOOD - Safe bounded copy
char comm[16];
bpf_probe_read_kernel_str(comm, sizeof(comm), task->comm);
```

### Struct Alignment
```c
// BAD - Unaligned struct
struct event {
    u32 pid;
    u64 timestamp;  // MISALIGNED
    u32 tid;
};

// GOOD - Properly aligned and packed
struct event {
    u64 timestamp;
    u32 pid;
    u32 tid;
} __attribute__((packed));
```

### BPF Map Access
```c
// BAD - Unchecked map operations
struct data *d = bpf_map_lookup_elem(&my_map, &key);
d->value = 123;  // NULL DEREF POSSIBLE

// GOOD - Always check map lookups
struct data *d = bpf_map_lookup_elem(&my_map, &key);
if (!d) {
    return 0;  // Handle missing entry
}
d->value = 123;
```

## 🧪 TESTING REQUIREMENTS

### Unit Test Standards
```go
// BAD - Test with no assertions
func TestCollector(t *testing.T) {
    NewCollector("test")
    // No assertions - REJECTED
}

// BAD - Skipped tests
func TestComplexScenario(t *testing.T) {
    t.Skip("Too complex")  // REJECTED
}

// GOOD - Comprehensive test with proper assertions
func TestCollectorLifecycle(t *testing.T) {
    collector, err := NewCollector("test")
    require.NoError(t, err)
    require.NotNil(t, collector)
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    err = collector.Start(ctx)
    require.NoError(t, err)
    
    assert.True(t, collector.IsHealthy())
    
    err = collector.Stop()
    require.NoError(t, err)
}
```

### Test Coverage Rules
- Minimum 80% coverage per package
- 100% coverage for error paths
- All public APIs must have tests
- Edge cases must be tested
- Concurrent operations must be tested

## 📐 CORRELATION ENGINE STANDARDS

### Node Type Definitions
```go
// BAD - String-based node types
nodeType := "pod"  // REJECTED

// GOOD - Strongly typed enums
type NodeType int

const (
    NodeTypePod NodeType = iota
    NodeTypeService
    NodeTypeConfigMap
)
```

### Graph Query Safety
```go
// BAD - SQL injection vulnerable
query := fmt.Sprintf("MATCH (n:%s) WHERE n.name = '%s'", nodeType, name)

// GOOD - Parameterized queries
query := "MATCH (n:$nodeType) WHERE n.name = $name"
params := map[string]interface{}{
    "nodeType": nodeType,
    "name": name,
}
```

## 🔐 NEO4J INTEGRATION PATTERNS

### Transaction Management
```go
// BAD - No transaction rollback
tx, _ := store.BeginTransaction(ctx)
err := tx.Execute(query1)
err = tx.Execute(query2)  // If this fails, query1 remains
tx.Commit()

// GOOD - Proper transaction handling
tx, err := store.BeginTransaction(ctx)
if err != nil {
    return fmt.Errorf("failed to begin transaction: %w", err)
}
defer tx.Rollback()  // Always rollback on defer

if err := tx.Execute(query1, params1); err != nil {
    return fmt.Errorf("query1 failed: %w", err)
}

if err := tx.Execute(query2, params2); err != nil {
    return fmt.Errorf("query2 failed: %w", err)
}

return tx.Commit()
```

## 🔭 OPENTELEMETRY STANDARDS (MANDATORY)

### NO CUSTOM TELEMETRY WRAPPERS ALLOWED
All components MUST use OpenTelemetry directly. Custom telemetry packages are **FORBIDDEN**.

```go
// BAD - Custom telemetry wrappers (ARCHITECTURE VIOLATION)
import "github.com/yairfalse/tapio/pkg/integrations/telemetry"

instrumentation, err := telemetry.NewInstrumentation(logger)
instrumentation.RecordMetric(ctx, "events", 1)

// GOOD - Direct OpenTelemetry usage
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric" 
    "go.opentelemetry.io/otel/trace"
)

tracer := otel.Tracer("component-name")
meter := otel.Meter("component-name")
```

### OTEL Pattern for ALL Components
Every component must implement this exact pattern:

```go
package collector

import (
    "context"
    
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
    "go.uber.org/zap"
)

type Collector struct {
    logger *zap.Logger
    
    // OTEL instrumentation - REQUIRED fields
    tracer          trace.Tracer
    eventsProcessed metric.Int64Counter
    errorsTotal     metric.Int64Counter
    processingTime  metric.Float64Histogram
}

func NewCollector(name string, logger *zap.Logger) (*Collector, error) {
    // Initialize OTEL components - MANDATORY pattern
    tracer := otel.Tracer(name)
    meter := otel.Meter(name)
    
    // Create metrics with descriptive names and descriptions
    eventsProcessed, err := meter.Int64Counter(
        fmt.Sprintf("%s_events_processed_total", name),
        metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
    )
    if err != nil {
        logger.Warn("Failed to create events counter", zap.Error(err))
    }
    
    errorsTotal, err := meter.Int64Counter(
        fmt.Sprintf("%s_errors_total", name),
        metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
    )
    if err != nil {
        logger.Warn("Failed to create errors counter", zap.Error(err))
    }
    
    processingTime, err := meter.Float64Histogram(
        fmt.Sprintf("%s_processing_duration_ms", name),
        metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
    )
    if err != nil {
        logger.Warn("Failed to create processing time histogram", zap.Error(err))
    }
    
    return &Collector{
        logger:          logger,
        tracer:          tracer,
        eventsProcessed: eventsProcessed,
        errorsTotal:     errorsTotal,
        processingTime:  processingTime,
    }, nil
}

func (c *Collector) ProcessEvent(ctx context.Context, event Event) error {
    // Always start spans for operations
    ctx, span := c.tracer.Start(ctx, "collector.process_event")
    defer span.End()
    
    start := time.Now()
    defer func() {
        // Record processing time
        duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
        if c.processingTime != nil {
            c.processingTime.Record(ctx, duration, metric.WithAttributes(
                attribute.String("event_type", event.Type),
            ))
        }
    }()
    
    // Set span attributes for debugging
    span.SetAttributes(
        attribute.String("event.type", event.Type),
        attribute.String("event.id", event.ID),
    )
    
    // Your business logic here
    if err := c.processBusinessLogic(ctx, event); err != nil {
        // Record error metrics
        if c.errorsTotal != nil {
            c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
                attribute.String("error_type", "processing_failed"),
                attribute.String("event_type", event.Type),
            ))
        }
        
        // Record error in span
        span.SetAttributes(attribute.String("error", err.Error()))
        return fmt.Errorf("failed to process event: %w", err)
    }
    
    // Record success metrics
    if c.eventsProcessed != nil {
        c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
            attribute.String("event_type", event.Type),
            attribute.String("status", "success"),
        ))
    }
    
    return nil
}
```

### Metric Naming Standards
All metrics MUST follow these naming conventions:

```go
// Counters - Always end with _total
eventsProcessedCounter := "component_events_processed_total"
errorsCounter := "component_errors_total" 
requestsCounter := "component_requests_total"

// Histograms - Include unit in name
durationHistogram := "component_processing_duration_ms"     // milliseconds
sizeHistogram := "component_payload_size_bytes"             // bytes
latencyHistogram := "component_request_latency_seconds"     // seconds

// Gauges - Describe current state
activeConnectionsGauge := "component_active_connections"
bufferUtilizationGauge := "component_buffer_utilization_ratio"
queueSizeGauge := "component_queue_size"
```

### Span Naming Standards
```go
// BAD - Generic span names
span := tracer.Start(ctx, "process")
span := tracer.Start(ctx, "handler")

// GOOD - Descriptive hierarchical names
span := tracer.Start(ctx, "collector.process_event")
span := tracer.Start(ctx, "aggregator.resolve_conflicts") 
span := tracer.Start(ctx, "storage.write_correlation")
span := tracer.Start(ctx, "ebpf.parse_kernel_event")
```

### Required Attributes for Spans
Every span MUST include these attributes where applicable:

```go
span.SetAttributes(
    attribute.String("component", "collector-name"),
    attribute.String("operation", "process_event"),
    attribute.String("event.type", event.Type),
    attribute.String("event.id", event.ID),
    attribute.Int("batch.size", len(events)),
)

// For errors - ALWAYS record error details
span.SetAttributes(
    attribute.String("error", err.Error()),
    attribute.String("error.type", "validation_failed"),
)
```

### Metric Collection Rules
1. **ALL operations must be measured**
2. **ALL errors must be counted with context**
3. **ALL durations must be recorded in appropriate units**
4. **Check for nil before recording metrics** (graceful degradation)

```go
// GOOD - Safe metric recording with nil checks
func (c *Collector) recordMetric(ctx context.Context, value int64) {
    if c.eventsProcessed != nil {
        c.eventsProcessed.Add(ctx, value, metric.WithAttributes(
            attribute.String("component", c.name),
        ))
    }
}
```

### Cross-Cutting Concern Exception
OpenTelemetry is considered a **cross-cutting concern** and is exempt from the 5-level hierarchy:

```go
// ALLOWED - All levels can import OpenTelemetry directly
// Level 0 (domain): Can use OTEL for domain events
// Level 1 (collectors): Can use OTEL for collection metrics
// Level 2 (intelligence): Can use OTEL for aggregation metrics
// Level 3 (integrations): Can use OTEL for integration metrics
// Level 4 (interfaces): Can use OTEL for API metrics
```

### FORBIDDEN Patterns
```go
// FORBIDDEN - Custom telemetry wrapper
import "github.com/yairfalse/tapio/pkg/integrations/telemetry"

// FORBIDDEN - Non-descriptive metric names
meter.Int64Counter("count")
meter.Float64Histogram("time")
meter.Float64Gauge("value")

// FORBIDDEN - Missing error attributes
span.SetAttributes(attribute.String("error", "failed"))  // Too generic

// FORBIDDEN - No nil checks
c.counter.Add(ctx, 1)  // Could panic if counter creation failed
```

### Testing OpenTelemetry Integration
```go
func TestCollectorMetrics(t *testing.T) {
    // Use test metric reader to verify metrics are recorded
    reader := sdkmetric.NewManualReader()
    provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
    otel.SetMeterProvider(provider)
    
    collector, err := NewCollector("test", logger)
    require.NoError(t, err)
    
    // Process test event
    err = collector.ProcessEvent(ctx, testEvent)
    require.NoError(t, err)
    
    // Verify metrics were recorded
    metrics := &metricdata.ResourceMetrics{}
    err = reader.Collect(ctx, metrics)
    require.NoError(t, err)
    
    // Verify specific metrics exist
    assert.Contains(t, getMetricNames(metrics), "test_events_processed_total")
}
```

## ⚡ PERFORMANCE STANDARDS

### Memory Allocation Rules
```go
// BAD - Allocations in hot path
func ProcessEvent(e Event) {
    metadata := make(map[string]string)  // Allocation per event
    // ...
}

// GOOD - Pool reusable objects
var metadataPool = sync.Pool{
    New: func() interface{} {
        return make(map[string]string, 10)
    },
}

func ProcessEvent(e Event) {
    metadata := metadataPool.Get().(map[string]string)
    defer func() {
        clear(metadata)  // Go 1.21+ clear builtin
        metadataPool.Put(metadata)
    }()
    // ...
}
```

### Channel Buffer Sizes
```go
// BAD - Unbuffered channel in producer
events := make(chan Event)  // Will block

// GOOD - Appropriate buffer size
events := make(chan Event, 1000)  // Buffer based on load testing
```

## 📋 VERIFICATION COMMANDS (MANDATORY)

Before ANY commit, you MUST run and pass:

```bash
# 1. Format check (MUST return 0)
gofmt -l . | grep -v vendor | wc -l

# 2. Imports organization
goimports -w .

# 3. Build verification
go build ./...

# 4. Test execution
go test ./... -race

# 5. Coverage check (must be >= 80%)
go test ./... -cover | grep -E "coverage: [8-9][0-9]\.[0-9]%|coverage: 100\.0%"

# 6. Vet check
go vet ./...

# 7. Architecture verification
go list -f '{{.ImportPath}}: {{.Imports}}' ./... | python3 -c "
import sys
hierarchy = {
    'pkg/domain': 0,
    'pkg/collectors': 1,
    'pkg/intelligence': 2,
    'pkg/integrations': 3,
    'pkg/interfaces': 4
}
for line in sys.stdin:
    parts = line.strip().split(': ')
    if len(parts) != 2:
        continue
    pkg = parts[0]
    imports = parts[1].strip('[]').split()
    
    pkg_level = -1
    for key, level in hierarchy.items():
        if key in pkg:
            pkg_level = level
            break
    
    if pkg_level == -1:
        continue
        
    for imp in imports:
        for key, level in hierarchy.items():
            if key in imp and level > pkg_level:
                print(f'VIOLATION: {pkg} (L{pkg_level}) imports {imp} (L{level})')
                sys.exit(1)
"
```

## 🎯 DEFINITION OF DONE

A task is ONLY complete when:
- [ ] All code is formatted (`gofmt -l . | wc -l` returns 0)
- [ ] All imports organized (`goimports`)
- [ ] Builds successfully (`go build ./...`)
- [ ] All tests pass (`go test ./... -race`)
- [ ] Coverage >= 80% per package
- [ ] No linter warnings (`golangci-lint run`)
- [ ] No architecture violations
- [ ] No `TODO`, `FIXME`, or stub functions
- [ ] All errors handled with context
- [ ] All resources properly cleaned up
- [ ] Concurrent operations are race-free
- [ ] Memory safety validated (for C/eBPF)
- [ ] Performance benchmarks pass (if applicable)
- [ ] Documentation updated (if API changed)

## 🚫 COMMON ANTI-PATTERNS TO AVOID

### 1. The "Quick Fix" (FROM ACTUAL CODEBASE)
```go
// NEVER DO THIS - from pkg/intelligence/aggregator/aggregator.go
func (a *CorrelationAggregator) QueryCorrelations(ctx context.Context, query CorrelationQuery) (*AggregatedResult, error) {
    // TODO: Implement actual correlation query logic
    // For now, return a mock result
    result := &AggregatedResult{
        ID: fmt.Sprintf("corr-%d", time.Now().Unix()),
        // ... mock data ...
    }
    return result, nil  // INSTANT REJECTION - STUB FUNCTION
}

// NEVER DO THIS - from pkg/intelligence/aggregator/aggregator.go
func (a *CorrelationAggregator) ListCorrelations(ctx context.Context, limit, offset int) (*CorrelationList, error) {
    // TODO: Implement actual listing logic from storage
    return &CorrelationList{
        Correlations: []CorrelationSummary{},
        Total:        0,
    }, nil  // INSTANT REJECTION - RETURNING EMPTY STUB
}
```

### 2. The "Works On My Machine"
```go
// NEVER hardcode paths
configPath := "/Users/john/config.yaml"  // REJECTED

// Use environment or flags
configPath := os.Getenv("CONFIG_PATH")
```

### 3. The "Silent Failure" (FROM ACTUAL CODEBASE)
```go
// NEVER DO THIS - from pkg/intelligence/correlation/dependency_correlator.go
svcName, _ = props["name"].(string)  // REJECTED - IGNORED TYPE ASSERTION

// NEVER DO THIS - from pkg/intelligence/service_test.go  
_ = service.ProcessEvent(ctx, event)  // REJECTED - IGNORED ERROR

// NEVER DO THIS - from pkg/collectors/ebpf/collector_test.go
_, _ = NewCollector("ebpf-cgroup")  // REJECTED - DOUBLE IGNORED

// GOOD - Always check errors and type assertions
svcName, ok := props["name"].(string)
if !ok {
    return fmt.Errorf("service name not found or invalid type")
}

if err := service.ProcessEvent(ctx, event); err != nil {
    return fmt.Errorf("failed to process event: %w", err)
}
```

### 4. The "Resource Leak"
```go
// NEVER forget cleanup
file, _ := os.Open(path)
// ... use file ...
// file never closed - LEAK!

// ALWAYS use defer for cleanup
file, err := os.Open(path)
if err != nil {
    return err
}
defer file.Close()
```

### 5. The "Global State" (FROM ACTUAL CODEBASE)
```go
// NEVER DO THIS - from pkg/collectors/registry/registry.go
var (
    mu        sync.RWMutex
    factories = make(map[string]CollectorFactory)  // GLOBAL MUTABLE STATE
)

// NEVER DO THIS - panic in package-level code
func Register(name string, factory CollectorFactory) {
    if _, exists := factories[name]; exists {
        panic(fmt.Sprintf("collector %s already registered", name))  // PANIC IN LIBRARY CODE
    }
}

// GOOD - Use proper registry pattern with instances
type Registry struct {
    mu        sync.RWMutex
    factories map[string]CollectorFactory
}

func (r *Registry) Register(name string, factory CollectorFactory) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    if _, exists := r.factories[name]; exists {
        return fmt.Errorf("collector %s already registered", name)
    }
    r.factories[name] = factory
    return nil
}
```

### 6. The "Interface{} Factory" (FROM ACTUAL CODEBASE)
```go
// NEVER DO THIS - from pkg/collectors/registry/registry.go
type CollectorFactory func(config map[string]interface{}) (collectors.Collector, error)  // REJECTED

// NEVER DO THIS - from pkg/collectors/dns/init.go
func CreateCollector(config map[string]interface{}) (collectors.Collector, error)  // REJECTED

// GOOD - Use strongly typed configuration
type DNSConfig struct {
    ServerAddr   string        `json:"server_addr"`
    Timeout      time.Duration `json:"timeout"`
    MaxRetries   int          `json:"max_retries"`
}

type CollectorFactory func(config *DNSConfig) (collectors.Collector, error)
```

### 7. The "Test Skip" (FROM ACTUAL CODEBASE)
```go
// NEVER DO THIS - from pkg/collectors/ebpf/cgroup_test.go
t.Skip("eBPF not available in test environment")  // REJECTED - SKIPPING TESTS

// NEVER DO THIS - from pkg/intelligence/service_test.go
if os.Getenv("INTEGRATION_TEST") != "true" {
    t.Skip("Skipping integration test")  // REJECTED
}

// GOOD - Use build tags for integration tests
// +build integration

func TestIntegration(t *testing.T) {
    // Test runs only with -tags=integration
}
```

## 📝 GIT WORKFLOW ENFORCEMENT

### Branch Rules
- NEVER commit to main directly
- Branch names: `<type>/<description>` (e.g., `fix/cgroup-extraction`)
- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`

### Commit Standards
```bash
# GOOD commit message
git commit -m "fix(ebpf): resolve cgroup ID extraction bug

- Fixed incorrect cgroup ID reading from task_struct
- Added validation for cgroup ID vs PID confusion
- Improved memory safety in kernel event parsing

Closes #456"

# BAD commit message
git commit -m "fixed stuff"  # REJECTED
```

### PR Requirements
- Must pass ALL verification commands
- Must include test results in description
- Must have 2 approvals for main merge
- Must not decrease overall coverage

## 🎖️ EXAMPLES FROM ACTUAL CODEBASE

### GOOD: Proper Error Handling (from correlation engine)
```go
func (e *Engine) Process(ctx context.Context, event *domain.UnifiedEvent) error {
    if event == nil {
        return fmt.Errorf("cannot process nil event")
    }
    
    span, ctx := e.tracer.Start(ctx, "correlation.engine.process")
    defer span.End()
    
    results := make([]*CorrelationResult, 0, len(e.correlators))
    
    for _, correlator := range e.correlators {
        select {
        case <-ctx.Done():
            return fmt.Errorf("context cancelled during correlation: %w", ctx.Err())
        default:
        }
        
        corResults, err := correlator.Process(ctx, event)
        if err != nil {
            span.RecordError(err)
            e.metrics.RecordError(correlator.Name(), err)
            continue // Don't fail entire pipeline
        }
        
        results = append(results, corResults...)
    }
    
    return e.persistResults(ctx, results)
}
```

### BAD: Interface{} Abuse (NEVER DO THIS)
```go
// From old implementation - REJECTED
type EventData map[string]interface{}  

func (e *Event) GetData(key string) interface{} {
    return e.Data[key]  // Type information lost
}
```

### GOOD: Proper eBPF Memory Safety
```go
func (c *Collector) parseKernelEventSafely(buffer []byte) (*KernelEvent, error) {
    expectedSize := int(unsafe.Sizeof(KernelEvent{}))
    
    if len(buffer) < expectedSize {
        return nil, fmt.Errorf("buffer too small: got %d, need %d", len(buffer), expectedSize)
    }
    
    if len(buffer) != expectedSize {
        return nil, fmt.Errorf("buffer size mismatch: got %d, expected %d", len(buffer), expectedSize)
    }
    
    event := (*KernelEvent)(unsafe.Pointer(&buffer[0]))
    
    // Validate event fields
    if event.EventType == 0 || event.EventType > 10 {
        return nil, fmt.Errorf("invalid event type: %d", event.EventType)
    }
    
    if event.PID == 0 {
        return nil, fmt.Errorf("invalid PID: 0")
    }
    
    return event, nil
}
```

## 🔨 VERIFICATION SCRIPT

Create this script as `verify.sh` and run before EVERY commit:

```bash
#!/bin/bash
set -e

echo "🔍 TAPIO STRICT VERIFICATION"
echo "============================"

# 1. Check for TODOs and stubs
echo -n "Checking for TODOs/FIXMEs... "
if grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" . 2>/dev/null; then
    echo "❌ FAILED - Found TODO/FIXME/stub code"
    exit 1
fi
echo "✅ PASSED"

# 2. Check for ignored errors
echo -n "Checking for ignored errors... "
if grep -r "_ = " --include="*.go" . 2>/dev/null | grep -v "test.go"; then
    echo "❌ FAILED - Found ignored errors"
    exit 1
fi
echo "✅ PASSED"

# 3. Check for interface{} in public APIs
echo -n "Checking for interface{} abuse... "
if grep -r "interface{}" --include="*.go" . | grep -v "json" | grep -v "test.go" | grep "func.*interface{}"; then
    echo "❌ FAILED - Found interface{} in public APIs"
    exit 1
fi
echo "✅ PASSED"

# 4. Check for panic() calls
echo -n "Checking for panic() calls... "
if grep -r "panic(" --include="*.go" . | grep -v "init()" | grep -v "test.go"; then
    echo "❌ FAILED - Found panic() outside init()"
    exit 1
fi
echo "✅ PASSED"

# 5. Format check
echo -n "Checking code formatting... "
UNFORMATTED=$(gofmt -l . | grep -v vendor | wc -l)
if [ "$UNFORMATTED" -ne "0" ]; then
    echo "❌ FAILED - Code not formatted"
    gofmt -l . | grep -v vendor
    exit 1
fi
echo "✅ PASSED"

# 6. Build check
echo -n "Building project... "
if ! go build ./... 2>/dev/null; then
    echo "❌ FAILED - Build errors"
    go build ./...
    exit 1
fi
echo "✅ PASSED"

# 7. Test with race detector
echo -n "Running tests with race detector... "
if ! go test ./... -race -timeout 30s 2>/dev/null; then
    echo "❌ FAILED - Tests failed"
    go test ./... -race
    exit 1
fi
echo "✅ PASSED"

# 8. Coverage check
echo "Checking test coverage..."
go test ./... -cover | while read line; do
    if echo "$line" | grep -q "coverage:"; then
        COVERAGE=$(echo "$line" | sed 's/.*coverage: \([0-9.]*\)%.*/\1/')
        PACKAGE=$(echo "$line" | cut -d' ' -f2)
        if (( $(echo "$COVERAGE < 80" | bc -l) )); then
            echo "❌ FAILED - Package $PACKAGE has only $COVERAGE% coverage (minimum 80%)"
            exit 1
        fi
        echo "✅ $PACKAGE: $COVERAGE%"
    fi
done

# 9. Vet check
echo -n "Running go vet... "
if ! go vet ./... 2>/dev/null; then
    echo "❌ FAILED - Vet issues found"
    go vet ./...
    exit 1
fi
echo "✅ PASSED"

# 10. Architecture check
echo -n "Checking architecture rules... "
python3 -c "
import subprocess
import sys

hierarchy = {
    'pkg/domain': 0,
    'pkg/collectors': 1,
    'pkg/intelligence': 2,
    'pkg/integrations': 3,
    'pkg/interfaces': 4
}

result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}}: {{.Imports}}', './...'], 
                       capture_output=True, text=True)

violations = []
for line in result.stdout.split('\n'):
    if not line.strip():
        continue
    parts = line.split(': ')
    if len(parts) != 2:
        continue
    
    pkg = parts[0]
    imports = parts[1].strip('[]').split()
    
    pkg_level = -1
    for key, level in hierarchy.items():
        if key in pkg:
            pkg_level = level
            break
    
    if pkg_level == -1:
        continue
        
    for imp in imports:
        for key, level in hierarchy.items():
            if key in imp and level > pkg_level:
                violations.append(f'{pkg} (L{pkg_level}) imports {imp} (L{level})')

if violations:
    print('❌ FAILED - Architecture violations found:')
    for v in violations:
        print(f'  - {v}')
    sys.exit(1)
else:
    print('✅ PASSED')
"
if [ $? -ne 0 ]; then
    exit 1
fi

echo ""
echo "✅ ALL CHECKS PASSED - Code is production ready!"
```

## 🏆 FINAL WORDS

**NO EXCUSES. NO SHORTCUTS. NO COMPROMISES.**

Every line of code you write represents the quality of this platform. If you cannot deliver production-grade code that passes ALL requirements, the task will be immediately reassigned.

Remember:
- Format first, always (`make fmt`)
- Test everything (minimum 80% coverage)
- Handle all errors with context
- Clean up all resources
- Validate all inputs
- Document complex logic
- Benchmark critical paths

**DELIVER EXCELLENCE OR GET REASSIGNED.**