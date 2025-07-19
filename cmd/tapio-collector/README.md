# Tapio Collector

Unified event collector with OTEL semantic correlation for the Tapio observability platform.

## Features

- **Multiple Collectors**: eBPF, Kubernetes, SystemD, JournalD
- **OTEL Semantic Correlation**: Intelligent event grouping with trace context
- **High Performance**: 165k+ events/sec throughput
- **gRPC Streaming**: Efficient event forwarding to Tapio server
- **Impact Assessment**: Business impact and predictive analytics

## Architecture

```
┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│    eBPF     │ │     K8s     │ │   SystemD   │ │  JournalD   │
│  Collector  │ │  Collector  │ │  Collector  │ │  Collector  │
└──────┬──────┘ └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
       │               │               │               │
       └───────────────┴───────────────┴───────────────┘
                              │
                    ┌─────────▼─────────┐
                    │ Collector Manager │
                    └─────────┬─────────┘
                              │
                    ┌─────────▼─────────┐
                    │   OTEL Semantic    │
                    │   Correlation      │
                    │   (TapioDataFlow)  │
                    └─────────┬─────────┘
                              │
                    ┌─────────▼─────────┐
                    │   Server Bridge    │
                    │  (gRPC Streaming)  │
                    └─────────┬─────────┘
                              │
                    ┌─────────▼─────────┐
                    │   Tapio Server     │
                    └───────────────────┘
```

## Usage

```bash
# Run with default settings
tapio-collector

# Custom configuration
tapio-collector \
  --server localhost:9090 \
  --otel-endpoint localhost:4317 \
  --enable-ebpf \
  --enable-k8s \
  --correlation semantic \
  --buffer-size 2000
```

## Command Line Options

- `--server`: Tapio server address (default: localhost:9090)
- `--otel-endpoint`: OTEL collector endpoint (default: localhost:4317)
- `--enable-ebpf`: Enable eBPF collector (default: true)
- `--enable-k8s`: Enable Kubernetes collector (default: true)
- `--enable-systemd`: Enable SystemD collector (default: true)
- `--enable-journald`: Enable JournalD collector (default: true)
- `--buffer-size`: Event buffer size (default: 1000)
- `--flush-interval`: Event flush interval (default: 1s)
- `--correlation`: Correlation mode - semantic or basic (default: semantic)
- `--grpc-insecure`: Use insecure gRPC connection (default: true)

## Semantic Correlation Features

When running in semantic mode, events are enriched with:

1. **Trace Context**: OTEL trace and span IDs for distributed tracing
2. **Semantic Groups**: Events grouped by meaning and causality
3. **Impact Assessment**: Business impact scores and cascade risk
4. **Predictive Analytics**: Predicted outcomes and prevention actions
5. **Root Cause Analysis**: Causal chains and contributing factors

## Performance

- **Throughput**: 165k+ events/sec
- **Latency**: <1ms for event enrichment
- **Memory**: ~500MB under normal load
- **CPU**: ~500m (0.5 cores)

## Building

```bash
# Build binary
go build -o tapio-collector cmd/tapio-collector/main.go

# Build Docker image
docker build -f cmd/tapio-collector/Dockerfile -t tapio-collector:latest .
```

## Running with Docker

```bash
# Basic run
docker run -d \
  --name tapio-collector \
  --network host \
  --privileged \
  -v /sys:/sys:ro \
  -v /proc:/proc:ro \
  tapio-collector:latest

# With custom configuration
docker run -d \
  --name tapio-collector \
  --network host \
  --privileged \
  -v /sys:/sys:ro \
  -v /proc:/proc:ro \
  tapio-collector:latest \
  --server tapio-server:9090 \
  --correlation semantic
```

## Integration with OTEL

The collector automatically exports traces to the configured OTEL endpoint. Each event is enriched with trace context that can be viewed in:

- Jaeger
- Zipkin
- Grafana Tempo
- Any OTEL-compatible backend

## Health Monitoring

The collector exposes health metrics every 10 seconds:

```
📈 Status: Events=15234, Active Collectors=4, Correlation Groups=127
```

Individual collector health can be monitored through their respective interfaces.

## Troubleshooting

1. **eBPF collector fails to start**: Requires root/CAP_SYS_ADMIN capability
2. **K8s collector fails**: Ensure kubeconfig is available
3. **High memory usage**: Reduce buffer-size or enable fewer collectors
4. **Missing events**: Check collector health and server connectivity

## License

Part of the Tapio observability platform.