# Tapio Relay Architecture

## Overview

The Tapio Relay is an intelligent event aggregation and routing layer that sits between collectors and consumers, providing:

- **High-performance buffering** with backpressure
- **Intelligent routing** to multiple destinations
- **Native OTEL export** for enterprise observability
- **Event aggregation** and pattern detection
- **Zero-configuration** with sensible defaults

## Architecture Flow

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Collector 1   │     │   Collector 2   │     │   Collector N   │
│  (Node: node1)  │     │  (Node: node2)  │     │  (Node: nodeN)  │
└────────┬────────┘     └────────┬────────┘     └────────┬────────┘
         │                       │                       │
         │ gRPC Stream           │ gRPC Stream           │ gRPC Stream
         │ (Events)              │ (Events)              │ (Events)
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────────────────────────────────────────────────────┐
│                         TAPIO RELAY                             │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────────┐  │
│  │   Buffer    │  │  Aggregator  │  │    Smart Router       │  │
│  │  (100k cap) │  │ (Time Window)│  │  (Rule-based routing) │  │
│  └─────────────┘  └──────────────┘  └───────────────────────┘  │
│         │                 │                     │                │
│         └─────────────────┴─────────────────────┘                │
│                           │                                      │
│  ┌────────────────────────┼────────────────────────────────┐    │
│  │                  Event Processor                         │    │
│  │  - Batching (1000 events)                              │    │
│  │  - Compression                                         │    │
│  │  - Enrichment                                          │    │
│  └────────────────────────┬────────────────────────────────┘    │
│                           │                                      │
│        ┌──────────────────┼──────────────────┐                  │
│        ▼                  ▼                  ▼                  │
│  ┌──────────┐      ┌──────────┐      ┌──────────┐             │
│  │  Engine  │      │   OTEL   │      │ Metrics  │             │
│  │  Client  │      │ Exporter │      │ Exporter │             │
│  └─────┬────┘      └─────┬────┘      └─────┬────┘             │
└────────┼─────────────────┼─────────────────┼───────────────────┘
         │                 │                 │
         ▼                 ▼                 ▼
┌─────────────────┐ ┌─────────────┐ ┌─────────────────┐
│  Tapio Engine   │ │OTEL Collector│ │   Prometheus    │
│  (Correlation)  │ │  (Tracing)   │ │   (Metrics)     │
└─────────────────┘ └─────────────┘ └─────────────────┘
         │                 │
         ▼                 ▼
┌─────────────────┐ ┌─────────────┐
│   Tapio GUI     │ │   Jaeger/    │
│  (Real-time)    │ │   Grafana    │
└─────────────────┘ └─────────────┘
```

## Key Components

### 1. Event Buffer
- **Ring buffer** implementation for zero-allocation performance
- **100k event capacity** with backpressure signaling
- **Lock-free** for single producer scenarios
- **Automatic flow control** when buffer approaches capacity

### 2. Smart Router
- **Rule-based routing** with CEL expressions
- **Multi-destination** support (fan-out)
- **Priority-based** failover
- **Dynamic rule updates** without restart

### 3. Event Aggregator
- **Time-window based** aggregation (5s default)
- **Pattern detection** (error storms, restart loops)
- **Significance scoring** for prioritization
- **Correlation ID** assignment

### 4. OTEL Exporter
- **Native OTLP/gRPC** protocol
- **Automatic span creation** from events
- **Rich attributes** including Tapio intelligence
- **Batch export** with configurable size

## Deployment Patterns

### 1. Standard Deployment (Recommended)
```
Collectors → Relay → Engine → GUI/CLI
                ↓
             OTEL Collector → Jaeger/Grafana
```

### 2. Direct Mode (Low Latency)
```
Collectors → Engine → GUI/CLI
         ↓
      OTEL Export
```

### 3. Multi-Cluster Federation
```
Cluster A: Collectors → Relay A ─┐
                                 ├→ Central Relay → Engine
Cluster B: Collectors → Relay B ─┘
```

## Configuration

### Minimal Configuration (Zero-Config)
```yaml
# Relay works out-of-box with defaults:
# - Engine: localhost:9090
# - OTEL: localhost:4317
# - Port: 9095
```

### Production Configuration
```yaml
# /etc/tapio/relay.yaml
server:
  port: 9095
  
engine:
  endpoint: tapio-engine:9090
  
otel:
  enabled: true
  endpoint: otel-collector:4317
  
buffer:
  size: 200000  # Increased for production
  
aggregation:
  window: 10s   # Longer window for better patterns
  
routing:
  rules:
    - name: critical_events
      condition: "event.level == 'CRITICAL'"
      destinations:
        - type: engine
          priority: 1
        - type: otel
          priority: 1
        - type: webhook
          endpoint: https://alerts.example.com
          priority: 2
```

## Performance Characteristics

### Throughput
- **Input**: 165,000 events/sec per relay instance
- **Output**: 150,000 events/sec (with aggregation)
- **Latency**: <500µs added latency
- **Memory**: ~512MB baseline, scales with buffer

### Scaling
- **Horizontal**: 2-10 replicas with HPA
- **Vertical**: Up to 2 CPU cores efficiently
- **Buffer**: Configurable 10k-1M events

## Monitoring

### Metrics Exposed
```
# Event metrics
tapio_relay_events_received_total
tapio_relay_events_processed_total
tapio_relay_events_aggregated_total
tapio_relay_events_dropped_total

# Performance metrics
tapio_relay_processing_duration_seconds
tapio_relay_buffer_utilization_ratio
tapio_relay_export_duration_seconds

# Health metrics
tapio_relay_up
tapio_relay_circuit_breaker_open
```

### Health Endpoints
- `/health` - Basic health check
- `/ready` - Readiness check (includes downstream)
- `/metrics` - Prometheus metrics

## Best Practices

### 1. Buffer Sizing
- Dev/Test: 10k-50k events
- Production: 100k-500k events
- High-volume: 1M+ events (increase memory)

### 2. Aggregation Windows
- Real-time: 1-5 seconds
- Balanced: 5-10 seconds
- Batch: 30-60 seconds

### 3. Routing Rules
- Keep rules simple and specific
- Use priorities for failover
- Test rules before production

### 4. High Availability
- Always run 2+ replicas
- Use PodDisruptionBudget
- Configure anti-affinity

## Integration Examples

### With OTEL Collector
```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 1s
    
exporters:
  jaeger:
    endpoint: jaeger:14250
    
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [jaeger]
```

### With Prometheus
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'tapio-relay'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: tapio-relay
        action: keep
```

## Troubleshooting

### Common Issues

1. **Buffer Full**
   - Increase buffer size
   - Add more relay replicas
   - Check downstream health

2. **High Latency**
   - Reduce batch size
   - Decrease flush interval
   - Check network connectivity

3. **Events Dropped**
   - Enable debug logging
   - Check circuit breaker status
   - Verify routing rules

### Debug Commands
```bash
# Check relay status
kubectl logs -n tapio-system deployment/tapio-relay

# View metrics
kubectl port-forward -n tapio-system svc/tapio-relay 9096:9096
curl localhost:9096/metrics

# Test connection
grpcurl -plaintext localhost:9095 list
```