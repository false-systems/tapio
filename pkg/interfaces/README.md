# Interfaces Layer (Level 4)

This layer provides user-facing interfaces and system configuration. It represents Level 4 (the highest level) in Tapio's architecture hierarchy.

## Architecture Position

```
Level 0: pkg/domain/          # ✓ Can import
Level 1: pkg/collectors/      # ✓ Can import  
Level 2: pkg/intelligence/    # ✓ Can import
Level 3: pkg/integrations/    # ✓ Can import
Level 4: pkg/interfaces/      # 📍 You are here (top level)
```

## Available Interfaces

### 🖥️ CLI (Command Line Interface)
**Location**: `pkg/interfaces/cli/`  
**Status**: In Development  
**Purpose**: Command-line interface for Tapio operations
**Features**:
- System health checks
- Event collection commands
- Analysis and correlation queries
- Configuration management
- Multiple output formats

### 🌐 Server
**Location**: `pkg/interfaces/server/`  
**Status**: Planned  
**Purpose**: HTTP/gRPC API server
**Features**:
- RESTful API endpoints
- gRPC service definitions
- WebSocket support for real-time data
- OpenAPI documentation
- Authentication and authorization

### 📊 Output Formatting
**Location**: `pkg/interfaces/output/`  
**Status**: Planned (move from pkg/humanoutput)  
**Purpose**: Format data for different consumers
**Features**:
- JSON/YAML formatting
- Table rendering
- Human-readable descriptions
- Prometheus exposition format
- Custom formatters

### ⚙️ Configuration Management
**Location**: `pkg/interfaces/config/`  
**Status**: Planned  
**Purpose**: Unified configuration system
**Features**:
- File-based configuration (YAML/JSON)
- Environment variable support
- Command-line flag parsing
- Configuration validation
- Hot-reload support

## Interface Pattern

All interfaces follow a consistent pattern:

```go
// 1. Configuration
config := &CLIConfig{
    OutputFormat: OutputFormatTable,
    Verbose: true,
}

// 2. Initialization
cli, err := NewCLI(ctx, config)
if err != nil {
    return err
}

// 3. Start
err = cli.Start(ctx)

// 4. Health monitoring
health, err := cli.Health(ctx)

// 5. Graceful shutdown
defer cli.Stop(ctx)
```

## Development Guidelines

1. **Dependencies**: Can import from ALL lower levels (0-3)
2. **User Experience**: Focus on usability and clarity
3. **Error Handling**: User-friendly error messages
4. **Documentation**: Comprehensive help and examples
5. **Testing**: Integration tests with real components

## Migration Plan

### To Move Here:
- `pkg/humanoutput/` → `pkg/interfaces/output/`
- `pkg/server/` → `pkg/interfaces/server/`
- `pkg/api/` → `pkg/interfaces/server/http/`
- CLI interface → Use `pkg/interfaces/cli/`

### New Implementations:
- Unified CLI with cobra
- Configuration management system
- Output formatting framework
- API gateway functionality

## Usage Examples

### CLI Example
```bash
# Check system health
tapio check --all

# Collect events
tapio collect --source=ebpf --output=json

# Analyze correlations
tapio analyze correlations --last=1h --output=human
```

### Server Example
```go
server := NewServer(config)
server.RegisterCollectors(collectors)
server.RegisterIntelligence(intelligence)
server.RegisterIntegrations(integrations)
server.ListenAndServe()
```

## Success Metrics

- ✅ Consistent user experience across interfaces
- ✅ Clear separation from business logic
- ✅ All interfaces can access all lower layers
- ✅ Comprehensive documentation and help
- ✅ Easy to extend with new interfaces