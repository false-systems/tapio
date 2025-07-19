# Human Output Generator

The Human Output module converts technical observability events and findings into human-readable insights, making complex system behavior understandable for developers, operators, and business stakeholders.

## Features

- **Natural Language Generation**: Transforms technical events into clear, actionable explanations
- **Multi-Audience Support**: Tailors explanations for developers, operators, or business users
- **Template-Based Generation**: Uses customizable templates for consistent messaging
- **Quality Assurance**: Built-in readability and complexity scoring
- **Interactive Elements**: Includes commands, links, and recommended actions
- **Multiple Output Formats**: Supports individual insights, reports, and summaries
- **Emoji Support**: Visual indicators for better UX (optional)
- **Confidence Scoring**: Indicates reliability of generated explanations

## Installation

```go
import "github.com/yairfalse/tapio/pkg/humanoutput"
```

## Quick Start

```go
// Create a generator with default configuration
config := humanoutput.DefaultConfig()
generator := humanoutput.NewGenerator(config)

// Generate human-readable insight from a finding
finding := &domain.Finding{
    Type:        "memory_leak",
    Severity:    domain.SeverityHigh,
    Title:       "Memory Leak Detected",
    Description: "Continuous memory growth in API service",
    Data: map[string]interface{}{
        "pod": "api-service-abc123",
        "namespace": "production",
        "memory_increase": "45",
    },
}

insight, err := generator.GenerateInsight(ctx, finding)
if err != nil {
    log.Fatal(err)
}

// Output human-readable explanation
fmt.Printf("%s %s\n", insight.Emoji, insight.Title)
fmt.Printf("What happened: %s\n", insight.WhatHappened)
fmt.Printf("What to do: %s\n", insight.WhatToDo)
```

## Configuration

```go
config := &humanoutput.Config{
    // Language settings
    DefaultLanguage:      "en",
    SupportedLanguages:   []string{"en"},
    
    // Style settings
    ExplanationStyle:     "simple",    // "technical", "simple", "executive"
    Audience:            "developer",   // "developer", "operator", "business"
    
    // Content settings
    MaxExplanationLength:   500,
    IncludeRecommendations: true,
    IncludeContext:        true,
    IncludeCommands:       true,
    IncludeEmoji:          true,
    
    // Quality settings
    EnableQualityCheck:    true,
    MinReadabilityScore:   0.6,
    MaxComplexityScore:    0.8,
}
```

## Core Types

### HumanInsight

The primary output type containing human-readable explanations:

```go
type HumanInsight struct {
    // Core explanation
    Title          string
    WhatHappened   string    // Clear description of the issue
    WhyItHappened  string    // Root cause explanation
    WhatItMeans    string    // Impact and consequences
    WhatToDo       string    // Actionable steps
    HowToPrevent   string    // Prevention strategies
    
    // Context
    BusinessImpact string    // Business implications
    UserImpact     string    // End-user effects
    
    // Metadata
    Severity       string
    Confidence     float64   // 0.0 to 1.0
    IsUrgent       bool
    IsActionable   bool
    
    // Interactive elements
    Commands       []string  // Useful kubectl/CLI commands
    RecommendedActions []RecommendedAction
}
```

### HumanReport

Comprehensive report from multiple findings:

```go
type HumanReport struct {
    Title         string
    Summary       string
    Insights      []*HumanInsight
    Trends        []Trend
    Recommendations []string
    OverallHealth string
}
```

### HumanSummary

System state summary from events:

```go
type HumanSummary struct {
    Title         string
    Overview      string
    KeyMetrics    map[string]string
    ActiveIssues  []IssueSummary
    SystemHealth  string
    NextSteps     []string
}
```

## Templates

The module includes pre-defined templates for common scenarios:

- **Memory Issues**: Memory leaks, OOM predictions
- **Network Problems**: Connectivity failures, timeouts
- **Performance**: Latency increases, CPU throttling
- **Storage**: Disk space warnings
- **Container**: Restart loops, crashes
- **Security**: Policy violations

### Custom Templates

Add custom templates for your specific use cases:

```go
template := &humanoutput.ExplanationTemplate{
    ID:                    "custom_alert",
    Name:                  "Custom Alert Template",
    EventCategories:       []string{"custom.alert"},
    Severities:           []string{"high"},
    WhatHappenedTemplate: "Custom alert triggered: {{.alert_name}}",
    WhyItHappenedTemplate: "Threshold exceeded: {{.threshold_value}}",
    WhatItMeansTemplate:  "This indicates {{.impact_description}}",
    WhatToDoTemplate:     "Check {{.resource_name}} and adjust {{.parameter}}",
    MinConfidence:        0.8,
}

templateManager.AddTemplate(template)
```

## Integration with SemanticCorrelationEngine

The module integrates seamlessly with the SemanticCorrelationEngine:

```go
// In semantic_correlation_engine.go
import "github.com/yairfalse/tapio/pkg/humanoutput"

type SemanticCorrelationEngine struct {
    // ... existing fields ...
    humanGenerator humanoutput.HumanOutputGenerator
}

func NewSemanticCorrelationEngine(batchSize int, batchTimeout time.Duration) *SemanticCorrelationEngine {
    return &SemanticCorrelationEngine{
        // ... existing initialization ...
        humanGenerator: humanoutput.NewGenerator(humanoutput.DefaultConfig()),
    }
}

// Add method to generate human insights
func (sce *SemanticCorrelationEngine) GenerateHumanInsight(ctx context.Context, finding *domain.Finding) (*humanoutput.HumanInsight, error) {
    return sce.humanGenerator.GenerateInsight(ctx, finding)
}
```

## Examples

### Basic Usage

See `examples/basic/main.go` for complete examples including:
- Converting findings to human insights
- Generating explanations from events
- Creating comprehensive reports
- Building system summaries

### Real-World Scenarios

#### Memory Leak Detection

```go
finding := &domain.Finding{
    Type:     "memory_leak",
    Severity: domain.SeverityHigh,
    Data: map[string]interface{}{
        "pod": "api-service-xyz",
        "memory_increase": "45",
        "time_window": "2 hours",
    },
}

insight, _ := generator.GenerateInsight(ctx, finding)
// Output:
// 🚨 High: Memory Leak Detected
// What happened: A memory leak was detected in api-service-xyz
// Why: The application is consuming memory continuously...
// What to do: 1. Review recent code changes...
```

#### Network Connectivity Issues

```go
event := &domain.Event{
    Category: domain.EventCategoryNetworkHealth,
    Severity: domain.SeverityCritical,
    Data: map[string]interface{}{
        "source": "frontend",
        "destination": "backend",
        "failure_count": "127",
    },
}

insight, _ := generator.GenerateEventExplanation(ctx, event)
// Output:
// ⚠️ Critical: Network connectivity issues detected
// Impact: Services cannot communicate reliably...
// Commands: kubectl get networkpolicies...
```

## Quality Metrics

The module automatically calculates quality metrics:

- **Readability Score**: Based on sentence complexity
- **Complexity Score**: Technical term density
- **Confidence Score**: Reliability of the explanation
- **Estimated Read Time**: Based on word count

## Best Practices

1. **Configure for Your Audience**: Set appropriate style and audience
2. **Use Templates**: Leverage templates for consistent messaging
3. **Include Context**: Enable context for richer explanations
4. **Review Quality Scores**: Monitor readability and complexity
5. **Customize Commands**: Add relevant kubectl commands
6. **Test Output**: Verify explanations make sense to target audience

## Performance Considerations

- Template matching is O(n) where n is number of templates
- Text generation is lightweight (string operations)
- Quality checks add minimal overhead
- Suitable for real-time correlation pipelines

## Future Enhancements

- [ ] Multi-language support
- [ ] AI-powered generation (when AI service available)
- [ ] Story narrative generation for complex incidents
- [ ] Customizable quality scoring algorithms
- [ ] Template hot-reloading
- [ ] Markdown and HTML output formats