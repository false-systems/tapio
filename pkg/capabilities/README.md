# Capability-Based Architecture: Zero Stubs, Maximum Clarity

This package implements Tapio's **stub-free architecture** that eliminates technical debt while providing clear, maintainable cross-platform support.

## Problem Solved

**Before:** 8+ stub files with 220+ lines of fake implementations
- `pkg/ebpf/monitor_stub.go` (140 lines of fake memory stats)
- `pkg/ebpf/enhanced_collector_stub.go` (77 lines of fake events)  
- `pkg/collectors/journald/collector_stub.go` (228 lines of mock logs)
- Multiple other stub files creating maintenance overhead

**After:** Zero stub files, clear capability reporting
- Real implementations only
- Clear error messages when capabilities unavailable
- No fake data anywhere in the system
- Plugin-based architecture for clean separation

## Architecture Overview

```
pkg/capabilities/
├── interface.go          # Core capability interfaces
├── registry.go          # Plugin registration system  
├── manager.go           # High-level capability management
├── detector.go          # Runtime platform detection
├── init.go             # Auto-registration of platform plugins
└── plugins/
    ├── ebpf_memory_linux.go     # Real eBPF implementation (Linux only)
    ├── native_memory.go         # Cross-platform memory monitoring
    ├── build_linux.go           # Linux build optimization
    └── build_nonlinux.go        # Non-Linux build optimization
```

## Key Principles

### 1. No Fake Implementations
```go
// OLD (stub approach):
func (s *StubMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
    return []ProcessMemoryStats{
        {PID: 1, Command: "mock-process", CurrentUsage: 1024*1024*5}, // FAKE DATA
    }, nil
}

// NEW (capability approach):  
func (p *NativeMemoryPlugin) GetMemoryStats() ([]ProcessMemoryStats, error) {
    return nil, capabilities.NewCapabilityError(
        "memory-stats",
        "requires eBPF monitoring (Linux only with kernel-level access)",
        runtime.GOOS,
    )
}
```

### 2. Clear Error Reporting
```go
// Request memory monitoring
memCap, err := capabilities.RequestMemoryMonitoring()
if err != nil {
    if capabilities.IsCapabilityError(err) {
        capErr := err.(*capabilities.CapabilityError)
        log.Printf("Memory monitoring unavailable: %s (platform: %s)", 
            capErr.Reason, capErr.Platform)
        // Show user what IS available
        return
    }
}
// If we get here, we have REAL memory monitoring
```

### 3. Plugin-Based Architecture
```go
// Capabilities register themselves based on platform
func init() {
    if runtime.GOOS == "linux" {
        capabilities.Register(NewEBPFMemoryPlugin(nil))  // Real eBPF
    }
    capabilities.Register(NewNativeMemoryPlugin())       // Cross-platform fallback
}
```

## Usage Examples

### Memory Monitoring
```go
import "github.com/yairfalse/tapio/pkg/capabilities"

// Request memory monitoring capability
memCap, err := capabilities.RequestMemoryMonitoring()
if err != nil {
    // Handle unavailability clearly - no fake data!
    log.Printf("Memory monitoring not available: %v", err)
    return
}

// Start monitoring  
ctx := context.Background()
if err := memCap.Start(ctx); err != nil {
    log.Fatalf("Failed to start: %v", err)
}
defer memCap.Stop()

// Get real data (or real error)
stats, err := memCap.GetMemoryStats()
if err != nil {
    log.Fatalf("Failed to get stats: %v", err)
}

fmt.Printf("Tracking %d processes\n", len(stats))
```

### Platform Discovery
```go
// Discover what's available on this platform
report := capabilities.GetCapabilityReport()

fmt.Printf("Platform: %s\n", report.Platform)
fmt.Printf("Available capabilities: %d/%d\n", 
    report.Summary.Available, report.Summary.Total)

for name, status := range report.Capabilities {
    if status.Info.Status == capabilities.CapabilityNotAvailable {
        fmt.Printf("❌ %s: %s\n", name, status.Info.Error)
    } else {
        fmt.Printf("✅ %s: available\n", name)
    }
}
```

### Graceful Degradation
```go
// Start all available capabilities
ctx := context.Background()
report := capabilities.StartWithGracefulDegradation(ctx)

fmt.Printf("Started: %v\n", report.Started)
fmt.Printf("Failed: %v\n", report.Failed)    // With reasons
fmt.Printf("Skipped: %v\n", report.Skipped)  // With reasons
```

## Platform Support Matrix

| Platform | eBPF Memory | Native Memory | Journald | Network |
|----------|-------------|---------------|----------|---------|
| Linux    | ✅ (kernel 4.14+, root) | ✅ (/proc) | 🚧 (planned) | 🚧 (planned) |
| macOS    | ❌ (Linux only) | 🚧 (task_info) | ❌ (Linux only) | 🚧 (planned) |
| Windows  | ❌ (Linux only) | 🚧 (PerfCounters) | ❌ (Linux only) | 🚧 (planned) |

**Legend:**
- ✅ Implemented and working
- 🚧 Planned/partial implementation  
- ❌ Not available (with clear error message)

## Build Optimization

The architecture includes build-time optimization to exclude unused code:

```go
// pkg/capabilities/plugins/ebpf_memory_linux.go
//go:build linux
// Real eBPF implementation only compiled on Linux

// pkg/capabilities/plugins/build_nonlinux.go  
//go:build !linux
// Provides clear error reporting on non-Linux platforms
```

**Result:** Non-Linux builds exclude all eBPF code, reducing binary size.

## Error Handling Philosophy

### No Silent Failures
```go
// OLD: Stub returns fake data silently
stats, _ := stubMonitor.GetMemoryStats() // Always "works"

// NEW: Clear capability reporting
stats, err := memCap.GetMemoryStats()
if err != nil {
    // Handle real unavailability
}
```

### Helpful Error Messages
```go
capabilities.NewCapabilityError(
    "memory-monitoring",
    "eBPF memory monitoring requires Linux kernel 4.14+ with root privileges or CAP_BPF capability", 
    runtime.GOOS,
)
```

## Testing Strategy

The architecture enables testing real implementations only:

```go
func TestMemoryCapability(t *testing.T) {
    memCap, err := capabilities.RequestMemoryMonitoring()
    if err != nil {
        // Skip test if not available - don't test fake implementations
        t.Skipf("Memory monitoring not available: %v", err)
    }
    
    // Test REAL implementation only
    stats, err := memCap.GetMemoryStats()
    // ... test real functionality
}
```

## Migration Guide

### From Stub-Based Code
```go
// BEFORE
monitor := ebpf.NewMonitor(config)  // Might be stub
stats, _ := monitor.GetMemoryStats() // Might be fake

// AFTER  
memCap, err := capabilities.RequestMemoryMonitoring()
if err != nil {
    log.Printf("Memory monitoring unavailable: %v", err)
    return // Handle unavailability explicitly
}
stats, err := memCap.GetMemoryStats() // Real data or real error
```

### From Platform Detection
```go
// BEFORE
if runtime.GOOS == "linux" {
    // Use real implementation
} else {
    // Use stub (fake data)
}

// AFTER
memCap, err := capabilities.RequestMemoryMonitoring()
if err != nil {
    // Capability handles platform detection
    return
}
// Use real implementation regardless of platform
```

## Performance Benefits

1. **Smaller Binaries:** Unused platform code excluded at build time
2. **No Runtime Overhead:** No fake event generation or mock data processing
3. **Clear Resource Usage:** Only real implementations consume resources
4. **Better Debugging:** No confusion between real and fake data

## Contributing

When adding new capabilities:

1. **Implement real functionality** in platform-specific plugins
2. **Provide clear error messages** for unsupported platforms  
3. **Use build tags** for platform-specific code
4. **Test real implementations only** - no stub tests
5. **Document platform requirements** clearly

Example:
```go
// pkg/capabilities/plugins/new_feature_linux.go
//go:build linux

func NewFeaturePlugin() *FeaturePlugin {
    // Real Linux implementation
}

// pkg/capabilities/plugins/new_feature_stub.go
//go:build !linux

func NewFeaturePlugin() capabilities.Capability {
    return NewNotAvailablePlugin("feature", "Linux-only feature")
}
```

## Architecture Benefits Summary

✅ **Zero Technical Debt:** No stub files to maintain  
✅ **Clear Error Handling:** Explicit capability unavailability  
✅ **Better Testing:** Real implementations only  
✅ **Smaller Binaries:** Build-time platform optimization  
✅ **No Fake Data:** Eliminates confusion and bugs  
✅ **Maintainable:** Plugin-based separation of concerns  
✅ **User-Friendly:** Clear error messages and capability discovery

This architecture ensures that Tapio provides clear, honest reporting about what's available on each platform while maintaining the accessibility principles outlined in the project's mission.