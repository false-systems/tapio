package plugins

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/capabilities"
)

// NativeMemoryPlugin provides basic memory monitoring using OS-native APIs
type NativeMemoryPlugin struct {
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewNativeMemoryPlugin creates a new native memory monitoring plugin
func NewNativeMemoryPlugin() *NativeMemoryPlugin {
	return &NativeMemoryPlugin{}
}

// Name returns the plugin name
func (p *NativeMemoryPlugin) Name() string {
	return "native-memory"
}

// Info returns capability information
func (p *NativeMemoryPlugin) Info() *capabilities.CapabilityInfo {
	info := &capabilities.CapabilityInfo{
		Name:     p.Name(),
		Platform: runtime.GOOS,
		Metadata: map[string]string{
			"implementation": "native",
			"data_source":    getNativeDataSource(),
		},
	}

	if !p.IsAvailable() {
		info.Status = capabilities.CapabilityNotAvailable
		info.Error = "native memory monitoring not implemented for this platform"
		return info
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.running {
		info.Status = capabilities.CapabilityEnabled
	} else {
		info.Status = capabilities.CapabilityAvailable
	}

	return info
}

// IsAvailable checks if native memory monitoring is available
func (p *NativeMemoryPlugin) IsAvailable() bool {
	// Available on all platforms, but with limited functionality
	return true
}

// Start initializes native memory monitoring
func (p *NativeMemoryPlugin) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("native memory monitoring already running")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.running = true

	return nil
}

// Stop gracefully stops native memory monitoring
func (p *NativeMemoryPlugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	if p.cancel != nil {
		p.cancel()
	}

	p.running = false
	return nil
}

// Health returns the current health status
func (p *NativeMemoryPlugin) Health() *capabilities.HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := &capabilities.HealthStatus{
		Timestamp: time.Now(),
		Metrics: map[string]any{
			"platform": runtime.GOOS,
			"limitations": []string{
				"No real-time kernel-level monitoring",
				"Limited to /proc filesystem data",
				"No OOM prediction capability",
			},
		},
	}

	if p.running {
		status.Status = capabilities.CapabilityEnabled
		status.Message = "Native memory monitoring active (limited functionality)"
	} else {
		status.Status = capabilities.CapabilityAvailable
		status.Message = "Native memory monitoring available"
	}

	return status
}

// GetMemoryStats returns basic memory statistics using native APIs
func (p *NativeMemoryPlugin) GetMemoryStats() ([]capabilities.ProcessMemoryStats, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.running {
		return nil, fmt.Errorf("native memory monitoring not running")
	}

	switch runtime.GOOS {
	case "linux":
		return p.getLinuxMemoryStats()
	case "darwin":
		return p.getDarwinMemoryStats()
	case "windows":
		return p.getWindowsMemoryStats()
	default:
		return nil, capabilities.NewCapabilityError(
			"memory-stats",
			fmt.Sprintf("native memory monitoring not implemented for %s", runtime.GOOS),
			runtime.GOOS,
		)
	}
}

// GetMemoryPredictions returns OOM predictions (limited functionality)
func (p *NativeMemoryPlugin) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*capabilities.OOMPrediction, error) {
	return nil, capabilities.NewCapabilityError(
		"oom-prediction",
		"OOM prediction requires eBPF monitoring (Linux only with kernel-level access)",
		runtime.GOOS,
	)
}

// Platform-specific implementations

func (p *NativeMemoryPlugin) getLinuxMemoryStats() ([]capabilities.ProcessMemoryStats, error) {
	// Read from /proc filesystem
	// This is basic implementation - real eBPF is much more accurate
	return []capabilities.ProcessMemoryStats{}, fmt.Errorf(
		"basic /proc reading not yet implemented - use eBPF monitoring for accurate Linux memory tracking",
	)
}

func (p *NativeMemoryPlugin) getDarwinMemoryStats() ([]capabilities.ProcessMemoryStats, error) {
	// Use macOS system APIs
	return []capabilities.ProcessMemoryStats{}, capabilities.NewCapabilityError(
		"memory-stats",
		"macOS memory monitoring not yet implemented - would use task_info() system calls",
		"darwin",
	)
}

func (p *NativeMemoryPlugin) getWindowsMemoryStats() ([]capabilities.ProcessMemoryStats, error) {
	// Use Windows Performance Counters or WMI
	return []capabilities.ProcessMemoryStats{}, capabilities.NewCapabilityError(
		"memory-stats",
		"Windows memory monitoring not yet implemented - would use Performance Counters API",
		"windows",
	)
}

func getNativeDataSource() string {
	switch runtime.GOOS {
	case "linux":
		return "/proc filesystem"
	case "darwin":
		return "task_info() system calls"
	case "windows":
		return "Performance Counters API"
	default:
		return "not available"
	}
}

// NotAvailablePlugin represents a capability that's not available
type NotAvailablePlugin struct {
	name     string
	reason   string
	platform string
}

// NewNotAvailablePlugin creates a plugin that clearly reports unavailability
func NewNotAvailablePlugin(name, reason string) *NotAvailablePlugin {
	return &NotAvailablePlugin{
		name:     name,
		reason:   reason,
		platform: runtime.GOOS,
	}
}

func (p *NotAvailablePlugin) Name() string {
	return p.name
}

func (p *NotAvailablePlugin) Info() *capabilities.CapabilityInfo {
	return &capabilities.CapabilityInfo{
		Name:     p.name,
		Status:   capabilities.CapabilityNotAvailable,
		Platform: p.platform,
		Error:    p.reason,
		Requirements: []string{
			"Feature not supported on this platform",
			"Use Linux with eBPF for full functionality",
		},
	}
}

func (p *NotAvailablePlugin) IsAvailable() bool {
	return false
}

func (p *NotAvailablePlugin) Start(ctx context.Context) error {
	return capabilities.NewCapabilityError(p.name, p.reason, p.platform)
}

func (p *NotAvailablePlugin) Stop() error {
	return nil // No-op for unavailable capabilities
}

func (p *NotAvailablePlugin) Health() *capabilities.HealthStatus {
	return &capabilities.HealthStatus{
		Status:    capabilities.CapabilityNotAvailable,
		Message:   p.reason,
		Timestamp: time.Now(),
		Metrics: map[string]any{
			"platform": p.platform,
			"reason":   p.reason,
		},
	}
}

// RegisterPlatformCapabilities registers appropriate capabilities for the current platform
func RegisterPlatformCapabilities() error {
	platform := runtime.GOOS

	// Register memory capabilities
	if platform == "linux" {
		// Register both eBPF and native for Linux
		if err := capabilities.Register(NewEBPFMemoryPlugin(nil)); err != nil {
			return fmt.Errorf("failed to register eBPF memory plugin: %w", err)
		}
	}

	// Always register native memory (with clear limitations)
	if err := capabilities.Register(NewNativeMemoryPlugin()); err != nil {
		return fmt.Errorf("failed to register native memory plugin: %w", err)
	}

	// Register network capabilities (not available yet)
	networkReason := fmt.Sprintf("network monitoring not yet implemented for %s - requires platform-specific implementation", platform)
	if err := capabilities.Register(NewNotAvailablePlugin("native-network", networkReason)); err != nil {
		return fmt.Errorf("failed to register network not-available plugin: %w", err)
	}

	// Register system capabilities
	systemReason := fmt.Sprintf("system monitoring not yet implemented for %s", platform)
	if platform != "linux" {
		systemReason += " - journald only available on Linux"
	}
	if err := capabilities.Register(NewNotAvailablePlugin("native-system", systemReason)); err != nil {
		return fmt.Errorf("failed to register system not-available plugin: %w", err)
	}

	return nil
}
