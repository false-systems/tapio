# pkg/integrations/ Layer Implementation Plan (Level 3)

## Overview

The integrations layer (Level 3) provides connectivity to external systems and protocols. It depends on:
- Level 0: `pkg/domain/` (core types)
- Level 1: `pkg/collectors/` (data sources)
- Level 2: `pkg/intelligence/` (processing)

## Proposed Structure

```
pkg/integrations/
├── go.mod                    # Independent module
├── README.md                 # Integration guidelines
├── core/                     # Shared integration interfaces
│   ├── interfaces.go         # Common integration contracts
│   ├── types.go             # Shared types for integrations
│   └── errors.go            # Integration-specific errors
│
├── otel/                     # OpenTelemetry Integration
│   ├── go.mod               # Independent module
│   ├── core/
│   │   ├── interfaces.go    # OTEL-specific interfaces
│   │   ├── types.go         # OTEL types
│   │   └── errors.go        # OTEL errors
│   ├── exporter/            # Trace/metric exporters
│   ├── processor/           # Event to OTEL conversion
│   ├── enricher/            # Semantic enrichment
│   └── README.md
│
├── prometheus/               # Prometheus Integration
│   ├── go.mod               # Independent module
│   ├── core/
│   │   ├── interfaces.go    # Prometheus interfaces
│   │   ├── types.go         # Metric types
│   │   └── errors.go        # Prometheus errors
│   ├── exporter/            # Metric exporters
│   ├── collector/           # Custom collectors
│   ├── registry/            # Metric registry
│   └── README.md
│
├── grpc/                     # gRPC Integration
│   ├── go.mod               # Independent module
│   ├── core/
│   │   ├── interfaces.go    # gRPC service interfaces
│   │   ├── types.go         # Message types
│   │   └── errors.go        # gRPC errors
│   ├── proto/               # Protocol buffer definitions
│   ├── server/              # gRPC server implementation
│   ├── client/              # gRPC client utilities
│   └── README.md
│
└── webhooks/                 # Webhook Integration
    ├── go.mod               # Independent module
    ├── core/
    │   ├── interfaces.go    # Webhook interfaces
    │   ├── types.go         # Webhook payload types
    │   └── errors.go        # Webhook errors
    ├── sender/              # Webhook dispatch
    ├── receiver/            # Webhook reception
    ├── signing/             # Webhook security
    └── README.md
```

## Integration Interfaces

### Core Integration Contract

```go
// pkg/integrations/core/interfaces.go
package core

import (
    "context"
    "github.com/falseyair/tapio/pkg/domain"
)

// Integration defines the base contract for all integrations
type Integration interface {
    // Name returns the integration identifier
    Name() string
    
    // Initialize sets up the integration
    Initialize(ctx context.Context, config Config) error
    
    // ProcessEvent handles incoming events from collectors/intelligence
    ProcessEvent(ctx context.Context, event *domain.Event) error
    
    // ProcessFinding handles findings from intelligence layer
    ProcessFinding(ctx context.Context, finding *domain.Finding) error
    
    // Health checks the integration status
    Health(ctx context.Context) (*HealthStatus, error)
    
    // Close cleanly shuts down the integration
    Close() error
}

// Config provides integration configuration
type Config interface {
    Validate() error
}

// HealthStatus represents integration health
type HealthStatus struct {
    Healthy bool
    Message string
    Details map[string]interface{}
}
```

### OTEL Integration Example

```go
// pkg/integrations/otel/core/interfaces.go
package core

import (
    "context"
    "go.opentelemetry.io/otel/trace"
    "github.com/falseyair/tapio/pkg/domain"
)

// OTELExporter exports Tapio events as OTEL traces
type OTELExporter interface {
    // ExportEvent converts and exports an event as a span
    ExportEvent(ctx context.Context, event *domain.Event) error
    
    // ExportCorrelation exports correlated events as linked spans
    ExportCorrelation(ctx context.Context, correlation *domain.Correlation) error
    
    // CreateTracer returns a configured tracer
    CreateTracer(name string) trace.Tracer
}

// SemanticEnricher adds semantic context to traces
type SemanticEnricher interface {
    // EnrichSpan adds semantic attributes to a span
    EnrichSpan(span trace.Span, event *domain.Event) error
    
    // AddCorrelationLinks adds correlation information
    AddCorrelationLinks(span trace.Span, correlation *domain.Correlation) error
}
```

## Implementation Guidelines

### 1. Dependency Rules

- ✅ CAN import: `pkg/domain/`, `pkg/collectors/`, `pkg/intelligence/`
- ❌ CANNOT import: `pkg/interfaces/`, other integrations
- ❌ CANNOT import: external packages beyond what's needed

### 2. Each Integration Must

- Have its own `go.mod` file
- Implement the base `Integration` interface
- Provide comprehensive error handling
- Include health checking
- Support graceful shutdown
- Have 80%+ test coverage

### 3. Configuration Pattern

```go
// Each integration has typed configuration
type PrometheusConfig struct {
    Address         string        `yaml:"address" validate:"required"`
    Port            int          `yaml:"port" validate:"min=1,max=65535"`
    MetricsPath     string       `yaml:"metrics_path"`
    ScrapeInterval  time.Duration `yaml:"scrape_interval"`
}

func (c PrometheusConfig) Validate() error {
    // Validation logic
}
```

### 4. Error Handling

```go
// Integration-specific errors
type IntegrationError struct {
    Integration string
    Operation   string
    Err         error
}

func (e IntegrationError) Error() string {
    return fmt.Sprintf("%s integration failed during %s: %v", 
        e.Integration, e.Operation, e.Err)
}
```

## Migration Plan

1. **Move existing OTEL code**
   - Current: `pkg/otel/`
   - Target: `pkg/integrations/otel/`
   - Create proper interfaces and structure

2. **Create new integrations**
   - Prometheus exporter for metrics
   - gRPC server for external communication
   - Webhook sender for alerts/notifications

3. **Ensure isolation**
   - Each integration gets its own go.mod
   - No cross-dependencies between integrations
   - Communication only through defined interfaces

## Testing Strategy

- Unit tests for each integration component
- Integration tests with mock external systems
- Contract tests for interface compliance
- Performance benchmarks for high-throughput scenarios

## Success Criteria

- ✅ All integrations follow the same pattern
- ✅ Clear interfaces between layers
- ✅ No circular dependencies
- ✅ Each integration can be developed/deployed independently
- ✅ Comprehensive documentation and examples