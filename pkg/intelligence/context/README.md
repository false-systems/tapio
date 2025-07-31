# Context Package

The `context` package provides individual event intelligence capabilities for the Tapio observability platform. It enriches single events with semantic understanding, validates their structure, and assesses their potential impact.

## Overview

This package is responsible for:
- **Event Validation**: Ensuring events meet structural and temporal requirements
- **Confidence Scoring**: Calculating reliability scores based on event properties
- **Impact Assessment**: Determining infrastructure and operational impact of events
- **Context Building**: Enriching events with semantic and operational context

## Architecture

```
context/
├── validation.go      # Event validation logic
├── confidence.go      # Confidence scoring algorithms
├── impact.go         # Impact assessment calculations
└── builder.go        # Context enrichment utilities
```

## Key Components

### EventValidator

Validates UnifiedEvents before processing:
- Required field validation (ID, Timestamp, Type, Source)
- Temporal validation (max age: 24 hours)
- Layer-specific data consistency checks

```go
validator := NewEventValidator()
err := validator.Validate(event)
```

### ConfidenceScorer

Calculates confidence scores (0.0-1.0) based on:
- Data completeness
- Source reliability
- Temporal factors
- Semantic richness

```go
scorer := NewConfidenceScorer()
confidence := scorer.CalculateConfidence(event)
```

### ImpactAnalyzer

Assesses potential impact across multiple dimensions:
- Infrastructure impact (0.0-1.0)
- Service dependencies
- Component cascade risk
- SLO implications

```go
analyzer := NewImpactAnalyzer()
impact := analyzer.AssessImpact(event)
```

### ContextBuilder

Orchestrates context enrichment:
```go
builder := NewContextBuilder()
enrichedEvent, err := builder.BuildContext(event)
```

## Usage Examples

### Basic Event Validation

```go
import "github.com/yairfalse/tapio/pkg/intelligence/context"

validator := context.NewEventValidator()
event := &domain.UnifiedEvent{
    ID:        "evt-123",
    Type:      domain.EventTypeSystem,
    Timestamp: time.Now(),
    Source:    "kubernetes-collector",
}

if err := validator.Validate(event); err != nil {
    log.Printf("Invalid event: %v", err)
}
```

### Complete Context Enrichment

```go
builder := context.NewContextBuilder()

// Process an event
event := &domain.UnifiedEvent{
    ID:   "evt-456",
    Type: domain.EventTypeMemory,
    // ... other fields
}

enrichedEvent, err := builder.BuildContext(event)
if err != nil {
    return fmt.Errorf("context building failed: %w", err)
}

// Access enriched data
fmt.Printf("Confidence: %.2f\n", enrichedEvent.Semantic.Confidence)
fmt.Printf("Infrastructure Impact: %.2f\n", enrichedEvent.Impact.InfrastructureImpact)
```

## Configuration

### Validation Settings

```go
// Custom validation with extended time window
validator := context.NewEventValidatorWithConfig(48 * time.Hour)
```

### Confidence Scoring Weights

The confidence scorer uses configurable weights:
- Data completeness: 40%
- Source reliability: 30%
- Temporal factors: 20%
- Semantic richness: 10%

## Performance Characteristics

- **Validation**: O(1) for basic checks, O(n) for layer data validation
- **Confidence Scoring**: O(1) computation
- **Impact Assessment**: O(n) where n is number of relationships
- **Memory Usage**: Minimal, no persistent state

## Best Practices

1. **Always Validate First**: Run validation before any processing
2. **Cache Results**: Confidence and impact scores are deterministic
3. **Handle Nil Fields**: All methods handle missing optional fields gracefully
4. **Use Builders**: Prefer ContextBuilder over individual components

## Testing

```bash
cd pkg/intelligence/context
go test -v ./...
go test -bench=. -benchmem
```

## Integration

This package integrates with:
- **Pipeline Package**: Used in validation and context stages
- **Correlation Package**: Provides enriched events for correlation
- **Domain Package**: Uses UnifiedEvent structure

## Error Handling

All methods return descriptive errors:
- Validation errors include specific field failures
- Context building errors preserve original error context
- No panics - all errors are returned

## Future Enhancements

- Machine learning-based confidence scoring
- Dynamic impact assessment rules
- Custom validation rules per event type
- Performance profiling hooks