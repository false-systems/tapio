# Tapio Event Pipeline

## Overview

The pipeline package provides a simple, efficient transformation layer that converts `RawEvent` data from collectors into structured `ObservationEvent` objects for the intelligence layer.

## Architecture

```
Collectors → RawEvent → Pipeline → Parser → ObservationEvent → Intelligence
```

### Key Components

1. **Pipeline**: Core orchestrator with channel-based flow
2. **ParserRegistry**: Manages parsers for different event sources
3. **Parser Interface**: Simple contract for event transformation
4. **Built-in Parsers**: Kernel, DNS, KubeAPI, and Generic parsers

## Usage

### Basic Setup

```go
// Create pipeline
config := &pipeline.Config{
    InputBufferSize:  10000,  // Buffer for incoming events
    OutputBufferSize: 1000,   // Buffer for parsed events
    Workers:          4,      // Number of parser workers
    MetricsEnabled:   true,   // Enable OTEL metrics
}
pipe := pipeline.New(logger, config)

// Register parsers
pipe.RegisterParser(parsers.NewKernelParser())
pipe.RegisterParser(parsers.NewDNSParser())
pipe.RegisterParser(parsers.NewKubeAPIParser())

// Start pipeline
ctx := context.Background()
err := pipe.Start(ctx)
defer pipe.Stop()
```

### Sending Events

```go
// Collectors send raw events
rawEvent := &domain.RawEvent{
    Timestamp: time.Now(),
    Source:    "kernel",      // Must match a registered parser
    Data:      eventBytes,    // Raw event data (JSON, protobuf, etc)
}

pipe.Input() <- rawEvent
```

### Receiving Parsed Events

```go
// Intelligence layer receives structured events
for obs := range pipe.Output() {
    // Process observation event
    fmt.Printf("Event: %s from %s\n", obs.Type, obs.Source)
    
    // Access correlation keys
    if obs.PID != nil {
        fmt.Printf("PID: %d\n", *obs.PID)
    }
    if obs.ContainerID != nil {
        fmt.Printf("Container: %s\n", *obs.ContainerID)
    }
}
```

## Creating Custom Parsers

Implement the `Parser` interface:

```go
type MyParser struct{}

func (p *MyParser) Source() string {
    return "mysource"
}

func (p *MyParser) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
    // Parse raw.Data based on your format
    var myData MyEventStruct
    if err := json.Unmarshal(raw.Data, &myData); err != nil {
        return nil, fmt.Errorf("failed to parse: %w", err)
    }
    
    // Create observation event
    obs := &domain.ObservationEvent{
        ID:        uuid.New().String(),
        Timestamp: raw.Timestamp,
        Source:    p.Source(),
        Type:      myData.EventType,
        // Set correlation keys...
    }
    
    return obs, nil
}
```

## Built-in Parsers

### KernelParser
- Source: `"kernel"`
- Parses: System calls, kernel events from eBPF
- Correlation: PID, ContainerID, Namespace

### DNSParser
- Source: `"dns"`
- Parses: DNS queries and responses
- Correlation: PID, ContainerID, PodName

### KubeAPIParser
- Source: `"kubeapi"`
- Parses: Kubernetes API events
- Correlation: Namespace, PodName, ServiceName, NodeName

### GenericParser
- Source: Configurable
- Parses: Any JSON-structured event
- Fallback: Creates minimal events for unparseable data

## Metrics

When `MetricsEnabled: true`, the pipeline exports:

- `pipeline_events_received_total`: Raw events received
- `pipeline_events_parsed_total`: Successfully parsed events
- `pipeline_parse_errors_total`: Parse failures
- `pipeline_parse_duration_ms`: Parse latency
- `pipeline_input_queue_size`: Current input buffer usage
- `pipeline_output_queue_size`: Current output buffer usage

## Design Principles

1. **Simple**: Channel-based, no callbacks or complex interfaces
2. **Efficient**: Minimal allocations, concurrent parsing
3. **Resilient**: Graceful degradation, error isolation
4. **Observable**: Built-in metrics and tracing
5. **Extensible**: Easy to add new parsers

## Performance

- Benchmarked at ~100k events/second on 4 cores
- Sub-millisecond parse latency for most events
- Memory-efficient with bounded buffers
- Backpressure handling via channel buffers

## Testing

```bash
# Run tests
go test ./pkg/pipeline/...

# With coverage
go test ./pkg/pipeline/... -cover

# Benchmarks
go test ./pkg/pipeline -bench=.
```