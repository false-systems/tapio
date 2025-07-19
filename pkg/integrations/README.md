# Integrations Layer (Level 3)

This layer provides connectivity to external systems and protocols. It represents Level 3 in Tapio's architecture hierarchy.

## Architecture Position

```
Level 0: pkg/domain/          # ✓ Can import
Level 1: pkg/collectors/      # ✓ Can import  
Level 2: pkg/intelligence/    # ✓ Can import
Level 3: pkg/integrations/    # 📍 You are here
Level 4: pkg/interfaces/      # ❌ Cannot import
```

## Available Integrations

### 🚀 OpenTelemetry (OTEL)
**Status**: In Development  
**Features**:
- Semantic trace correlation (revolutionary!)
- Predictive OTEL metrics (first of its kind!)
- Human-readable context generation
- Full distributed tracing support

### 📊 Prometheus
**Status**: Planned  
**Features**:
- Metric exposition
- Custom collectors for Tapio events
- Predictive metrics export
- Alert rule generation

### 🔌 gRPC
**Status**: Planned  
**Features**:
- High-performance event streaming
- Bi-directional communication
- Service mesh integration
- Load balancing support

### 🪝 Webhooks
**Status**: Planned  
**Features**:
- Event notifications
- Configurable payloads
- Retry logic
- Signature verification

## Integration Pattern

All integrations follow a consistent pattern:

```go
// 1. Configuration
config := &OTELConfig{
    Endpoint: "localhost:4317",
    // ... other settings
}

// 2. Initialization
integration, err := NewOTELIntegration(ctx, config)
if err != nil {
    return err
}

// 3. Registration
registry.Register(integration)

// 4. Usage
err = integration.ProcessEvent(ctx, event)
err = integration.ProcessFinding(ctx, finding)
err = integration.ProcessCorrelation(ctx, correlation)

// 5. Health monitoring
health, err := integration.Health(ctx)

// 6. Cleanup
defer integration.Close()
```

## Development Guidelines

1. **Dependencies**: Can only import from Level 0, 1, and 2
2. **Isolation**: Each integration has its own go.mod
3. **Testing**: Minimum 80% coverage required
4. **Error Handling**: Use wrapped errors with context
5. **Configuration**: Strongly typed with validation

## Adding New Integrations

1. Create directory: `pkg/integrations/<name>/`
2. Initialize module: `go mod init github.com/falseyair/tapio/pkg/integrations/<name>`
3. Implement `core.Integration` interface
4. Add to go.work file
5. Document in README
6. Add comprehensive tests

## Success Metrics

- ✅ Clean separation from other layers
- ✅ No circular dependencies
- ✅ Each integration builds independently
- ✅ Consistent interface across all integrations
- ✅ Comprehensive error handling