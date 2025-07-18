# Markdown to Correlation Rules

This package allows users to write custom correlation rules in markdown and load them into Tapio's correlation engine.

## 🎯 Purpose

While Tapio comes with opinionated, high-quality built-in correlations, users can extend it with their own domain-specific patterns. Write correlations in plain English/Markdown, and Tapio will convert them to executable rules.

## ✨ Features

- **Natural Language**: Write correlations in plain English
- **Flexible Conditions**: Support for thresholds, durations, and complex conditions
- **Multiple Actions**: Root cause identification, predictions, recommendations
- **Metadata Support**: Severity, confidence, categories via inline text or YAML
- **Integration Ready**: Loads directly into Tapio's semantic rules engine

## 📝 Markdown Syntax

### Basic Pattern

```markdown
## Pattern Name

When [condition],
then [action].

[Optional metadata]
```

### Conditions

```markdown
# Threshold conditions
When memory > 80%
When CPU > 90% for 5 minutes
When latency > 500ms

# Multiple conditions (AND logic)
When memory > 80% and CPU > 90%

# Text conditions
When service is unhealthy
When database connection fails
```

### Actions

```markdown
# Basic insight
then alert high memory usage

# Root cause
Root cause: Memory leak in application

# Predictions  
Predict: OOM will occur within 10 minutes
Expect: Service degradation in 5 minutes

# Recommendations
Recommend: Scale up the service
Fix: Increase memory limits
Action: Restart the pod
```

### Metadata

```markdown
# Inline metadata
Severity: critical
Confidence: 85%
Category: memory

# YAML block for complex metadata
```yaml
severity: high
confidence: 90
category: performance
custom_field: value
```
```

## 🚀 Usage

### CLI Commands

```bash
# Load correlations from markdown
tapio correlations load my-patterns.md

# Validate markdown file
tapio correlations validate my-patterns.md

# Update/modify existing correlations
tapio correlations update my-updated-patterns.md

# Delete correlations by ID
tapio correlations delete user_memory_leak_pattern
tapio correlations delete rule1 rule2 rule3 --force

# List all loaded correlations
tapio correlations list
tapio correlations list --custom --category=memory

# Export as JSON
tapio correlations load my-patterns.md --json

# Dry run (parse without loading/updating)
tapio correlations load my-patterns.md --dry-run
tapio correlations update my-patterns.md --dry-run
```

### Programmatic Usage

```go
import "github.com/yairfalse/tapio/pkg/correlation/markdown"

// Create translator
translator := markdown.NewCorrelationTranslator()

// Read markdown content
content, _ := ioutil.ReadFile("my-patterns.md")

// Convert to semantic rules
rules, err := translator.TranslateMarkdownToRules(string(content))

// Or load directly into engine
engine := correlation.NewSemanticRulesEngine()
err = translator.LoadMarkdownRulesIntoEngine(string(content), engine)

// Update existing rules
err = translator.UpdateMarkdownRulesInEngine(string(content), engine)

// Delete rules by ID
ruleIDs := []string{"user_rule_1", "user_rule_2"}
err = translator.DeleteRulesFromEngine(ruleIDs, engine)
```

## 📖 Complete Example

```markdown
# Production Correlation Rules

## Memory Leak Detection

When memory usage > 85% and keeps increasing for 10 minutes,
then this indicates a memory leak.

Root cause: Check for heap allocation patterns in the application.
Recommend: Enable memory profiling and check for unclosed resources.

Severity: high
Confidence: 85%
Category: memory

## Database Cascade Failure

When database latency > 500ms for 30 seconds,
then predict API errors will start appearing within 2 minutes.

I expect to see:
- Connection pool exhaustion
- Request timeouts increasing  
- 5xx errors spiking

Root cause: Database is overwhelmed or network issues.
Fix: Scale database replicas or check for slow queries.

```yaml
severity: critical
confidence: 90
category: cascade_failure
```

## Service Dependency Pattern

If auth-service errors > 10 per second,
then api-gateway will fail within 30 seconds.

This is a known dependency cascade in our architecture.
Action: Check auth-service health immediately.

Category: dependency
```

## 🔧 How It Works

1. **Parser** extracts patterns from markdown structure
2. **Condition Extraction** identifies thresholds, operators, and durations
3. **Action Classification** categorizes actions (insight, prediction, recommendation)
4. **Metadata Parsing** extracts both inline and YAML metadata
5. **Translation** converts to Tapio's SemanticRule format
6. **Loading** integrates with correlation engine via JSON

## 🎨 Best Practices

1. **Be Specific**: Use exact metric names and thresholds
2. **Include Context**: Add descriptions explaining why the pattern matters
3. **Set Appropriate Severity**: Critical for immediate issues, high/medium/low for others
4. **Add Confidence**: Higher for well-known patterns, lower for experimental ones
5. **Categorize Properly**: Helps with rule organization and filtering

## 🤝 Integration with Correlation Engine

The translated rules become `SemanticRule` objects that:
- Execute alongside built-in correlations
- Support the same features (caching, performance hints, etc.)
- Can trigger automated actions (with safety controls)
- Appear in correlation findings with "user_defined" source

## 📚 See Also

- [Sample Correlations](examples/sample_correlations.md) - Full example file
- [Correlation Engine Docs](../README.md) - Main correlation engine documentation
- [Semantic Rules](../semantic_rules.go) - Semantic rule implementation