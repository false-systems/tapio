# pkg/interfaces/ Layer Implementation Plan (Level 4)

## Overview

The interfaces layer (Level 4) provides user-facing interfaces and system configuration. It can depend on ALL lower layers:
- Level 0: `pkg/domain/` (core types)
- Level 1: `pkg/collectors/` (data sources)
- Level 2: `pkg/intelligence/` (processing)
- Level 3: `pkg/integrations/` (external systems)

## Proposed Structure

```
pkg/interfaces/
├── go.mod                    # Independent module
├── README.md                 # Interface guidelines
├── core/                     # Shared interface utilities
│   ├── interfaces.go         # Common interface contracts
│   ├── types.go             # Shared types
│   └── errors.go            # Interface-specific errors
│
├── cli/                      # Command-Line Interface
│   ├── go.mod               # Independent module
│   ├── cmd/                 # Command implementations
│   │   ├── root.go          # Root command
│   │   ├── check.go         # Health check command
│   │   ├── collect.go       # Data collection commands
│   │   ├── analyze.go       # Analysis commands
│   │   └── config.go        # Configuration commands
│   ├── output/              # Output formatters
│   │   ├── table.go         # Table output
│   │   ├── json.go          # JSON output
│   │   └── human.go         # Human-readable output
│   ├── interactive/         # Interactive mode
│   └── README.md
│
├── server/                   # Server Interface (HTTP/gRPC)
│   ├── go.mod               # Independent module
│   ├── http/                # HTTP API
│   │   ├── router.go        # API router
│   │   ├── handlers/        # Request handlers
│   │   ├── middleware/      # HTTP middleware
│   │   └── openapi/         # OpenAPI spec
│   ├── grpc/                # gRPC API
│   │   ├── server.go        # gRPC server
│   │   ├── services/        # Service implementations
│   │   └── interceptors/    # gRPC interceptors
│   ├── websocket/           # WebSocket support
│   └── README.md
│
├── gui/                      # Web GUI Interface
│   ├── go.mod               # Independent module
│   ├── backend/             # GUI backend
│   │   ├── api/             # GUI-specific API
│   │   ├── auth/            # Authentication
│   │   └── sessions/        # Session management
│   ├── frontend/            # Frontend assets
│   │   ├── src/             # Source code
│   │   ├── public/          # Static assets
│   │   └── build/           # Built assets
│   └── README.md
│
├── config/                   # Configuration Management
│   ├── go.mod               # Independent module
│   ├── loader/              # Config loading
│   │   ├── file.go          # File-based config
│   │   ├── env.go           # Environment config
│   │   └── flags.go         # Command-line flags
│   ├── validator/           # Config validation
│   ├── templates/           # Config templates
│   └── README.md
│
└── output/                   # Output Formatting (moved from humanoutput)
    ├── go.mod               # Independent module
    ├── formatters/          # Output formatters
    │   ├── table.go         # Table formatting
    │   ├── json.go          # JSON formatting
    │   ├── yaml.go          # YAML formatting
    │   └── human.go         # Human-readable formatting
    ├── renderers/           # Complex rendering
    │   ├── correlation.go   # Correlation visualization
    │   ├── timeline.go      # Timeline rendering
    │   └── graph.go         # Graph visualization
    └── README.md
```

## Interface Patterns

### Core Interface Contract

```go
// pkg/interfaces/core/interfaces.go
package core

import (
    "context"
    "github.com/falseyair/tapio/pkg/domain"
)

// Interface defines the base contract for all user interfaces
type Interface interface {
    // Name returns the interface identifier
    Name() string
    
    // Initialize sets up the interface
    Initialize(ctx context.Context, config Config) error
    
    // Start begins serving the interface
    Start(ctx context.Context) error
    
    // Stop gracefully shuts down the interface
    Stop(ctx context.Context) error
    
    // Health checks the interface status
    Health(ctx context.Context) (*HealthStatus, error)
}

// OutputFormatter formats data for display
type OutputFormatter interface {
    // Format formats the data according to the implementation
    Format(data interface{}) ([]byte, error)
    
    // SupportsStreaming indicates if streaming is supported
    SupportsStreaming() bool
}
```

### CLI Interface Example

```go
// pkg/interfaces/cli/cmd/root.go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/falseyair/tapio/pkg/interfaces/cli/output"
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
    Use:   "tapio",
    Short: "Tapio - Advanced System Intelligence Platform",
    Long: `Tapio provides predictive analytics, correlation analysis, 
and automated incident response for distributed systems.`,
}

// Execute runs the CLI
func Execute() error {
    return rootCmd.Execute()
}

// Output formatting example
func formatOutput(format string, data interface{}) error {
    formatter := output.GetFormatter(format)
    return formatter.Format(os.Stdout, data)
}
```

### Server Interface Example

```go
// pkg/interfaces/server/http/router.go
package http

import (
    "github.com/gin-gonic/gin"
    "github.com/falseyair/tapio/pkg/domain"
    "github.com/falseyair/tapio/pkg/collectors"
    "github.com/falseyair/tapio/pkg/intelligence"
)

// Router configures HTTP routes
type Router struct {
    collectors   map[string]collectors.Collector
    intelligence intelligence.Engine
}

// SetupRoutes configures all HTTP routes
func (r *Router) SetupRoutes(engine *gin.Engine) {
    api := engine.Group("/api/v1")
    {
        // Health endpoints
        api.GET("/health", r.healthHandler)
        api.GET("/ready", r.readyHandler)
        
        // Collector endpoints
        api.GET("/collectors", r.listCollectors)
        api.GET("/collectors/:name/status", r.collectorStatus)
        
        // Events endpoints
        api.GET("/events", r.listEvents)
        api.GET("/events/:id", r.getEvent)
        
        // Correlation endpoints
        api.GET("/correlations", r.listCorrelations)
        api.POST("/correlations/analyze", r.analyzeEvents)
    }
}
```

## Implementation Guidelines

### 1. Dependency Rules

- ✅ CAN import: ALL lower layers (domain, collectors, intelligence, integrations)
- ❌ CANNOT import: other interfaces at the same level
- ✅ CAN use external UI libraries (cobra, gin, etc.)

### 2. Each Interface Must

- Have its own `go.mod` file
- Provide user-friendly error messages
- Support multiple output formats where applicable
- Include comprehensive help/documentation
- Support configuration via files/env/flags
- Have integration tests

### 3. CLI Design Principles

```bash
# Intuitive command structure
tapio check health                    # Check system health
tapio collect events --source=ebpf    # Collect from specific source
tapio analyze correlations --last=1h  # Analyze recent events
tapio config validate                 # Validate configuration

# Consistent output formatting
tapio events list --output=json
tapio events list --output=table
tapio events list --output=human
```

### 4. Server API Principles

- RESTful design for HTTP endpoints
- gRPC for high-performance streaming
- WebSocket for real-time updates
- OpenAPI documentation
- Consistent error responses
- Rate limiting and authentication

### 5. Configuration Management

```yaml
# Example tapio.yaml
collectors:
  ebpf:
    enabled: true
    sampling_rate: 100
  kubernetes:
    enabled: true
    namespaces: ["default", "monitoring"]

intelligence:
  correlation:
    window: 5m
    min_events: 3
  prediction:
    enabled: true
    models: ["oom", "cascade"]

integrations:
  prometheus:
    enabled: true
    port: 9090
  otel:
    enabled: true
    endpoint: "localhost:4317"

interfaces:
  cli:
    output: table
    color: true
  server:
    http:
      port: 8080
      cors: true
    grpc:
      port: 9090
```

## Migration Plan

1. **Move existing interfaces**
   - `pkg/server/` → `pkg/interfaces/server/`
   - `pkg/api/` → `pkg/interfaces/server/http/`
   - `pkg/humanoutput/` → `pkg/interfaces/output/`

2. **Create new interfaces**
   - CLI with cobra for command-line usage
   - Configuration management system
   - Web GUI (future enhancement)

3. **Standardize patterns**
   - Consistent error handling
   - Unified output formatting
   - Common configuration approach

## Testing Strategy

- Unit tests for individual handlers/commands
- Integration tests for full interface flows
- E2E tests for user scenarios
- Performance tests for API endpoints
- Usability tests for CLI commands

## Success Criteria

- ✅ Consistent user experience across interfaces
- ✅ Clear separation between interface and business logic
- ✅ All interfaces can be developed independently
- ✅ Comprehensive documentation and examples
- ✅ Easy to add new interfaces or extend existing ones