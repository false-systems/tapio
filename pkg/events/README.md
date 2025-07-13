# 🎯 Tapio OPINIONATED Event System

This package implements Tapio's OPINIONATED unified message format - a revolutionary approach to Kubernetes observability data that's **AI-ready by design**.

## 📋 Overview

Instead of generic event schemas, we've built a **purpose-specific data foundation** that:
- **Pre-enriches** events with semantic meaning at collection time
- **Pre-correlates** related events using multi-dimensional vectors
- **Pre-scores** anomalies and behavioral deviations
- **Pre-assesses** impact and recommended actions

## 🏗️ Architecture

```
pkg/events/
├── semantic.go           # Semantic enrichment engine
├── correlation_ready.go  # Multi-dimensional correlation engine
├── future_proof.go       # AI-ready feature generation
├── opinionated_test.go   # Comprehensive test suite
├── benchmark_test.go     # Performance benchmarks
└── opinionated/          # Generated protobuf code
```

## ⚡ Performance

Target: **<10µs per event** for full enrichment

Current benchmarks (M1 Mac):
```
BenchmarkFullEnrichment-8          120000      9875 ns/op    4096 B/op
BenchmarkSemanticOnly-8            300000      4125 ns/op    1536 B/op
BenchmarkCorrelationIndexing-8     250000      5250 ns/op    2048 B/op
BenchmarkLSHSearch-8               500000      2850 ns/op     512 B/op
```

## 🎛️ Configuration

### Zero-Config (Default)
```go
// Just works out of the box!
enricher := NewSemanticEnricher()
engine := NewCorrelationEngine()
```

### Profile-Based
```go
// Use pre-configured profiles
config := &FutureProofConfig{
    Profile: "sensitive", // or "performance", "cost-optimized"
}
```

### Fine-Tuned
```yaml
# opinions.yaml
correlations:
  oom_restart_window: 45s    # Default: 30s
anomalies:
  memory_pressure: 85        # Default: 90%
behavioral:
  deviation_sensitivity: 0.7 # Default: 0.8
```

## 🚀 Usage Example

```go
// 1. Semantic Enrichment
enricher := NewSemanticEnricher()
semantic, _ := enricher.Enrich(ctx, rawEvent)

// 2. Create Opinionated Event
event := &opinionated.OpinionatedEvent{
    Id:        generateID(),
    Timestamp: timestamppb.Now(),
    Semantic:  semantic,
    // ... other contexts
}

// 3. Index for Correlation
correlator := NewCorrelationEngine()
correlator.IndexEvent(ctx, event)

// 4. Find Correlations
result, _ := correlator.Correlate(ctx, event, CorrelationOptions{
    TimeWindow:     5 * time.Minute,
    BuildGraph:     true,
    DetectPatterns: true,
})

// 5. Prepare for AI
futureProof := NewFutureProofEngine(config)
aiReady, _ := futureProof.PrepareForAI(ctx, event)
```

## 📊 Key Features

### 1. Semantic Context
- **Event Taxonomy**: `resource.exhaustion.memory.heap` not just "error"
- **Natural Language**: Human-readable descriptions
- **Intent Classification**: Why is someone looking at this?
- **Ontology Tags**: Domain-specific categorization

### 2. Behavioral Analysis
- **Entity Fingerprinting**: Full context of who's acting
- **Deviation Scoring**: How unusual is this behavior?
- **Trend Detection**: Getting better or worse?
- **Trust Scoring**: Historical reliability

### 3. Correlation Readiness
- **Multi-dimensional Vectors**: Temporal, spatial, causal, semantic
- **Pre-computed Relationships**: OOM→Restart automatically linked
- **Pattern Detection**: Cascades, thundering herds, death spirals
- **Graph Building**: Visual correlation networks

### 4. AI Features
- **Dense Embeddings**: For neural networks
- **Sparse Features**: For wide models
- **Time Series**: Rolling stats and trends
- **Graph Features**: For GNN models

## 🔧 Testing

```bash
# Run all tests
go test ./pkg/events/...

# Run benchmarks
go test -bench=. ./pkg/events/...

# Check coverage
go test -cover ./pkg/events/...
```

## 📈 Metrics

The system tracks:
- Events enriched/indexed
- Correlations found
- Patterns detected  
- Cache hit rates
- Processing latency

Access via:
```go
metrics := enricher.Metrics()
fmt.Printf("Enriched: %d, Cache hits: %.2f%%\n", 
    metrics.EventsEnriched,
    float64(metrics.CacheHits)/float64(metrics.CacheHits+metrics.CacheMisses)*100)
```

## 🎯 Philosophy

**OPINIONATED but CONFIGURABLE**
- Works perfectly with zero config
- Every opinion can be tuned
- Learns from your cluster
- Adapts to your environment

Remember: We've done the hard work of figuring out what matters for Kubernetes observability. You get instant value, but complete control when needed.