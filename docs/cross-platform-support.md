# Cross-Platform Support for Tapio

## Overview

Tapio now supports cross-platform development and deployment, allowing developers to work on macOS and Windows while maintaining full Linux functionality for production environments.

## Problem Solved

**Issue**: eBPF Platform Limitations - Linux-Only
- **Problem**: Build constraints too restrictive
- **Impact**: Limited adoption on macOS/Windows dev environments
- **Solution**: Implemented cross-platform compatibility layer with graceful degradation

## Architecture

### Platform Detection

The system automatically detects the current platform and available features:

```go
platform := collectors.GetCurrentPlatform()
fmt.Printf("OS: %s\n", platform.OS)
fmt.Printf("eBPF Support: %v\n", platform.HasEBPF)
fmt.Printf("Journald Support: %v\n", platform.HasJournald)
```

### Graceful Degradation

- **Linux**: Full native eBPF and journald support
- **macOS/Windows**: Stub implementations with mock data for development

## Implementation Details

### 1. Platform Abstraction Layer

**File**: `pkg/collectors/platform.go`

```go
type Platform struct {
    OS           string
    Architecture string
    HasEBPF      bool
    HasJournald  bool
    HasSystemd   bool
}
```

### 2. Cross-Platform eBPF Adapter

**File**: `pkg/collectors/ebpf_adapter.go`

- Detects platform capabilities at runtime
- Provides unified interface across platforms
- Generates mock events for development on non-Linux systems

### 3. Build System Enhancements

**File**: `build/Makefile.platform`

```bash
# Build for specific platforms
make build-linux    # Full eBPF support
make build-darwin   # Stub implementations
make build-windows  # Stub implementations
make build-all      # All platforms
```

### 4. Conditional Compilation

**Build Tags**:
- `linux`: Linux-specific implementations
- `ebpf`: eBPF functionality
- `journald`: Journald log collection
- `darwin`: macOS-specific code
- `windows`: Windows-specific code

## Usage Examples

### Cross-Platform Development

```go
// Works on all platforms
adapter, err := collectors.NewEBPFAdapter()
if err != nil {
    log.Fatal(err)
}

// Platform-aware behavior
if adapter.IsAvailable() {
    // Native eBPF on Linux
    log.Info("Using native eBPF monitoring")
} else {
    // Stub implementation on macOS/Windows
    log.Info("Using stub implementation for development")
}
```

### Platform-Specific Features

```go
// Check what collectors are supported
supported := collectors.GetSupportedCollectors()
for _, collector := range supported {
    fmt.Printf("Supported: %s\n", collector)
}

// Get platform-specific messages
message := collectors.GetPlatformMessage("ebpf")
fmt.Printf("eBPF: %s\n", message)
```

## Build Instructions

### Development Build (Current Platform)

```bash
# Automatically detects and builds for current platform
make dev-build
make dev-test
make dev-run
```

### Cross-Platform Builds

```bash
# Build for all platforms
make build-all

# Platform-specific builds
make build-linux      # Production Linux build
make build-darwin     # macOS development build
make build-windows    # Windows development build
```

### Testing

```bash
# Test current platform
make dev-test

# Test platform-specific features
make test-linux       # Linux native features
make test-darwin      # macOS stub implementations
make test-windows     # Windows stub implementations
```

## Platform-Specific Behavior

### Linux (Production)

- **eBPF**: Full native support with kernel-level monitoring
- **Journald**: Real systemd journal log collection
- **Performance**: Optimized for production workloads
- **Features**: All collectors and monitoring capabilities

### macOS/Windows (Development)

- **eBPF**: Stub implementation with mock data
- **Journald**: Mock log events for testing
- **Performance**: Lightweight for development
- **Features**: UI testing, API development, basic functionality

## Configuration

### Platform-Aware Configuration

```yaml
collectors:
  - name: ebpf
    type: ebpf
    enabled: true
    config:
      # These settings work on all platforms
      # Linux uses actual eBPF, others use stubs
      sampling_rate: 1.0
      buffer_size: 65536
```

### Development vs Production

```yaml
# Development (macOS/Windows)
collectors:
  - name: mock-ebpf
    type: ebpf
    enabled: true
    config:
      mock_events: true
      event_interval: 30s

# Production (Linux)
collectors:
  - name: production-ebpf
    type: ebpf
    enabled: true
    config:
      enable_memory_monitoring: true
      enable_network_monitoring: true
```

## Performance Characteristics

### Linux (Native)

- **Memory**: 30-100MB depending on collectors
- **CPU**: 1-5% system overhead
- **Features**: Full eBPF monitoring, real-time events

### macOS/Windows (Stub)

- **Memory**: <10MB
- **CPU**: <0.1% system overhead
- **Features**: Mock events, API compatibility

## Troubleshooting

### Common Issues

1. **Build Errors on macOS/Windows**
   - Ensure you're using the correct build tags
   - Run `make platform-info` to check feature support

2. **Missing Events in Development**
   - Normal behavior on non-Linux platforms
   - Mock events are generated every 30 seconds

3. **Performance Differences**
   - Expected between platforms
   - Use Linux for production benchmarking

### Debug Commands

```bash
# Check platform information
make platform-info

# Test cross-platform functionality
go run test_simple_cross_platform.go

# Verify build tags
go build -tags "darwin,unix" -o tapio-dev ./cmd/tapio
```

## Future Enhancements

### Planned Features

- [ ] Windows performance counters integration
- [ ] macOS system events integration
- [ ] Enhanced mock data generation
- [ ] Cross-platform benchmarking

### Integration Points

- [ ] Docker Desktop support on macOS/Windows
- [ ] Kubernetes-in-Docker (KinD) integration
- [ ] Cloud development environment support

## Best Practices

### For Developers

1. **Test on Linux**: Always validate production features on Linux
2. **Use Stubs Wisely**: Leverage mock data for UI/API development
3. **Platform Detection**: Always check platform capabilities
4. **Build Tags**: Use appropriate build tags for platform-specific code

### For Production

1. **Linux Only**: Deploy only on Linux systems
2. **Feature Validation**: Verify all required features are available
3. **Performance Testing**: Use Linux for all performance benchmarks
4. **Monitoring**: Enable full eBPF monitoring in production

## Contributing

When adding new platform-specific features:

1. Create platform-agnostic interfaces
2. Implement platform-specific versions with build tags
3. Provide stub implementations for non-supported platforms
4. Add comprehensive tests for all platforms
5. Update documentation and build system

## Example: Adding a New Collector

```go
// collector_interface.go (no build tags)
type MyCollector interface {
    Collect() ([]Event, error)
    IsSupported() bool
}

// collector_linux.go
//go:build linux
package collectors
func NewMyCollector() MyCollector {
    return &linuxCollector{}
}

// collector_stub.go  
//go:build !linux
package collectors
func NewMyCollector() MyCollector {
    return &stubCollector{}
}
```

This approach ensures that Tapio remains accessible to developers on all platforms while maintaining its powerful Linux-native capabilities for production use.