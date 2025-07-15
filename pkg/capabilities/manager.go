package capabilities

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// Manager provides high-level capability management with graceful degradation
type Manager struct {
	registry *CapabilityRegistry
	platform string
}

// NewManager creates a new capability manager
func NewManager() *Manager {
	return &Manager{
		registry: NewRegistry(),
		platform: runtime.GOOS,
	}
}

// RequestMemoryMonitoring attempts to get memory monitoring capability
// Returns clear error if not available instead of fake data
func (m *Manager) RequestMemoryMonitoring() (MemoryCapability, error) {
	// Try eBPF first (Linux only)
	if m.platform == "linux" {
		if cap, err := m.registry.GetMemoryCapability("ebpf-memory"); err == nil {
			if cap.IsAvailable() {
				return cap, nil
			}
		}
	}

	// Try native platform monitoring
	if cap, err := m.registry.GetMemoryCapability("native-memory"); err == nil {
		if cap.IsAvailable() {
			return cap, nil
		}
	}

	// No capability available - return clear error instead of stub
	return nil, NewCapabilityError(
		"memory-monitoring",
		fmt.Sprintf("no memory monitoring capability available (platform: %s, available: %v)",
			m.platform, m.registry.ListByType("memory")),
		m.platform,
	)
}

// RequestNetworkMonitoring attempts to get network monitoring capability
func (m *Manager) RequestNetworkMonitoring() (NetworkCapability, error) {
	// Try eBPF first (Linux only)
	if m.platform == "linux" {
		if cap, err := m.registry.GetNetworkCapability("ebpf-network"); err == nil {
			if cap.IsAvailable() {
				return cap, nil
			}
		}
	}

	// Try native platform monitoring
	if cap, err := m.registry.GetNetworkCapability("native-network"); err == nil {
		if cap.IsAvailable() {
			return cap, nil
		}
	}

	return nil, NewCapabilityError(
		"network-monitoring",
		fmt.Sprintf("no network monitoring capability available (platform: %s, available: %v)",
			m.platform, m.registry.ListByType("network")),
		m.platform,
	)
}

// RequestSystemMonitoring attempts to get system monitoring capability
func (m *Manager) RequestSystemMonitoring() (SystemCapability, error) {
	// Try journald first (Linux only)
	if m.platform == "linux" {
		if cap, err := m.registry.GetSystemCapability("journald"); err == nil {
			if cap.IsAvailable() {
				return cap, nil
			}
		}
	}

	// Try native system monitoring
	if cap, err := m.registry.GetSystemCapability("native-system"); err == nil {
		if cap.IsAvailable() {
			return cap, nil
		}
	}

	return nil, NewCapabilityError(
		"system-monitoring",
		fmt.Sprintf("no system monitoring capability available (platform: %s, available: %v)",
			m.platform, m.registry.ListByType("system")),
		m.platform,
	)
}

// GetCapabilityReport returns a comprehensive report of all capabilities
func (m *Manager) GetCapabilityReport() *CapabilityReport {
	status := m.registry.GetStatus()
	health := m.registry.GetHealthStatus()

	report := &CapabilityReport{
		Platform:     m.platform,
		Timestamp:    time.Now(),
		Capabilities: make(map[string]*CapabilityStatus),
		Summary:      &CapabilitySummary{},
	}

	var available, enabled, errors int
	for name, info := range status {
		capStatus := &CapabilityStatus{
			Info:   info,
			Health: health[name],
		}

		switch info.Status {
		case CapabilityAvailable:
			available++
		case CapabilityEnabled:
			enabled++
		case CapabilityError:
			errors++
		}

		report.Capabilities[name] = capStatus
	}

	report.Summary.Total = len(status)
	report.Summary.Available = available
	report.Summary.Enabled = enabled
	report.Summary.Errors = errors
	report.Summary.NotAvailable = report.Summary.Total - available - enabled - errors

	return report
}

// StartWithGracefulDegradation starts all available capabilities
// Reports what couldn't be started instead of silently failing
func (m *Manager) StartWithGracefulDegradation(ctx context.Context) *StartupReport {
	report := &StartupReport{
		Platform:  m.platform,
		Timestamp: time.Now(),
		Started:   make([]string, 0),
		Failed:    make(map[string]string),
		Skipped:   make(map[string]string),
	}

	for name, cap := range m.registry.capabilities {
		if !cap.IsAvailable() {
			info := cap.Info()
			report.Skipped[name] = fmt.Sprintf("not available: %s", info.Error)
			continue
		}

		if err := cap.Start(ctx); err != nil {
			report.Failed[name] = err.Error()
		} else {
			report.Started = append(report.Started, name)
		}
	}

	return report
}

// CapabilityReport provides comprehensive capability information
type CapabilityReport struct {
	Platform     string                       `json:"platform"`
	Timestamp    time.Time                    `json:"timestamp"`
	Capabilities map[string]*CapabilityStatus `json:"capabilities"`
	Summary      *CapabilitySummary           `json:"summary"`
}

// CapabilityStatus combines capability info and health
type CapabilityStatus struct {
	Info   *CapabilityInfo `json:"info"`
	Health *HealthStatus   `json:"health"`
}

// CapabilitySummary provides a summary of capability status
type CapabilitySummary struct {
	Total        int `json:"total"`
	Available    int `json:"available"`
	Enabled      int `json:"enabled"`
	NotAvailable int `json:"not_available"`
	Errors       int `json:"errors"`
}

// StartupReport provides information about capability startup
type StartupReport struct {
	Platform  string            `json:"platform"`
	Timestamp time.Time         `json:"timestamp"`
	Started   []string          `json:"started"`
	Failed    map[string]string `json:"failed"`
	Skipped   map[string]string `json:"skipped"`
}

// GetPlatformSupportMatrix returns what's supported on each platform
func GetPlatformSupportMatrix() map[string][]string {
	return map[string][]string{
		"linux": {
			"ebpf-memory",
			"ebpf-network",
			"journald",
			"native-memory",
			"native-network",
			"native-system",
		},
		"darwin": {
			"native-memory",
			"native-network",
			"native-system",
		},
		"windows": {
			"native-memory",
			"native-network",
			"native-system",
		},
	}
}

// IsCapabilitySupported checks if a capability is supported on a platform
func IsCapabilitySupported(capability, platform string) bool {
	supported := GetPlatformSupportMatrix()
	platformCaps, exists := supported[platform]
	if !exists {
		return false
	}

	for _, cap := range platformCaps {
		if cap == capability {
			return true
		}
	}
	return false
}

// Global manager instance
var globalManager = NewManager()

// Global convenience functions

// RequestMemoryMonitoring requests memory monitoring from global manager
func RequestMemoryMonitoring() (MemoryCapability, error) {
	return globalManager.RequestMemoryMonitoring()
}

// RequestNetworkMonitoring requests network monitoring from global manager
func RequestNetworkMonitoring() (NetworkCapability, error) {
	return globalManager.RequestNetworkMonitoring()
}

// RequestSystemMonitoring requests system monitoring from global manager
func RequestSystemMonitoring() (SystemCapability, error) {
	return globalManager.RequestSystemMonitoring()
}

// GetCapabilityReport gets capability report from global manager
func GetCapabilityReport() *CapabilityReport {
	return globalManager.GetCapabilityReport()
}

// StartWithGracefulDegradation starts capabilities with graceful degradation
func StartWithGracefulDegradation(ctx context.Context) *StartupReport {
	return globalManager.StartWithGracefulDegradation(ctx)
}
