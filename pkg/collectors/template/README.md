# Template Collector

This is the STANDARD template that ALL collectors must follow.

## Structure

Every collector MUST have these files:
- `collector.go` - Main implementation (~500 lines max)
- `collector_test.go` - Unit tests (80%+ coverage)
- `types.go` - Event types and structures
- `config.go` - Configuration with validation
- `README.md` - Documentation
- `testdata/` - Test fixtures

## Required Methods

```go
// Collector interface (from pkg/collectors/interface.go)
type Collector interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
    Events() <-chan RawEvent
    IsHealthy() bool
}
```

## Enhanced Metadata

ALL collectors MUST extract K8s metadata when available:

```go
metadata["k8s_namespace"] = "default"
metadata["k8s_name"] = "nginx-7c4ff8b6d5-xyz"
metadata["k8s_kind"] = "Pod"
metadata["k8s_uid"] = "abc-123"
metadata["k8s_labels"] = "app=nginx,version=1.2"
metadata["k8s_owner_refs"] = "ReplicaSet/nginx-7c4ff8b6d5"
```

## Testing Requirements

1. Unit tests with 80%+ coverage
2. Test all error cases
3. Test configuration validation
4. Test event generation
5. Test health checks
6. Test graceful shutdown

## Example Usage

```go
// Create collector
config := template.DefaultConfig()
collector, err := template.New("my-collector", config)

// Start collection
ctx := context.Background()
err = collector.Start(ctx)

// Process events
for event := range collector.Events() {
    // event.Metadata contains K8s context
    fmt.Printf("Event from %s/%s\n", 
        event.Metadata["k8s_namespace"],
        event.Metadata["k8s_name"])
}

// Stop collection
err = collector.Stop()
```

## Performance Targets

- Event rate: 10K+ events/sec
- Memory usage: <100MB
- CPU usage: <5%
- Startup time: <1 second
- Shutdown time: <5 seconds