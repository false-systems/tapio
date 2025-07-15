package capabilities

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// PlatformDetector provides runtime detection of platform capabilities
type PlatformDetector struct {
	platform string
	cache    map[string]bool
}

// NewPlatformDetector creates a new platform detector
func NewPlatformDetector() *PlatformDetector {
	return &PlatformDetector{
		platform: runtime.GOOS,
		cache:    make(map[string]bool),
	}
}

// DetectEBPFSupport checks if eBPF is supported at runtime
func (d *PlatformDetector) DetectEBPFSupport() (bool, string) {
	if cached, exists := d.cache["ebpf"]; exists {
		return cached, "cached result"
	}

	// Only Linux supports eBPF
	if d.platform != "linux" {
		d.cache["ebpf"] = false
		return false, fmt.Sprintf("eBPF not supported on %s", d.platform)
	}

	// Check kernel version
	if supported, reason := d.checkKernelVersion(); !supported {
		d.cache["ebpf"] = false
		return false, reason
	}

	// Check for eBPF filesystem
	if supported, reason := d.checkEBPFFilesystem(); !supported {
		d.cache["ebpf"] = false
		return false, reason
	}

	// Check permissions
	if supported, reason := d.checkEBPFPermissions(); !supported {
		d.cache["ebpf"] = false
		return false, reason
	}

	d.cache["ebpf"] = true
	return true, "eBPF fully supported"
}

// DetectJournaldSupport checks if journald is available
func (d *PlatformDetector) DetectJournaldSupport() (bool, string) {
	if cached, exists := d.cache["journald"]; exists {
		return cached, "cached result"
	}

	// Only Linux has journald
	if d.platform != "linux" {
		d.cache["journald"] = false
		return false, fmt.Sprintf("journald not available on %s", d.platform)
	}

	// Check if journald is running
	if supported, reason := d.checkJournaldService(); !supported {
		d.cache["journald"] = false
		return false, reason
	}

	// Check for journal files
	if supported, reason := d.checkJournaldFiles(); !supported {
		d.cache["journald"] = false
		return false, reason
	}

	d.cache["journald"] = true
	return true, "journald available"
}

// DetectNativeMemorySupport checks native memory monitoring capabilities
func (d *PlatformDetector) DetectNativeMemorySupport() (bool, string) {
	switch d.platform {
	case "linux":
		// Check /proc filesystem
		if _, err := os.Stat("/proc/meminfo"); err != nil {
			return false, "/proc filesystem not accessible"
		}
		return true, "/proc filesystem available"
	case "darwin":
		// macOS would use task_info() system calls
		return false, "task_info() implementation not yet available"
	case "windows":
		// Windows would use Performance Counters
		return false, "Performance Counters implementation not yet available"
	default:
		return false, fmt.Sprintf("native memory monitoring not implemented for %s", d.platform)
	}
}

// GetPlatformCapabilities returns all detected capabilities
func (d *PlatformDetector) GetPlatformCapabilities() map[string]CapabilityDetection {
	capabilities := make(map[string]CapabilityDetection)

	// eBPF support
	ebpfSupported, ebpfReason := d.DetectEBPFSupport()
	capabilities["ebpf"] = CapabilityDetection{
		Name:      "eBPF",
		Supported: ebpfSupported,
		Reason:    ebpfReason,
		Platform:  d.platform,
	}

	// Journald support
	journaldSupported, journaldReason := d.DetectJournaldSupport()
	capabilities["journald"] = CapabilityDetection{
		Name:      "journald",
		Supported: journaldSupported,
		Reason:    journaldReason,
		Platform:  d.platform,
	}

	// Native memory support
	memorySupported, memoryReason := d.DetectNativeMemorySupport()
	capabilities["native-memory"] = CapabilityDetection{
		Name:      "native-memory",
		Supported: memorySupported,
		Reason:    memoryReason,
		Platform:  d.platform,
	}

	return capabilities
}

// CapabilityDetection represents the result of capability detection
type CapabilityDetection struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
	Platform  string `json:"platform"`
}

// Private helper methods

func (d *PlatformDetector) checkKernelVersion() (bool, string) {
	// Read kernel version
	versionBytes, err := os.ReadFile("/proc/version")
	if err != nil {
		return false, "cannot read kernel version"
	}

	version := string(versionBytes)
	// Parse version (simplified)
	if strings.Contains(version, "Linux") {
		// Extract version number (this is a simplified check)
		// Real implementation would parse semantic version
		return true, "kernel version check passed"
	}

	return false, "unsupported kernel"
}

func (d *PlatformDetector) checkEBPFFilesystem() (bool, string) {
	// Check if bpffs is mounted
	if _, err := os.Stat("/sys/fs/bpf"); err != nil {
		return false, "bpffs not mounted at /sys/fs/bpf"
	}

	return true, "bpffs mounted"
}

func (d *PlatformDetector) checkEBPFPermissions() (bool, string) {
	// Check if running as root
	if os.Geteuid() == 0 {
		return true, "running as root"
	}

	// Check for CAP_BPF capability (simplified)
	// Real implementation would check capabilities properly
	return false, "insufficient privileges (need root or CAP_BPF)"
}

func (d *PlatformDetector) checkJournaldService() (bool, string) {
	// Check if systemd-journald is running
	// This is a simplified check
	if _, err := os.Stat("/run/systemd/journal"); err != nil {
		return false, "systemd-journald not running"
	}

	return true, "systemd-journald running"
}

func (d *PlatformDetector) checkJournaldFiles() (bool, string) {
	// Check for journal files
	journalDirs := []string{
		"/var/log/journal",
		"/run/log/journal",
	}

	for _, dir := range journalDirs {
		if _, err := os.Stat(dir); err == nil {
			return true, fmt.Sprintf("journal files found in %s", dir)
		}
	}

	return false, "no journal files found"
}

// GetDetailedPlatformInfo returns comprehensive platform information
func GetDetailedPlatformInfo() *PlatformInfo {
	detector := NewPlatformDetector()
	capabilities := detector.GetPlatformCapabilities()

	info := &PlatformInfo{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		Capabilities: capabilities,
	}

	// Add kernel information on Linux
	if runtime.GOOS == "linux" {
		if version, err := getKernelVersion(); err == nil {
			info.KernelVersion = version
		}
	}

	return info
}

// PlatformInfo contains detailed platform information
type PlatformInfo struct {
	OS            string                           `json:"os"`
	Architecture  string                           `json:"architecture"`
	KernelVersion string                           `json:"kernel_version,omitempty"`
	Capabilities  map[string]CapabilityDetection  `json:"capabilities"`
}

func getKernelVersion() (string, error) {
	versionBytes, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", err
	}

	version := string(versionBytes)
	// Extract just the version number
	parts := strings.Fields(version)
	if len(parts) >= 3 {
		return parts[2], nil
	}

	return strings.TrimSpace(version), nil
}

// Global detector instance
var globalDetector = NewPlatformDetector()

// Global convenience functions

// DetectEBPF detects eBPF support using global detector
func DetectEBPF() (bool, string) {
	return globalDetector.DetectEBPFSupport()
}

// DetectJournald detects journald support using global detector
func DetectJournald() (bool, string) {
	return globalDetector.DetectJournaldSupport()
}

// DetectNativeMemory detects native memory support using global detector
func DetectNativeMemory() (bool, string) {
	return globalDetector.DetectNativeMemorySupport()
}