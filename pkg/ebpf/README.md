# eBPF Package Structure

## Component Responsibilities

### Collectors (Data Collection Layer)
**Primary Responsibility**: Load and manage eBPF programs, handle kernel interaction

- `collector.go` - Basic memory monitoring
- `enhanced_collector.go` - Multi-program orchestration
- Focus: eBPF program lifecycle, ring buffer management

### Events (Data Model Layer)
**Primary Responsibility**: Define structured event types

- `types.go` - Core event structures
- `event_types.go` - Event categorization and constants
- Focus: Type definitions, no business logic

### Parsers (Data Transformation Layer)
**Primary Responsibility**: Convert raw bytes to typed events

- `event_parsers.go` - Parser interfaces and registry
- `event_parsers_impl.go` - Concrete parser implementations
- `parser_errors.go` - Detailed error handling
- Focus: Binary data parsing, validation

### Managers (Orchestration Layer)
**Primary Responsibility**: Coordinate collectors and handle lifecycle

- `manager.go` - Multi-collector coordination
- `ring_buffer_manager.go` - Efficient buffer management
- Focus: High-level operations, resource management

### Utilities (Support Layer)
**Primary Responsibility**: Cross-cutting concerns

- `error_handler.go` - Error handling and recovery
- `utils.go` - Common utilities
- Focus: Reusable functionality

## Refactoring Guidelines

1. **Keep collectors focused on eBPF operations**
   - Move event processing logic to separate handlers
   - Collectors should only emit raw events

2. **Standardize event types**
   - Use consistent naming (NetworkEvent, not NetEvent)
   - Clearly separate full events from simple events

3. **Centralize parsing logic**
   - All binary parsing in parser implementations
   - Parsers should not have side effects

4. **Clear interfaces between layers**
   - Collectors produce events
   - Parsers transform data
   - Managers coordinate operations

## Migration Path

To reduce overlapping responsibilities:

1. Extract event processing from collectors to dedicated processors
2. Move statistics calculation to a separate analytics component
3. Create clear boundaries between data collection and analysis
4. Use dependency injection for better testability