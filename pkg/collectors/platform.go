package collectors

import (
	"runtime"
)

// Platform represents the current platform
type Platform struct {
	OS           string
	Architecture string
	HasEBPF      bool
	HasJournald  bool
	HasSystemd   bool
}

// GetCurrentPlatform returns platform information
func GetCurrentPlatform() Platform {
	return Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		HasEBPF:      hasEBPFSupport(),
		HasJournald:  hasJournaldSupport(),
		HasSystemd:   hasSystemdSupport(),
	}
}

// hasEBPFSupport checks if eBPF is available on the current platform
func hasEBPFSupport() bool {
	return runtime.GOOS == "linux"
}

// hasJournaldSupport checks if journald is available on the current platform
func hasJournaldSupport() bool {
	return runtime.GOOS == "linux"
}

// hasSystemdSupport checks if systemd is available on the current platform
func hasSystemdSupport() bool {
	return runtime.GOOS == "linux"
}

// GetSupportedCollectors returns the list of collectors supported on the current platform
func GetSupportedCollectors() []string {
	platform := GetCurrentPlatform()
	supported := []string{}

	// Basic collectors that work on all platforms
	supported = append(supported, "simple", "basic")

	// Platform-specific collectors
	if platform.HasEBPF {
		supported = append(supported, "ebpf", "memory", "network")
	}
	
	if platform.HasJournald {
		supported = append(supported, "journald", "systemd")
	}

	// Mock collectors for development (always available)
	supported = append(supported, "mock", "stub")

	return supported
}

// IsCollectorSupported checks if a collector is supported on the current platform
func IsCollectorSupported(collectorType string) bool {
	supported := GetSupportedCollectors()
	for _, s := range supported {
		if s == collectorType {
			return true
		}
	}
	return false
}

// GetPlatformMessage returns a descriptive message about platform support
func GetPlatformMessage(collectorType string) string {
	platform := GetCurrentPlatform()
	
	switch collectorType {
	case "ebpf":
		if platform.HasEBPF {
			return "eBPF collector is supported on this Linux system"
		}
		return "eBPF collector is only supported on Linux (stub implementation active)"
	case "journald":
		if platform.HasJournald {
			return "Journald collector is supported on this Linux system"
		}
		return "Journald collector is only supported on Linux (stub implementation active)"
	case "systemd":
		if platform.HasSystemd {
			return "Systemd collector is supported on this Linux system"
		}
		return "Systemd collector is only supported on Linux (stub implementation active)"
	default:
		return "Collector is supported on all platforms"
	}
}