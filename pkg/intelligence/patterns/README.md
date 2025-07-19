# Pattern Recognition Engine

AI-powered pattern detection for observability events, providing intelligent multi-source correlation and predictive analytics.

## Features

- **Multi-Source Event Correlation**: Correlates events from eBPF, systemd, and Kubernetes
- **Memory Leak Detection**: Sophisticated AI that detects memory leaks by analyzing kernel events, service restarts, and pod evictions
- **Extensible Pattern Framework**: Easy-to-use base pattern for creating new detection algorithms
- **Real-Time Processing**: Designed for high-throughput, low-latency pattern matching
- **Confidence Scoring**: Intelligent confidence calculation based on event completeness and temporal correlation
- **Pattern Manager**: Orchestrates multiple patterns with concurrent execution

## Installation

```go
import "github.com/yairfalse/tapio/pkg/patternrecognition"
```

## Quick Start

```go
// Create pattern recognition engine with default config
config := patternrecognition.DefaultConfig()
engine := patternrecognition.Engine(config)

// Analyze events for patterns
events := []domain.Event{
    // Your observability events
}

matches, err := engine.DetectPatterns(ctx, events)
if err != nil {
    log.Fatal(err)
}

// Process detected patterns
for _, match := range matches {
    fmt.Printf("Pattern detected: %s (confidence: %.2f)\n", 
        match.Pattern.Name, match.Confidence)
    fmt.Printf("Description: %s\n", match.Correlation.Description)
}
```

## Built-in Patterns

### Memory Leak Detection

The memory leak pattern is a sophisticated multi-source correlation algorithm that detects:

1. **eBPF Memory Pressure** (>80% usage)
2. **SystemD Service Restarts** (memory-related failures)
3. **Kubernetes Pod Evictions** (OOM kills)

The pattern uses temporal correlation to ensure events are properly sequenced and calculates confidence based on:
- Event completeness (all three sources present = higher confidence)
- Temporal alignment (events occur in expected sequence)
- Severity levels (critical events boost confidence)
- Event frequency (multiple occurrences increase confidence)

### Example Memory Leak Detection

```go
pattern := patternrecognition.NewMemoryLeakPattern()

// The pattern will detect sequences like:
// 1. eBPF detects 85% memory usage at 10:00
// 2. eBPF detects 90% memory usage at 10:05
// 3. SystemD restarts service due to memory at 10:08
// 4. Kubernetes evicts pod due to OOM at 10:10

// This creates a high-confidence correlation showing the memory leak progression
```

## Creating Custom Patterns

Extend the `BasePattern` to create your own detection algorithms:

```go
type MyCustomPattern struct {
    *patternrecognition.BasePattern
}

func NewMyCustomPattern() patternrecognition.Pattern {
    bp := patternrecognition.NewBasePattern(
        "my_custom_pattern",
        "My Custom Pattern",
        "Detects custom conditions in your system",
        patternrecognition.PatternCategoryCustom,
    )
    
    // Configure pattern behavior
    bp.SetTimeWindow(15 * time.Minute)
    bp.SetMinConfidence(0.75)
    bp.SetPriority(patternrecognition.PatternPriorityHigh)
    
    return &MyCustomPattern{BasePattern: bp}
}

func (p *MyCustomPattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
    // Implement your pattern matching logic
    // Use helper methods from BasePattern:
    // - SortEventsByTimestamp()
    // - GroupEventsByHost()
    // - FilterEventsByTimeWindow()
    // - CreateCorrelation()
}

func (p *MyCustomPattern) CanMatch(event domain.Event) bool {
    // Quick filter to determine if event is relevant
    return event.Type == domain.EventTypeCustom
}
```

## Configuration

```go
config := &patternrecognition.Config{
    // Engine settings
    Name:                "my-pattern-engine",
    Enabled:             true,
    MaxConcurrentEvents: 1000,
    
    // Pattern settings
    EnabledPatterns:     []string{"memory_leak", "cascade_failure"},
    DefaultTimeWindow:   30 * time.Minute,
    MinConfidenceScore:  0.7,
    
    // Performance settings
    BatchSize:           100,
    ProcessingTimeout:   5 * time.Second,
    PatternMatchTimeout: 1 * time.Second,
    
    // Memory management
    EventBufferSize:     10000,
    MaxEventsPerPattern: 100,
    CleanupInterval:     5 * time.Minute,
}
```

## Integration with SemanticCorrelationEngine

The pattern recognition engine integrates seamlessly with the SemanticCorrelationEngine:

```go
import "github.com/yairfalse/tapio/pkg/patternrecognition"

type SemanticCorrelationEngine struct {
    // ... existing fields ...
    patternEngine patternrecognition.PatternRecognitionEngine
}

func NewSemanticCorrelationEngine(batchSize int, batchTimeout time.Duration) *SemanticCorrelationEngine {
    return &SemanticCorrelationEngine{
        // ... existing initialization ...
        patternEngine: patternrecognition.Engine(patternrecognition.DefaultConfig()),
    }
}

// Add method to detect patterns
func (sce *SemanticCorrelationEngine) DetectPatterns(ctx context.Context, events []domain.Event) ([]patternrecognition.PatternMatch, error) {
    return sce.patternEngine.DetectPatterns(ctx, events)
}
```

## Pattern Statistics

Monitor pattern performance and effectiveness:

```go
stats := engine.GetPatternStats()

for patternID, matches := range stats.TotalMatches {
    fmt.Printf("Pattern %s: %d matches\n", patternID, matches)
    fmt.Printf("  Match rate: %.2f%%\n", stats.MatchRate[patternID] * 100)
    fmt.Printf("  Avg confidence: %.2f\n", stats.AverageConfidence[patternID])
    fmt.Printf("  Processing time: %v\n", stats.ProcessingTime[patternID])
}
```

## Performance Characteristics

- **Concurrent Processing**: Patterns are evaluated concurrently
- **Event Filtering**: CanMatch() provides fast pre-filtering
- **Time Window Management**: Efficient sliding window implementation
- **Memory Bounded**: Configurable limits on events per pattern
- **Timeout Protection**: Per-pattern timeout prevents blocking

## Advanced Features

### Temporal Correlation

The engine includes sophisticated temporal correlation to ensure events occur in expected sequences:

```go
// Memory leak pattern expects:
// 1. eBPF memory pressure events (earliest)
// 2. SystemD restarts (after memory pressure)
// 3. K8s evictions (last in sequence)

// Events out of sequence reduce confidence or invalidate the pattern
```

### Confidence Calculation

Confidence scores are calculated based on multiple factors:

- **Base Confidence**: From event type matches
- **Completeness Bonus**: More event sources = higher confidence
- **Severity Weighting**: Critical events boost confidence
- **Temporal Alignment**: Proper sequencing increases confidence
- **Frequency Factor**: Multiple similar events increase confidence

### Host-Based Grouping

Patterns automatically group events by host to detect host-specific issues:

```go
// Events from host1 and host2 are analyzed separately
// This allows detecting memory leaks on specific nodes
```

## Testing

Comprehensive test coverage includes:

```go
// Unit tests for pattern matching
go test ./pkg/patternrecognition

// Test specific patterns
go test -run TestMemoryLeakPattern

// Benchmark pattern performance
go test -bench=. ./pkg/patternrecognition
```

## Future Patterns

The framework is designed to support additional patterns:

- **Cascade Failure Detection**: Detects cascading failures across services
- **Network Failure Patterns**: Identifies network connectivity issues
- **OOM Prediction**: Predicts out-of-memory conditions before they occur
- **Performance Degradation**: Detects gradual performance decline
- **Security Anomalies**: Identifies suspicious activity patterns

## Best Practices

1. **Configure Time Windows**: Set appropriate time windows for your environment
2. **Tune Confidence Thresholds**: Adjust based on false positive tolerance
3. **Monitor Statistics**: Use pattern stats to optimize configuration
4. **Implement CanMatch**: Efficient pre-filtering improves performance
5. **Test Patterns**: Validate patterns with real event data

## Contributing

To add a new pattern:

1. Create a new file: `my_pattern.go`
2. Implement the `Pattern` interface
3. Add comprehensive tests
4. Update the default patterns list
5. Document the pattern behavior

## License

Part of the Tapio observability platform.