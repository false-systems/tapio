# Simplified eBPF Collector

This is a minimal eBPF collector implementation that follows the principle of collectors only collecting raw data with no business logic.

## Architecture

The simplified collector:
- Only collects raw bytes from eBPF programs
- No enrichment, filtering, or semantic processing
- No Kubernetes context extraction
- Minimal metadata (only CPU and event type hint)
- All intelligence moved to the pipeline layer

## Implementation

- `collector_simple.go` - Main collector implementing the minimal Collector interface
- `collector_simple_test.go` - Tests including mock implementation
- Uses only the memory tracker BPF program as an example

## Usage

```go
config := ebpf.DefaultConfig()
collector := ebpf.NewCollector(config)

err := collector.Start(ctx)
if err != nil {
    log.Fatal(err)
}
defer collector.Stop()

for event := range collector.Events() {
    // event.Type = "ebpf"
    // event.Data = raw bytes from kernel
    // event.Metadata = minimal hints (cpu, size, event_type)
}
```

## Migration from Complex Collector

The previous collector had:
- 8 BPF programs (5.8MB total)
- Event enrichment with process, container, K8s context
- Filtering engine with semantic rules
- Dual-path processing (raw + semantic)
- Impact assessment and scoring
- Rate limiting and circuit breakers

All of this logic should be moved to the pipeline service that processes RawEvents into UnifiedEvents.