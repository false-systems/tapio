# 📝 Tapio Opinions - Natural Language Configuration

Write your Kubernetes observability opinions in plain English (Markdown) and let Tapio translate them into precise configuration.

## 🎯 Overview

The Opinions package allows you to configure Tapio's OPINIONATED behavior using natural language instead of YAML. Write documentation-style Markdown about how your cluster behaves, and we'll extract the configuration automatically.

## ✨ Features

- **Markdown → Config**: Write in plain English, get valid YAML
- **Config → Markdown**: Export existing configs for easy editing  
- **Smart Extraction**: NLP-based rule extraction from natural text
- **Validation**: Comprehensive validation with helpful error messages
- **Templates**: Pre-built templates for common scenarios
- **Interactive Builder**: CLI wizard for creating opinions
- **Enrichment**: Intelligent defaults based on context

## 🚀 Quick Start

### 1. Write Your Opinions in Markdown

```markdown
# My Production Cluster

## Memory Management
Our Java apps run well at 85% memory usage. When memory gets high, 
predict OOM 5 minutes in advance so we can scale.

## Correlation Windows  
When OOM happens, pods take about 45 seconds to restart because of 
graceful shutdown requirements.

## Service Importance
The payment-service is super critical (weight: 1.0) but the 
analytics service is less important (weight: 0.3).
```

### 2. Translate to Configuration

```bash
# Convert markdown to YAML
tapio opinions translate my-opinions.md -o my-opinions.yaml

# Validate the configuration
tapio opinions validate my-opinions.yaml

# Apply to cluster
kubectl apply -f my-opinions.yaml
```

### 3. Generated Configuration

```yaml
apiVersion: tapio.io/v1
kind: OpinionConfig
metadata:
  name: my-production-cluster
spec:
  anomaly_thresholds:
    memory_usage: 0.85
  prediction_config:
    prediction_windows:
      oom: 5m
  correlation_windows:
    oom_restart: 45s
  importance_weights:
    payment-service: 1.0
    analytics: 0.3
```

## 📚 Markdown Syntax Guide

### Basic Values

```markdown
- **Memory threshold**: 85%
- **OOM window**: 5 minutes
- Restart time: **45 seconds**
```

### Service-Specific Rules

```markdown
- `payment-api` can use up to **90% memory**
- `batch-job` pods can use up to **95% memory**
```

### Tables for Complex Rules

```markdown
| Time Period | Sensitivity | Description |
|-------------|-------------|-------------|
| Business Hours | 0.7 | High sensitivity |
| Night | 0.9 | Low sensitivity |
```

### YAML Blocks

```markdown
```yaml
service_weights:
  payment-service: 1.0
  analytics: 0.3
```
```

### Service Dependencies

```markdown
When **database** has issues, I expect to see:
- **api-gateway** errors within **10 seconds**
- **payment-service** errors within **15 seconds**
```

## 🎨 Templates

### Use Pre-built Templates

```bash
# List available templates
tapio opinions templates list

# Start from a template
tapio opinions init --template=high-traffic-api

# Popular templates:
- high-traffic-api: For stateless APIs with high load
- stateful-database: For databases and stateful sets
- batch-processing: For cron jobs and batch workloads
- microservices: For typical microservice architectures
```

### Interactive Builder

```bash
# Build opinions interactively
tapio opinions create --interactive

# Questions asked:
✓ Cluster name: production-api
✓ Cluster type: production
✓ Memory threshold: 85
✓ OOM prediction window: 5m
✓ Critical services: payment-api,order-service
```

## 🔧 CLI Usage

```bash
# Translate markdown to YAML
tapio opinions translate input.md -o output.yaml

# Export YAML to markdown for editing
tapio opinions export config.yaml -o editable.md

# Validate configuration
tapio opinions validate my-opinions.yaml

# Test with dry-run
tapio check --opinions=my-opinions.yaml --dry-run

# Live preview while editing
tapio opinions preview my-opinions.md --watch
```

## 📖 Complete Example

See [examples/opinions-template.md](../../examples/opinions-template.md) for a comprehensive real-world example.

## 🧪 Testing

```go
// Translate markdown programmatically
translator := opinions.NewTranslator()
config, err := translator.TranslateMarkdown(markdownContent)

// Validate configuration
validator := opinions.NewOpinionValidator()
err := validator.Validate(config)

// Get detailed validation results
result := validator.ValidateWithDetails(config)
for _, warning := range result.Warnings {
    log.Printf("Warning: %s - %s", warning.Field, warning.Message)
}
```

## 🎯 Best Practices

1. **Start Simple**: Begin with basic thresholds, add complexity as needed
2. **Document Why**: Always explain your reasoning in the markdown
3. **Use Templates**: Start from templates matching your workload
4. **Validate Often**: Run validation after each change
5. **Version Control**: Keep opinions in git with your infrastructure code

## 🔍 How It Works

1. **Parsing**: Markdown is parsed into structured sections
2. **Extraction**: NLP patterns extract configuration values
3. **Enrichment**: Smart defaults are applied based on context
4. **Validation**: Configuration is validated for correctness
5. **Generation**: Clean YAML is generated

## 🤝 Contributing

To add new extraction patterns:

1. Add pattern to `extractor.go`
2. Add test cases to `translator_test.go`
3. Update documentation

## 📄 License

Part of the Tapio project - making Kubernetes observability accessible to everyone.