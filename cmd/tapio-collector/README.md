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

## gRPC Integration

### eBPF gRPC Connection

The eBPF collector features built-in gRPC streaming to the Tapio server for real-time event correlation and analysis. This connection was previously disabled but has been restored.

#### Connection Architecture

```
┌─────────────────┐    ┌──────────────────────┐    ┌─────────────────┐
│ eBPF Collector  │    │ EBPFCollectorAdapter │    │  Tapio Server   │
│                 │    │                      │    │                 │
│ Kernel Events   ├───►│ Dual-Path Processor  ├───►│ gRPC Streaming  │
│ (domain.Event)  │    │ - Raw Path           │    │ - Correlation   │
│                 │    │ - Semantic Path      │    │ - Intelligence  │
└─────────────────┘    │ - gRPC Client        │    │ - OTEL Traces   │
                       └──────────────────────┘    └─────────────────┘
```

#### Features

1. **Dual-Path Processing**:
   - **Raw Path**: Disabled in production for performance
   - **Semantic Path**: Enabled with intelligent event filtering
   - **gRPC Streaming**: Bidirectional streaming to Tapio server

2. **Connection Configuration**:
   ```bash
   tapio-collector --server localhost:9090 --enable-ebpf
   ```

3. **Automatic Reconnection**: Built-in connection management with retry logic

4. **Event Enrichment**: Events are enriched before streaming:
   - Process context (PID, UID, GID, Comm)
   - Container information 
   - Kubernetes metadata
   - Network context
   - Severity scoring

#### Configuration Details

The adapter configures the dual-path processor with:

```go
processorConfig := &ebpf.ProcessorConfig{
    RawBufferSize:      10000,
    SemanticBufferSize: 5000,
    WorkerCount:        4,
    BatchSize:          100,
    FlushInterval:      time.Second,
    EnableRawPath:      false, // Disabled for production
    EnableSemanticPath: true,  // Enable semantic correlation
    TapioServerAddr:    serverAddress, // gRPC server connection
    SemanticBatchSize:  50,
    MaxMemoryUsage:     512 * 1024 * 1024, // 512MB
    MetricsInterval:    30 * time.Second,
}
```

#### Event Flow

1. **Collection**: eBPF collector gathers kernel events
2. **Conversion**: domain.Event → RawEvent for processing
3. **Processing**: Dual-path processor applies filtering and enrichment
4. **Streaming**: TapioGRPCClient sends events via bidirectional gRPC
5. **Correlation**: Tapio server processes events for semantic correlation

#### Verification

To verify the gRPC connection is working:

```bash
# Start Tapio server
tapio-server --grpc-port 9090

# Start collector with eBPF enabled
tapio-collector --server localhost:9090 --enable-ebpf

# Look for success messages
✅ eBPF collector enabled with gRPC connection to localhost:9090
```

#### Connection Status

The eBPF collector connection status:
- ✅ **Enabled**: gRPC streaming active
- ✅ **Tested**: Connection verified in development
- ✅ **Production Ready**: Resource limits and error handling configured

## Troubleshooting

1. **eBPF collector fails to start**: Requires root/CAP_SYS_ADMIN capability
2. **K8s collector fails**: Ensure kubeconfig is available
3. **High memory usage**: Reduce buffer-size or enable fewer collectors
4. **Missing events**: Check collector health and server connectivity
5. **gRPC connection issues**: 
   - Verify Tapio server is running on specified port
   - Check network connectivity between collector and server
   - Ensure gRPC port (default 9090) is accessible
6. **eBPF gRPC adapter errors**: Check logs for processor or client initialization failures

## License

Part of the Tapio observability platform.